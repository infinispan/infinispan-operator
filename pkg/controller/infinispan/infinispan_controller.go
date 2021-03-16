package infinispan

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/caches"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/version"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	DataMountPath             = "/opt/infinispan/server/data"
	DataMountVolume           = "data-volume"
	CustomLibrariesMountPath  = "/opt/infinispan/server/lib/custom-libraries"
	CustomLibrariesVolumeName = "custom-libraries"
	EncryptMountPath          = "/etc/encrypt"
	ConfigVolumeName          = "config-volume"
	EncryptVolumeName         = "encrypt-volume"
	IdentitiesVolumeName      = "identities-volume"
	AdminIdentitiesVolumeName = "admin-identities-volume"
)

var log = logf.Log.WithName("controller_infinispan")

// Kubernetes object
var kubernetes *kube.Kubernetes

// Add creates a new Infinispan Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	kubernetes = kube.NewKubernetesFromController(mgr)
	return &ReconcileInfinispan{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

type SecondaryResourceType struct {
	objectType      runtime.Object
	objectPredicate predicate.Funcs
}

func secondaryResourceTypes() []SecondaryResourceType {
	return []SecondaryResourceType{
		{&appsv1.StatefulSet{}, predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			}},
		},
		{&corev1.ConfigMap{}, predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			}},
		},
		{&corev1.Secret{}, predicate.Funcs{
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			}},
		},
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("infinispan-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Infinispan
	err = c.Watch(&source.Kind{Type: &infinispanv1.Infinispan{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Infinispan
	for _, secondaryResource := range secondaryResourceTypes() {
		if err = c.Watch(&source.Kind{Type: secondaryResource.objectType}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &infinispanv1.Infinispan{},
		}, secondaryResource.objectPredicate); err != nil {
			return err
		}
	}
	return nil
}

var _ reconcile.Reconciler = &ReconcileInfinispan{}

// ReconcileInfinispan reconciles a Infinispan object
type ReconcileInfinispan struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Infinispan object and makes changes based on the state read
// and what is in the Infinispan.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileInfinispan) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info(fmt.Sprintf("+++++ Reconciling Infinispan. Operator Version: %s", version.Version))
	defer reqLogger.Info("----- End Reconciling Infinispan.")

	// Fetch the Infinispan instance
	infinispan := &infinispanv1.Infinispan{}
	infinispan.Name = request.Name
	infinispan.Namespace = request.Namespace
	var preliminaryChecksResult *reconcile.Result
	var preliminaryChecksError error
	err := r.update(infinispan, func() {
		// Apply defaults and endpoint encryption settings if not already set
		infinispan.ApplyDefaults()
		infinispan.Spec.Affinity = podAffinity(infinispan, PodLabels(infinispan.Name))
		errLabel := infinispan.ApplyOperatorLabels()
		if errLabel != nil {
			reqLogger.Error(errLabel, "Error applying operator label")
		}
		infinispan.ApplyEndpointEncryptionSettings(kubernetes.GetServingCertsMode(), reqLogger)

		// Perform all the possible preliminary checks before go on
		preliminaryChecksResult, preliminaryChecksError = infinispan.PreliminaryChecks()
		if preliminaryChecksError != nil {
			infinispan.SetCondition(infinispanv1.ConditionPrelimChecksPassed, metav1.ConditionFalse, preliminaryChecksError.Error())
		} else {
			infinispan.SetCondition(infinispanv1.ConditionPrelimChecksPassed, metav1.ConditionTrue, "")
		}
	}, false)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Infinispan resource not found. Ignoring since object must be deleted")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		reqLogger.Info("Error reading the object")
		return reconcile.Result{}, err
	}

	if preliminaryChecksResult != nil {
		return *preliminaryChecksResult, preliminaryChecksError
	}

	// Wait for the ConfigMap to be created by config-controller
	configMap := &corev1.ConfigMap{}
	if result, err := kube.LookupResource(infinispan.GetConfigName(), infinispan.Namespace, configMap, r.client, reqLogger); result != nil {
		return *result, err
	}

	// Wait for the Secret to be created by secret-controller or provided by ser
	var userSecret *corev1.Secret
	if infinispan.IsAuthenticationEnabled() {
		userSecret = &corev1.Secret{}
		if result, err := kube.LookupResource(infinispan.GetSecretName(), infinispan.Namespace, userSecret, r.client, reqLogger); result != nil {
			return *result, err
		}
	}

	adminSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(infinispan.GetAdminSecretName(), infinispan.Namespace, adminSecret, r.client, reqLogger); result != nil {
		return *result, err
	}

	// Reconcile the StatefulSet
	// Check if the StatefulSet already exists, if not create a new one
	statefulSet := &appsv1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, statefulSet)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Configuring the StatefulSet")

		// Define a new StatefulSet
		statefulSet, err = r.statefulSetForInfinispan(infinispan, adminSecret, userSecret, configMap)
		if err != nil {
			reqLogger.Error(err, "failed to configure new StatefulSet")
			return reconcile.Result{}, err
		}
		reqLogger.Info("Creating a new StatefulSet", "StatefulSet.Name", statefulSet.Name)
		err = r.client.Create(context.TODO(), statefulSet)
		if err != nil {
			reqLogger.Error(err, "failed to create new StatefulSet", "StatefulSet.Name", statefulSet.Name)
			return reconcile.Result{}, err
		}

		// StatefulSet created successfully
		reqLogger.Info("End of the StatefulSet creation")
	}
	if err != nil {
		reqLogger.Error(err, "failed to get StatefulSet")
		return reconcile.Result{}, err
	}

	// Update Pod's status for the OLM
	if err := r.update(infinispan, func() {
		infinispan.Status.PodStatus = olm.GetSingleStatefulSetStatus(*statefulSet)
	}); err != nil {
		return reconcile.Result{}, err
	}

	// Wait for the cluster Service to be created by service-controller
	if result, err := kube.LookupResource(infinispan.Name, infinispan.Namespace, &corev1.Service{}, r.client, reqLogger); result != nil {
		return *result, err
	}

	// Wait for the cluster ping Service to be created by service-controller
	if result, err := kube.LookupResource(infinispan.GetPingServiceName(), infinispan.Namespace, &corev1.Service{}, r.client, reqLogger); result != nil {
		return *result, err
	}

	if infinispan.IsExposed() {
		var exposeAddress string
		switch infinispan.GetExposeType() {
		case infinispanv1.ExposeTypeLoadBalancer, infinispanv1.ExposeTypeNodePort:
			// Wait for the cluster external Service to be created by service-controller
			externalService := &corev1.Service{}
			if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalService, r.client, reqLogger); result != nil {
				return *result, err
			}
			if len(externalService.Spec.Ports) > 0 && infinispan.GetExposeType() == infinispanv1.ExposeTypeNodePort {
				if exposeHost, err := kubernetes.GetNodeHost(reqLogger); err != nil {
					return reconcile.Result{}, err
				} else {
					exposeAddress = fmt.Sprintf("%s:%d", exposeHost, externalService.Spec.Ports[0].NodePort)
				}
			} else if infinispan.GetExposeType() == infinispanv1.ExposeTypeLoadBalancer {
				// Waiting for LoadBalancer cloud provider to update the configured hostname inside Status field
				if exposeAddress, err = kubernetes.GetExternalAddress(externalService); err != nil {
					return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCreateResource}, nil
				}
			}
		case infinispanv1.ExposeTypeRoute:
			var okIngress = false
			okRoute, err := kubernetes.IsGroupVersionSupported(routev1.GroupVersion.String(), "Route")
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Failed to check if %s,%s is supported", routev1.GroupVersion.String(), "Route"))
				// Log an error and try to check with Ingress
				okIngress, err = kubernetes.IsGroupVersionSupported(networkingv1beta1.SchemeGroupVersion.String(), "Ingress")
				if err != nil {
					reqLogger.Error(err, fmt.Sprintf("Failed to check if %s,%s is supported", networkingv1beta1.SchemeGroupVersion.String(), "Ingress"))
				}
			}
			if okRoute {
				externalRoute := &routev1.Route{}
				if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalRoute, r.client, reqLogger); result != nil {
					return *result, err
				}
				exposeAddress = externalRoute.Spec.Host
			} else if okIngress {
				externalIngress := &networkingv1beta1.Ingress{}
				if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalIngress, r.client, reqLogger); result != nil {
					return *result, err
				}
				if len(externalIngress.Spec.Rules) > 0 {
					exposeAddress = externalIngress.Spec.Rules[0].Host
				}
			}
		}
		if err := r.update(infinispan, func() {
			if exposeAddress == "" {
				infinispan.Status.ConsoleUrl = nil
			} else {
				infinispan.Status.ConsoleUrl = pointer.StringPtr(fmt.Sprintf("%s://%s/console", infinispan.GetEndpointScheme(), exposeAddress))
			}
		}); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		if err := r.update(infinispan, func() {
			infinispan.Status.ConsoleUrl = nil
		}); err != nil {
			return reconcile.Result{}, err
		}
	}

	if infinispan.IsUpgradeNeeded(reqLogger) {
		reqLogger.Info("Upgrade needed")
		err = r.destroyResources(infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to delete resources before upgrade")
			return reconcile.Result{}, err
		}

		if err := r.update(infinispan, func() {
			if infinispan.Spec.Replicas != infinispan.Status.ReplicasWantedAtRestart {
				reqLogger.Info("removed Infinispan resources, force an upgrade now", "replicasWantedAtRestart", infinispan.Status.ReplicasWantedAtRestart)
				infinispan.Spec.Replicas = infinispan.Status.ReplicasWantedAtRestart
			}
		}); err != nil {
			return reconcile.Result{}, err
		}
		if err := r.update(infinispan, func() {
			infinispan.SetCondition(infinispanv1.ConditionUpgrade, metav1.ConditionFalse, "")
		}); err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{Requeue: true}, nil
	}

	// List the pods for this Infinispan's deployment
	podList := &corev1.PodList{}
	if err := kubernetes.ResourcesList(infinispan.Namespace, PodLabels(infinispan.Name), podList); err != nil {
		reqLogger.Error(err, "failed to list pods", "Infinispan.Namespace")
		return reconcile.Result{}, err
	}

	if err = r.updatePodsLabels(infinispan, podList); err != nil {
		return reconcile.Result{}, err
	}

	result, err := r.scheduleUpgradeIfNeeded(infinispan, podList, reqLogger)
	if result != nil {
		return *result, err
	}

	cluster, err := NewCluster(infinispan, kubernetes)
	if err != nil {
		return reconcile.Result{}, err
	}

	// If user set Spec.replicas=0 we need to perform a graceful shutdown
	// to preserve the data
	var res *reconcile.Result
	res, err = r.reconcileGracefulShutdown(infinispan, statefulSet, podList, reqLogger, cluster)
	if res != nil {
		return *res, err
	}
	// If upgrade required, do not process any further and handle upgrades
	if infinispan.IsUpgradeCondition() {
		reqLogger.Info("IsUpgradeCondition")
		return reconcile.Result{Requeue: true}, nil
	}

	// Here where to reconcile with spec updates that reflect into
	// changes to statefulset.spec.container.
	res, err = r.reconcileContainerConf(infinispan, statefulSet, configMap, adminSecret, userSecret, reqLogger)
	if res != nil {
		return *res, err
	}

	// Update the Infinispan status with the pod status
	// Wait until all pods have IPs assigned
	// Without those IPs, it's not possible to execute next calls

	if !kube.ArePodIPsReady(podList) {
		reqLogger.Info("Pods IPs are not ready yet")
		return reconcile.Result{}, r.update(infinispan, func() {
			infinispan.SetCondition(infinispanv1.ConditionWellFormed, metav1.ConditionUnknown, "Pods are not ready")
			infinispan.Status.StatefulSetName = statefulSet.Name
		})
	}

	// If x-site enable configure the coordinator pods to be selected by the x-site service
	if infinispan.HasSites() {
		found := applyLabelsToCoordinatorsPod(podList, cluster, r.client, reqLogger)
		if !found {
			// If a coordinator is not found then requeue early
			return reconcile.Result{Requeue: true}, nil
		}
	}

	// All pods ready start autoscaler if needed
	if infinispan.Spec.Autoscale != nil && infinispan.IsCache() {
		addAutoscalingEquipment(types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, r)
	}
	// Inspect the system and get the current Infinispan conditions
	currConds := getInfinispanConditions(podList.Items, infinispan, cluster)

	// Update the Infinispan status with the pod status
	if err := r.update(infinispan, func() {
		infinispan.SetConditions(currConds)
	}); err != nil {
		return reconcile.Result{}, err
	}

	// View didn't form, requeue until view has formed
	if infinispan.NotClusterFormed(len(podList.Items), int(infinispan.Spec.Replicas)) {
		reqLogger.Info("notClusterFormed")
		return reconcile.Result{Requeue: true, RequeueAfter: consts.DefaultWaitClusterNotWellFormed}, nil
	}

	// Below the code for a wellFormed cluster
	err = configureLoggers(podList, cluster, infinispan)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create default cache if it doesn't exists.
	if infinispan.IsCache() {
		existsCache, err := cluster.ExistsCache(consts.DefaultCacheName, podList.Items[0].Name)
		if err != nil {
			reqLogger.Error(err, "failed to validate default cache for cache service")
			return reconcile.Result{}, err
		}
		if !existsCache {
			reqLogger.Info("createDefaultCache")
			err = caches.CreateCacheFromDefault(podList.Items[0].Name, infinispan, cluster, reqLogger)
			if err != nil {
				reqLogger.Error(err, "failed to create default cache for cache service")
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, err
}

func configureLoggers(pods *corev1.PodList, cluster ispn.ClusterInterface, infinispan *infinispanv1.Infinispan) error {
	if infinispan.Spec.Logging == nil || len(infinispan.Spec.Logging.Categories) == 0 {
		return nil
	}
	for _, pod := range pods.Items {
		serverLoggers, err := cluster.GetLoggers(pod.Name)
		if err != nil {
			return err
		}
		for category, level := range infinispan.Spec.Logging.Categories {
			serverLevel, ok := serverLoggers[category]
			if !(ok && string(level) == serverLevel) {
				if err := cluster.SetLogger(pod.Name, category, string(level)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *ReconcileInfinispan) destroyResources(infinispan *infinispanv1.Infinispan) error {
	// TODO destroying all upgradable resources for recreation is too manual
	// Labels cannot easily be used to remove all resources with a given label.
	// Resource controller could be used to make this easier.
	// If all upgradable resources are controlled by the Stateful Set,
	// removing the Stateful Set should remove the rest.
	// Then, stateful set could be controlled by Infinispan to keep current logic.

	// Remove finalizer (we don't use it anymore) if it present and set owner reference for old PVCs
	err := r.upgradeInfinispan(infinispan)
	if err != nil {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.Name,
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetConfigName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.Name,
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetPingServiceName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetServiceExternalName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetSiteServiceName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func (r *ReconcileInfinispan) upgradeInfinispan(infinispan *infinispanv1.Infinispan) error {
	if contains(infinispan.GetFinalizers(), consts.InfinispanFinalizer) {
		// Set Infinispan CR as owner reference for PVC if it not defined
		pvcs := &corev1.PersistentVolumeClaimList{}
		err := kubernetes.ResourcesList(infinispan.Namespace, LabelsResource(infinispan.Name, ""), pvcs)
		if err != nil {
			return err
		}

		for _, pvc := range pvcs.Items {
			if !metav1.IsControlledBy(&pvc, infinispan) {
				controllerutil.SetControllerReference(infinispan, &pvc, r.scheme)
				pvc.OwnerReferences[0].BlockOwnerDeletion = pointer.BoolPtr(false)
				err := r.client.Update(context.TODO(), &pvc)
				if err != nil {
					return err
				}
			}
		}
	}

	return r.update(infinispan, func() {
		// Remove finalizer if it defined in the Infinispan CR
		if contains(infinispan.GetFinalizers(), consts.InfinispanFinalizer) {
			infinispan.SetFinalizers(remove(infinispan.GetFinalizers(), consts.InfinispanFinalizer))
		}

		infinispan.Spec.Image = nil
		sc := infinispan.Spec.Service.Container
		if sc != nil && sc.Storage != nil && *sc.Storage == "" {
			sc.Storage = nil
		}

		if infinispan.HasSites() {
			// Migrate Spec.Service.Locations Host and Port parameters into the unified URL schema
			for i, location := range infinispan.Spec.Service.Sites.Locations {
				if location.Host != nil && *location.Host != "" {
					port := consts.CrossSitePort
					if location.Port != nil && *location.Port > 0 {
						port = int(*location.Port)
					}
					infinispan.Spec.Service.Sites.Locations[i].Host = nil
					infinispan.Spec.Service.Sites.Locations[i].Port = nil
					infinispan.Spec.Service.Sites.Locations[i].URL = fmt.Sprintf("%s://%s:%d", consts.StaticCrossSiteUriSchema, *location.Host, port)
				}
			}
		}
	})
}

func (r *ReconcileInfinispan) scheduleUpgradeIfNeeded(infinispan *infinispanv1.Infinispan, podList *corev1.PodList, logger logr.Logger) (*reconcile.Result, error) {
	if len(podList.Items) == 0 {
		return nil, nil
	}
	if infinispan.IsUpgradeCondition() {
		return nil, nil
	}

	// All pods need to be ready for the upgrade to be scheduled
	// Handles brief window during which resources have been removed,
	//and old ones terminating while new ones are being created.
	// We don't want yet another upgrade to be scheduled then.
	if !kube.AreAllPodsReady(podList) {
		return nil, nil
	}

	// Get default Infinispan image for a running Infinispan pod
	podDefaultImage := kube.GetPodDefaultImage(podList.Items[0].Spec.Containers[0])

	// Get Infinispan image that the operator creates
	desiredImage := consts.DefaultImageName

	// If the operator's default image differs from the pod's default image,
	// schedule an upgrade by gracefully shutting down the current cluster.
	if podDefaultImage != desiredImage {
		logger.Info("schedule an Infinispan cluster upgrade", "pod default image", podDefaultImage, "desired image", desiredImage)
		if err := r.update(infinispan, func() {
			infinispan.Spec.Replicas = 0
		}); err != nil {
			return &reconcile.Result{}, err
		}
		if err := r.update(infinispan, func() {
			infinispan.SetCondition(infinispanv1.ConditionUpgrade, metav1.ConditionTrue, "")
		}); err != nil {
			return &reconcile.Result{}, err
		}
	}
	return nil, nil
}

func podAffinity(i *infinispanv1.Infinispan, matchLabels map[string]string) *corev1.Affinity {
	// The user hasn't configured Affinity, so we utilise the default strategy of preferring pods are deployed on distinct nodes
	if i.Spec.Affinity == nil {
		return &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: matchLabels,
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				}},
			},
		}
	}
	return i.Spec.Affinity
}

func (r *ReconcileInfinispan) updatePodsLabels(m *infinispanv1.Infinispan, podList *corev1.PodList) error {
	if len(podList.Items) == 0 {
		return nil
	}

	labelsForPod := PodLabels(m.Name)
	m.AddOperatorLabelsForPods(labelsForPod)
	m.AddLabelsForPods(labelsForPod)

	for _, pod := range podList.Items {
		podLabels := make(map[string]string)
		for index, value := range pod.Labels {
			if _, ok := labelsForPod[index]; ok || consts.SystemPodLabels[index] {
				podLabels[index] = value
			}
		}
		for index, value := range labelsForPod {
			podLabels[index] = value
		}

		_, err := controllerutil.CreateOrUpdate(context.TODO(), r.client, &pod, func() error {
			if pod.CreationTimestamp.IsZero() {
				return errors.NewNotFound(corev1.Resource(""), pod.Name)
			}
			pod.Labels = podLabels
			return nil
		})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// statefulSetForInfinispan returns an infinispan StatefulSet object
func (r *ReconcileInfinispan) statefulSetForInfinispan(m *infinispanv1.Infinispan, adminSecret, userSecret *corev1.Secret, configMap *corev1.ConfigMap) (*appsv1.StatefulSet, error) {
	reqLogger := log.WithValues("Request.Namespace", m.Namespace, "Request.Name", m.Name)
	lsPod := PodLabels(m.Name)
	labelsForPod := PodLabels(m.Name)
	m.AddOperatorLabelsForPods(labelsForPod)
	m.AddLabelsForPods(labelsForPod)

	pvcs := &corev1.PersistentVolumeClaimList{}
	err := kubernetes.ResourcesList(m.Namespace, LabelsResource(m.Name, ""), pvcs)
	if err != nil {
		return nil, err
	}
	dataVolumeName := DataMountVolume
	for _, pvc := range pvcs.Items {
		if strings.HasPrefix(pvc.Name, fmt.Sprintf("%s-%s", m.Name, m.Name)) {
			dataVolumeName = m.Name
			break
		}
	}

	memory, err := resource.ParseQuantity(m.Spec.Container.Memory)
	if err != nil {
		return nil, err
	}
	replicas := m.Spec.Replicas
	volumeMounts := []corev1.VolumeMount{{
		Name:      ConfigVolumeName,
		MountPath: consts.ServerConfigRoot,
	}, {
		Name:      dataVolumeName,
		MountPath: DataMountPath,
	}, {
		Name:      AdminIdentitiesVolumeName,
		MountPath: consts.ServerAdminIdentitiesRoot,
	}}
	volumes := []corev1.Volume{{
		Name: ConfigVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: configMap.Name},
			},
		},
	}, {
		Name: AdminIdentitiesVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: m.GetAdminSecretName(),
			},
		},
	}}

	if m.HasCustomLibraries() {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{Name: CustomLibrariesVolumeName, MountPath: CustomLibrariesMountPath, ReadOnly: true})
		volumes = append(volumes, corev1.Volume{Name: CustomLibrariesVolumeName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: m.Spec.Dependencies.VolumeClaimName, ReadOnly: true}}})
	}

	podResources, err := PodResources(m.Spec.Container)
	if err != nil {
		return nil, err
	}
	dep := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        m.Name,
			Namespace:   m.Namespace,
			Annotations: consts.DeploymentAnnotations,
			Labels:      map[string]string{},
		},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: lsPod,
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labelsForPod,
					Annotations: map[string]string{"updateDate": time.Now().String()},
				},
				Spec: corev1.PodSpec{
					Affinity: m.Spec.Affinity,
					Containers: []corev1.Container{{
						Image: m.ImageName(),
						Name:  "infinispan",
						Env: PodEnv(m, &[]corev1.EnvVar{
							{Name: "CONFIG_HASH", Value: hashString(configMap.Data[consts.ServerConfigFilename])},
							{Name: "ADMIN_IDENTITIES_HASH", Value: hashString(string(adminSecret.Data[consts.ServerIdentitiesFilename]))},
						}),
						LivenessProbe:  PodLivenessProbe(m),
						Ports:          PodPortsWithXsite(m),
						ReadinessProbe: PodReadinessProbe(m),
						StartupProbe:   PodStartupProbe(m),
						Resources:      *podResources,
						VolumeMounts:   volumeMounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}

	// Only append IDENTITIES_HASH and secret volume if authentication is enabled
	spec := &dep.Spec.Template.Spec
	if AddVolumeForUserAuthentication(m, spec) {
		spec.Containers[0].Env = append(spec.Containers[0].Env,
			corev1.EnvVar{
				Name:  "IDENTITIES_HASH",
				Value: hashString(string(userSecret.Data[consts.ServerIdentitiesFilename])),
			})
	}

	if !m.IsEphemeralStorage() {
		// Persistent vol size must exceed memory size
		// so that it can contain all the in memory data
		pvSize := consts.DefaultPVSize
		if pvSize.Cmp(memory) < 0 {
			pvSize = memory
		}

		if m.IsDataGrid() && m.StorageSize() != "" {
			var pvErr error
			pvSize, pvErr = resource.ParseQuantity(m.StorageSize())
			if pvErr != nil {
				return nil, pvErr
			}
			if pvSize.Cmp(memory) < 0 {
				reqLogger.Info("WARNING: persistent volume size is less than memory size. Graceful shutdown may not work.", "Volume Size", pvSize, "Memory", memory)
			}
		}

		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name:      dataVolumeName,
			Namespace: m.Namespace,
		},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: pvSize,
					},
				},
			},
		}

		controllerutil.SetControllerReference(m, pvc, r.scheme)
		pvc.OwnerReferences[0].BlockOwnerDeletion = pointer.BoolPtr(false)
		// Set a storage class if it specified
		if storageClassName := m.StorageClassName(); storageClassName != "" {
			if _, err := kube.FindStorageClass(storageClassName, r.client); err != nil {
				return nil, err
			}
			pvc.Spec.StorageClassName = &storageClassName
		}
		dep.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{*pvc}

		AddVolumeChmodInitContainer("data-chmod-pv", dataVolumeName, DataMountPath, &dep.Spec.Template.Spec)
	} else {
		volumes := &dep.Spec.Template.Spec.Volumes
		ephemeralVolume := corev1.Volume{
			Name: dataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
		*volumes = append(*volumes, ephemeralVolume)
		reqLogger.Info("WARNING: Ephemeral storage configured. All data will be lost on cluster shutdown and restart.")
	}

	AddVolumeForEncryption(m, &dep.Spec.Template.Spec)

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, dep, r.scheme)
	return dep, nil
}

// Returns true if the volume has been added
func AddVolumeForUserAuthentication(i *infinispanv1.Infinispan, spec *corev1.PodSpec) bool {
	if _, index := findSecretInVolume(spec, IdentitiesVolumeName); !i.IsAuthenticationEnabled() || index >= 0 {
		return false
	}

	v := &spec.Volumes
	*v = append(*v, corev1.Volume{
		Name: IdentitiesVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: i.GetSecretName(),
			},
		},
	})

	vm := &spec.Containers[0].VolumeMounts
	*vm = append(*vm, corev1.VolumeMount{
		Name:      IdentitiesVolumeName,
		MountPath: consts.ServerUserIdentitiesRoot,
	})
	return true
}

func PodPorts() []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{ContainerPort: consts.InfinispanAdminPort, Name: consts.InfinispanAdminPortName, Protocol: corev1.ProtocolTCP},
		{ContainerPort: consts.InfinispanPingPort, Name: consts.InfinispanPingPortName, Protocol: corev1.ProtocolTCP},
		{ContainerPort: consts.InfinispanUserPort, Name: consts.InfinispanUserPortName, Protocol: corev1.ProtocolTCP},
	}
	return ports
}

func PodPortsWithXsite(i *infinispanv1.Infinispan) []corev1.ContainerPort {
	ports := PodPorts()
	if i.HasSites() {
		ports = append(ports, corev1.ContainerPort{ContainerPort: consts.CrossSitePort, Name: consts.CrossSitePortName, Protocol: corev1.ProtocolTCP})
	}
	return ports
}

func PodLivenessProbe(i *infinispanv1.Infinispan) *corev1.Probe {
	return probe(i, 5, 10, 10, 1, 80)
}

func PodReadinessProbe(i *infinispanv1.Infinispan) *corev1.Probe {
	return probe(i, 5, 10, 10, 1, 80)
}

func PodStartupProbe(i *infinispanv1.Infinispan) *corev1.Probe {
	// Maximum 10 minutes (60 * 10s) to finish startup
	return probe(i, 60, 10, 10, 1, 80)
}

func probe(i *infinispanv1.Infinispan, failureThreshold, initialDelay, period, successThreshold, timeout int32) *corev1.Probe {
	return &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTP,
				Path:   consts.ServerHTTPHealthStatusPath,
				Port:   intstr.FromInt(consts.InfinispanAdminPort)},
		},
		FailureThreshold:    failureThreshold,
		InitialDelaySeconds: initialDelay,
		PeriodSeconds:       period,
		SuccessThreshold:    successThreshold,
		TimeoutSeconds:      timeout,
	}
}

func PodResources(spec infinispanv1.InfinispanContainerSpec) (*corev1.ResourceRequirements, error) {
	memory, err := resource.ParseQuantity(spec.Memory)
	if err != nil {
		return nil, err
	}
	cpuRequests, cpuLimits, err := spec.GetCpuResources()
	if err != nil {
		return nil, err
	}
	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    *cpuRequests,
			corev1.ResourceMemory: memory,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    *cpuLimits,
			corev1.ResourceMemory: memory,
		},
	}, nil
}

func PodEnv(i *infinispanv1.Infinispan, systemEnv *[]corev1.EnvVar) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: consts.ServerConfigPath},
		// Prevent the image from generating a user if authentication disabled
		{Name: "MANAGED_ENV", Value: "TRUE"},
		{Name: "JAVA_OPTIONS", Value: i.GetJavaOptions()},
		{Name: "EXTRA_JAVA_OPTIONS", Value: i.Spec.Container.ExtraJvmOpts},
		{Name: "DEFAULT_IMAGE", Value: consts.DefaultImageName},
		{Name: "ADMIN_IDENTITIES_PATH", Value: consts.ServerAdminIdentitiesPath},
	}

	// Adding additional variables listed in ADDITIONAL_VARS env var
	envVar, defined := os.LookupEnv("ADDITIONAL_VARS")
	if defined {
		var addVars []string
		err := json.Unmarshal([]byte(envVar), &addVars)
		if err == nil {
			for _, name := range addVars {
				value, defined := os.LookupEnv(name)
				if defined {
					envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
				}
			}
		}
	}

	if i.IsAuthenticationEnabled() {
		envVars = append(envVars, corev1.EnvVar{Name: "IDENTITIES_PATH", Value: consts.ServerUserIdentitiesPath})
	}

	if systemEnv != nil {
		envVars = append(envVars, *systemEnv...)
	}

	return envVars
}

// Adding an init container that run chmod if needed
func AddVolumeChmodInitContainer(containerName, volumeName, mountPath string, spec *corev1.PodSpec) {
	if chmod, ok := os.LookupEnv("MAKE_DATADIR_WRITABLE"); ok && chmod == "true" {
		c := &spec.InitContainers
		*c = append(*c, chmodInitContainer(containerName, volumeName, mountPath))
	}
}

func chmodInitContainer(containerName, volumeName, mountPath string) corev1.Container {
	return corev1.Container{
		Image:   consts.InitContainerImageName,
		Name:    containerName,
		Command: []string{"sh", "-c", fmt.Sprintf("chmod -R g+w %s", mountPath)},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      volumeName,
			MountPath: mountPath,
		}},
	}
}

func AddVolumeForEncryption(i *infinispanv1.Infinispan, spec *corev1.PodSpec) bool {
	secret := i.GetEncryptionSecretName()
	if _, index := findSecretInVolume(spec, EncryptVolumeName); secret == "" || i.IsEncryptionDisabled() || index >= 0 {
		return false
	}

	v := &spec.Volumes
	*v = append(*v, corev1.Volume{Name: EncryptVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secret,
			},
		},
	})

	vm := &spec.Containers[0].VolumeMounts
	*vm = append(*vm, corev1.VolumeMount{
		Name:      EncryptVolumeName,
		MountPath: EncryptMountPath,
	})
	return true
}

func ConfigureServerEncryption(m *infinispanv1.Infinispan, c *config.InfinispanConfiguration, client client.Client) error {
	if m.IsEncryptionDisabled() {
		return nil
	}
	if m.IsEncryptionCertFromService() {
		if strings.Contains(m.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			c.Keystore.CrtPath = "/etc/encrypt"
			c.Keystore.Path = "/opt/infinispan/server/conf/keystore"
			c.Keystore.Password = "password"
			c.Keystore.Alias = "server"
			return nil
		}
	}
	// Fetch the tls secret if name is provided
	tlsSecretName := m.GetEncryptionSecretName()
	if tlsSecretName != "" {
		tlsSecret := &corev1.Secret{}
		err := client.Get(context.TODO(), types.NamespacedName{Namespace: m.Namespace, Name: tlsSecretName}, tlsSecret)
		if err != nil {
			if errors.IsNotFound(err) {
				return fmt.Errorf("Secret %s for endpoint encryption not found.", tlsSecretName)
			}
			return fmt.Errorf("Error in getting secret %s for endpoint encryption: %w", tlsSecretName, err)
		}
		if _, ok := tlsSecret.Data["keystore.p12"]; ok {
			// If user provide a keystore in secret then use it ...
			c.Keystore.Path = "/etc/encrypt/keystore.p12"
			c.Keystore.Password = string(tlsSecret.Data["password"])
			c.Keystore.Alias = string(tlsSecret.Data["alias"])
		} else {
			// ... else suppose tls.key and tls.crt are provided
			c.Keystore.CrtPath = "/etc/encrypt"
			c.Keystore.Path = "/opt/infinispan/server/conf/keystore"
			c.Keystore.Password = "password"
			c.Keystore.Alias = "server"
		}
	}
	return nil
}

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(pods []corev1.Pod, m *infinispanv1.Infinispan, cluster ispn.ClusterInterface) []infinispanv1.InfinispanCondition {
	var status []infinispanv1.InfinispanCondition
	clusterViews := make(map[string]bool)
	var errors []string
	// Avoid to inspect the system if we're still waiting for the pods
	if int32(len(pods)) < m.Spec.Replicas {
		errors = append(errors, fmt.Sprintf("Running %d pods. Needed %d", len(pods), m.Spec.Replicas))
	} else {
		for _, pod := range pods {
			if kube.IsPodReady(pod) {
				members, err := cluster.GetClusterMembers(pod.Name)
				if err == nil {
					sort.Strings(members)
					clusterView := strings.Join(members, ",")
					clusterViews[clusterView] = true
				} else {
					errors = append(errors, pod.Name+": "+err.Error())
				}
			} else {
				// Pod not ready, no need to query
				errors = append(errors, pod.Name+": pod not ready")
			}
		}
	}
	// Evaluating WellFormed condition
	wellformed := infinispanv1.InfinispanCondition{Type: infinispanv1.ConditionWellFormed}
	views := make([]string, len(clusterViews))
	i := 0
	for k := range clusterViews {
		views[i] = k
		i++
	}
	sort.Strings(views)
	if len(errors) == 0 {
		if len(views) == 1 {
			wellformed.Status = metav1.ConditionTrue
			wellformed.Message = "View: " + views[0]
		} else {
			wellformed.Status = metav1.ConditionFalse
			wellformed.Message = "Views: " + strings.Join(views, ",")
		}
	} else {
		wellformed.Status = metav1.ConditionUnknown
		wellformed.Message = "Errors: " + strings.Join(errors, ",") + " Views: " + strings.Join(views, ",")
	}
	status = append(status, wellformed)
	return status
}

func (r *ReconcileInfinispan) reconcileGracefulShutdown(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet,
	podList *corev1.PodList, logger logr.Logger, cluster ispn.ClusterInterface) (*reconcile.Result, error) {
	if ispn.Spec.Replicas == 0 {
		logger.Info(".Spec.Replicas==0")
		if *statefulSet.Spec.Replicas != 0 {
			logger.Info("StatefulSet.Spec.Replicas!=0")
			// If cluster hasn't a `stopping` condition or it's false then send a graceful shutdown
			if !ispn.IsConditionTrue(infinispanv1.ConditionStopping) {
				res, err := r.gracefulShutdownReq(ispn, podList, logger, cluster)
				if res != nil {
					return res, err
				}
			} else {
				// cluster has a `stopping` wait for all the pods becomes unready
				logger.Info("Waiting that all the pods become unready")
				for _, pod := range podList.Items {
					if kube.IsPodReady(pod) {
						logger.Info("One or more pods still ready", "Pod.Name", pod.Name)
						// Stop the work and requeue until cluster is down
						return &reconcile.Result{Requeue: true, RequeueAfter: time.Second}, nil
					}
				}
			}

			// If here all the pods are unready, set statefulset replicas and ispn.replicas to 0
			if err := r.update(ispn, func() {
				ispn.Status.ReplicasWantedAtRestart = *statefulSet.Spec.Replicas
			}); err != nil {
				return &reconcile.Result{}, err
			}
			statefulSet.Spec.Replicas = pointer.Int32Ptr(0)
			err := r.client.Update(context.TODO(), statefulSet)
			if err != nil {
				logger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
				return &reconcile.Result{}, err
			}
		}

		return &reconcile.Result{}, r.update(ispn, func() {
			if statefulSet.Status.CurrentReplicas == 0 {
				ispn.SetCondition(infinispanv1.ConditionGracefulShutdown, metav1.ConditionTrue, "")
				ispn.SetCondition(infinispanv1.ConditionStopping, metav1.ConditionFalse, "")
			}
		})
	}
	if ispn.Spec.Replicas != 0 && ispn.IsConditionTrue(infinispanv1.ConditionGracefulShutdown) {
		logger.Info("Resuming from graceful shutdown")
		// If here we're resuming from graceful shutdown
		if ispn.Spec.Replicas != ispn.Status.ReplicasWantedAtRestart {
			return &reconcile.Result{Requeue: true}, fmt.Errorf("Spec.Replicas(%d) must be 0 or equal to Status.ReplicasWantedAtRestart(%d)", ispn.Spec.Replicas, ispn.Status.ReplicasWantedAtRestart)
		}

		if err := r.update(ispn, func() {
			ispn.SetCondition(infinispanv1.ConditionGracefulShutdown, metav1.ConditionFalse, "")
			ispn.Status.ReplicasWantedAtRestart = 0
		}); err != nil {
			return &reconcile.Result{}, err
		}

		return &reconcile.Result{Requeue: true}, nil
	}
	return nil, nil
}

// gracefulShutdownReq send a graceful shutdown request to the cluster
func (r *ReconcileInfinispan) gracefulShutdownReq(ispn *infinispanv1.Infinispan, podList *corev1.PodList,
	logger logr.Logger, cluster ispn.ClusterInterface) (*reconcile.Result, error) {
	logger.Info("Sending graceful shutdown request")
	// send a graceful shutdown to the first ready pod
	// if there's no ready pod we're in trouble
	for _, pod := range podList.Items {
		if kube.IsPodReady(pod) {
			err := cluster.GracefulShutdown(pod.GetName())
			if err != nil {
				logger.Error(err, "failed to exec shutdown command on pod")
				continue
			}
			logger.Info("Executed graceful shutdown on pod: ", "Pod.Name", pod.Name)

			if err := r.update(ispn, func() {
				ispn.SetCondition(infinispanv1.ConditionStopping, metav1.ConditionTrue, "")
				ispn.SetCondition(infinispanv1.ConditionWellFormed, metav1.ConditionFalse, "")
			}); err != nil {
				return &reconcile.Result{}, err
			}
			// Stop the work and requeue until cluster is down
			return &reconcile.Result{Requeue: true, RequeueAfter: time.Second}, nil
		}
	}
	return nil, nil
}

// reconcileContainerConf reconcile the .Container struct is changed in .Spec. This needs a cluster restart
func (r *ReconcileInfinispan) reconcileContainerConf(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet, configMap *corev1.ConfigMap, adminSecret, userSecret *corev1.Secret, logger logr.Logger) (*reconcile.Result, error) {
	updateNeeded := false
	rollingUpgrade := true
	// Ensure the deployment size is the same as the spec
	replicas := ispn.Spec.Replicas
	previousReplicas := *statefulSet.Spec.Replicas
	if previousReplicas != replicas {
		statefulSet.Spec.Replicas = &replicas
		logger.Info("replicas changed, update infinispan", "replicas", replicas, "previous replicas", previousReplicas)
		updateNeeded = true
		rollingUpgrade = false
	}

	// Changes to statefulset.spec.template.spec.containers[].resources
	spec := &statefulSet.Spec.Template.Spec
	res := spec.Containers[0].Resources
	ispnContr := &ispn.Spec.Container
	if ispnContr.Memory != "" {
		quantity, err := resource.ParseQuantity(ispnContr.Memory)
		if err != nil {
			return &reconcile.Result{}, err
		}
		previousMemory := res.Requests["memory"]
		if quantity.Cmp(previousMemory) != 0 {
			res.Requests["memory"] = quantity
			res.Limits["memory"] = quantity
			logger.Info("memory changed, update infinispan", "memory", quantity, "previous memory", previousMemory)
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			updateNeeded = true
		}
	}
	if ispnContr.CPU != "" {
		cpuReq, cpuLim, err := ispn.Spec.Container.GetCpuResources()
		if err != nil {
			return &reconcile.Result{}, err
		}
		previousCPUReq := res.Requests["cpu"]
		previousCPULim := res.Limits["cpu"]
		if cpuReq.Cmp(previousCPUReq) != 0 || cpuLim.Cmp(previousCPULim) != 0 {
			res.Requests["cpu"] = *cpuReq
			res.Limits["cpu"] = *cpuLim
			logger.Info("cpu changed, update infinispan", "cpuLim", cpuLim, "cpuReq", cpuReq, "previous cpuLim", previousCPULim, "previous cpuReq", previousCPUReq)
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			updateNeeded = true
		}
	}

	if !reflect.DeepEqual(spec.Affinity, ispn.Spec.Affinity) {
		spec.Affinity = ispn.Spec.Affinity
		updateNeeded = true
	}

	// Validate ConfigMap changes (by the hash of the infinispan.yaml key value)
	updateNeeded = updateStatefulSetEnv(statefulSet, "CONFIG_HASH", hashString(configMap.Data[consts.ServerConfigFilename])) || updateNeeded
	updateNeeded = updateStatefulSetEnv(statefulSet, "ADMIN_IDENTITIES_HASH", hashString(string(adminSecret.Data[consts.ServerIdentitiesFilename]))) || updateNeeded

	// Validate identities Secret name changes
	if secretName, secretIndex := findSecretInVolume(&statefulSet.Spec.Template.Spec, IdentitiesVolumeName); secretIndex >= 0 && secretName != ispn.GetSecretName() {
		// Update new Secret name inside StatefulSet.Spec.Template
		statefulSet.Spec.Template.Spec.Volumes[secretIndex].Secret.SecretName = ispn.GetSecretName()
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		updateNeeded = true
	}

	if ispn.IsAuthenticationEnabled() {
		if AddVolumeForUserAuthentication(ispn, spec) {
			spec.Containers[0].Env = append(spec.Containers[0].Env,
				corev1.EnvVar{Name: "IDENTITIES_HASH", Value: hashString(string(userSecret.Data[consts.ServerIdentitiesFilename]))},
				corev1.EnvVar{Name: "IDENTITIES_PATH", Value: consts.ServerUserIdentitiesPath},
			)
			updateNeeded = true
		} else {
			// Validate Secret changes (by the hash of the identities.yaml key value)
			updateNeeded = updateStatefulSetEnv(statefulSet, "IDENTITIES_HASH", hashString(string(userSecret.Data[consts.ServerIdentitiesFilename]))) || updateNeeded
		}
	}

	updateNeeded = AddVolumeForEncryption(ispn, spec) || updateNeeded

	// Validate extra Java options changes
	if updateStatefulSetEnv(statefulSet, "EXTRA_JAVA_OPTIONS", ispnContr.ExtraJvmOpts) {
		updateStatefulSetEnv(statefulSet, "JAVA_OPTIONS", ispn.GetJavaOptions())
		updateNeeded = true
	}

	if updateNeeded {
		logger.Info("updateNeeded")
		// If updating the parameters results in a rolling upgrade, we can update the labels here too
		if rollingUpgrade {
			labelsForPod := PodLabels(ispn.Name)
			ispn.AddOperatorLabelsForPods(labelsForPod)
			ispn.AddLabelsForPods(labelsForPod)
			statefulSet.Spec.Template.Labels = labelsForPod
		}
		err := r.client.Update(context.TODO(), statefulSet)
		if err != nil {
			logger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
			return &reconcile.Result{}, err
		}
		// Spec updated - return and requeue
		return &reconcile.Result{Requeue: true}, nil
	}
	return nil, nil
}

func updateStatefulSetEnv(statefulSet *appsv1.StatefulSet, envName, newValue string) bool {
	env := &statefulSet.Spec.Template.Spec.Containers[0].Env
	envIndex := kube.GetEnvVarIndex(envName, env)
	prevEnvValue := (*env)[envIndex].Value
	if prevEnvValue != newValue {
		(*env)[envIndex].Value = newValue
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		return true
	}
	return false
}

func findSecretInVolume(pod *corev1.PodSpec, volumeName string) (string, int) {
	for i, volumes := range pod.Volumes {
		if volumes.Secret != nil && volumes.Name == volumeName {
			return volumes.Secret.SecretName, i
		}
	}
	return "", -1
}

// UpdateFn can contains logic required for Infinispan CR update.
// Mutations (Infinispan CR Spec and Status updates) are only possible in this function.
// All others updates outside this functions will be skipped.
type UpdateFn func()

func (r *ReconcileInfinispan) update(ispn *infinispanv1.Infinispan, update UpdateFn, ignoreNotFound ...bool) error {
	_, err := kube.CreateOrPatch(context.TODO(), r.client, ispn, func() error {
		if ispn.CreationTimestamp.IsZero() {
			return errors.NewNotFound(infinispanv1.Resource("infinispan.org"), ispn.Name)
		}
		if update != nil {
			update()
		}
		return nil
	})
	if len(ignoreNotFound) == 0 || (len(ignoreNotFound) > 0 && ignoreNotFound[0]) && errors.IsNotFound(err) {
		return nil
	}
	return err
}

// LabelsResource returns the labels that must me applied to the resource
func LabelsResource(name, resourceType string) map[string]string {
	m := map[string]string{"infinispan_cr": name, "clusterName": name}
	if resourceType != "" {
		m["app"] = resourceType
	}
	return m
}

func PodLabels(name string) map[string]string {
	return LabelsResource(name, "infinispan-pod")
}

func ServiceLabels(name string) map[string]string {
	return map[string]string{
		"clusterName": name,
		"app":         "infinispan-pod",
	}
}

func hashString(data string) string {
	hash := sha1.New()
	hash.Write([]byte(data))
	return hex.EncodeToString(hash.Sum(nil))
}

// TODO Replace with controllerutil.ContainsFinalizer after operator-sdk update (controller-runtime v0.6.1+)
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TODO Replace with controllerutil.RemoveFinalizer after operator-sdk update (controller-runtime v0.6.0+)
func remove(list []string, s string) []string {
	var slice []string
	for _, v := range list {
		if v != s {
			slice = append(slice, v)
		}
	}
	return slice
}
