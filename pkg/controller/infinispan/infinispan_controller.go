package infinispan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/caches"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/version"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/discovery"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	DataMountPath        = "/opt/infinispan/server/data"
	EncryptMountPath     = "/etc/encrypt"
	ConfigVolumeName     = "config-volume"
	EncryptVolumeName    = "encrypt-volume"
	IdentitiesVolumeName = "identities-volume"
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
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &infinispanv1.Infinispan{},
	})
	if err != nil {
		return err
	}
	return nil
}

// TODO: If we will invoke getDiscoveryClient multiple times in one reconcile stage, it's necessary move 'discovery' to the ReconcileInfinispan struct due to performance reasons
func (r *ReconcileInfinispan) getDiscoveryClient() (discovery.DiscoveryInterface, error) {
	discovery, err := discovery.NewDiscoveryClientForConfig(kubernetes.RestConfig)
	return discovery, err
}

func (r *ReconcileInfinispan) isGroupVersionSupported(groupVersion string, kind string) (bool, error) {
	cli, err := r.getDiscoveryClient()
	if err != nil {
		log.Error(err, "Failed to return a discovery client for the current reconciler")
		return false, err
	}

	res, err := cli.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, v := range res.APIResources {
		if v.Kind == kind {
			return true, nil
		}
	}

	return false, nil
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
	err := r.client.Get(context.TODO(), request.NamespacedName, infinispan)
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

	// Apply defaults if not already set
	infinispan.ApplyDefaults()

	// Perform all the possible preliminary checks before go on
	result, err := infinispan.PreliminaryChecks()
	if err != nil {
		if infinispan.SetCondition(infinispanv1.ConditionPrelimChecksPassed, metav1.ConditionFalse, err.Error()) {
			err1 := r.client.Status().Update(context.TODO(), infinispan)
			if err1 != nil {
				reqLogger.Error(err1, "Could not update error conditions")
			}
		}
		return *result, err
	}
	if infinispan.SetCondition(infinispanv1.ConditionPrelimChecksPassed, metav1.ConditionTrue, "") {
		err = r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "Could not update error conditions")
		}
	}

	infinispan.ApplyEndpointEncryptionSettings(kubernetes.GetServingCertsMode(), reqLogger)

	xsite := &config.XSite{}
	if infinispan.HasSites() {

		// Check x-site configuration first.
		// Must be done before creating any Infinispan resources,
		// because remote site host:port combinations need to be injected into Infinispan.

		// For cross site, reconcile must come before compute, because
		// we need xsite service details to compute xsite struct
		siteService, err := r.reconcileXSite(infinispan, r.scheme, reqLogger)
		if err != nil {
			reqLogger.Error(err, "Error in reconcileXSite", "Infinispan.Namespace", infinispan.Namespace)
			return reconcile.Result{}, err
		}

		err = ComputeXSite(infinispan, kubernetes, siteService, reqLogger, xsite)
		if err != nil {
			reqLogger.Error(err, "Error in computeXSite", "Infinispan.Namespace", infinispan.Namespace)
			return reconcile.Result{}, err
		}
	}

	configMap, err := r.computeConfigMap(xsite, infinispan)
	if err != nil {
		reqLogger.Error(err, "could not create Infinispan configuration")
		return reconcile.Result{}, err
	}

	err = r.reconcileConfigMap(infinispan, configMap, reqLogger)
	if err != nil {
		reqLogger.Error(err, "Error in reconcileConfigMap")
		return reconcile.Result{}, err
	}

	// Reconcile the StatefulSet
	// Check if the StatefulSet already exists, if not create a new one
	statefulSet := &appsv1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, statefulSet)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Configuring the StatefulSet")
		var secret *corev1.Secret
		secret, err = r.findSecret(infinispan)
		if err != nil && infinispan.Spec.Security.EndpointSecretName != "" {
			reqLogger.Error(err, "could not find secret", "Secret.Name", infinispan.Spec.Security.EndpointSecretName)
			return reconcile.Result{}, err
		}

		if secret == nil {
			reqLogger.Info("Creating identity secret")
			// Generate the identities secret if not provided by the user
			identities, err := users.GetCredentials()
			if err != nil {
				reqLogger.Error(err, "could not get identities for Secret")
				return reconcile.Result{}, err
			}

			secret = r.secretForInfinispan(identities, infinispan)
			reqLogger.Info("Creating a new Secret", "Secret.Name", secret.Name)
			err = r.client.Create(context.TODO(), secret)
			if err != nil {
				reqLogger.Error(err, "could not create a new Secret")
				return reconcile.Result{}, err
			}
			// Set the endpoint secret name and update .Spec.Security
			infinispan.Spec.Security.EndpointSecretName = secret.GetName()
			err = r.client.Update(context.TODO(), infinispan)
			if err != nil {
				reqLogger.Error(err, "failed to update Infinispan Spec")
				return reconcile.Result{}, err
			}
		}

		if err = updateSecurity(infinispan, r.client, reqLogger); err != nil {
			return reconcile.Result{}, err
		}

		// Define a new StatefulSet
		statefulSet, err = r.statefulSetForInfinispan(infinispan, secret, configMap)
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
		reqLogger.Info("End of the StetefulSet creation")
	}
	if err != nil {
		reqLogger.Error(err, "failed to get StatefulSet")
		return reconcile.Result{}, err
	}

	ser := r.computeService(infinispan)
	setupServiceForEncryption(infinispan, ser)

	err = r.reconcileService(infinispan, ser, reqLogger)
	if err != nil {
		reqLogger.Error(err, "Error in reconcileService")
		return reconcile.Result{}, err
	}

	pingService := r.computePingService(infinispan)

	err = r.reconcileService(infinispan, pingService, reqLogger)
	if err != nil {
		reqLogger.Error(err, "Error in reconcileService for ping DNS")
		return reconcile.Result{}, err
	}

	if infinispan.IsExposed() {
		switch infinispan.Spec.Expose.Type {
		case infinispanv1.ExposeTypeLoadBalancer, infinispanv1.ExposeTypeNodePort:
			externalService := r.computeServiceExternal(infinispan)
			result, err := r.reconcileExternalService(infinispan, externalService, reqLogger)
			if result != nil {
				return *result, err
			}
		case infinispanv1.ExposeTypeRoute:
			ok, err := r.isGroupVersionSupported(routev1.GroupVersion.String(), "Route")
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Failed to check if %s is supported", routev1.GroupVersion.String()))
				// Log an error and try to go on with Ingress
				ok = false
			}
			if ok {
				route := r.computeRoute(infinispan)
				result, err := r.reconcileRoute(infinispan, route, reqLogger)
				if result != nil {
					return *result, err
				}
			} else {
				ingress := r.computeIngress(infinispan)
				result, err := r.reconcileIngress(infinispan, ingress, reqLogger)
				if result != nil {
					return *result, err
				}
			}
		}
	}

	if infinispan.IsUpgradeNeeded(reqLogger) {
		reqLogger.Info("Upgrade needed")
		err = r.destroyResources(infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to delete resources before upgrade")
			return reconcile.Result{}, err
		}

		infinispan.SetCondition(infinispanv1.ConditionUpgrade, metav1.ConditionFalse, "")
		err = r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan status")
			return reconcile.Result{}, err
		}

		reqLogger.Info("removed Infinispan resources, force an upgrade now", "replicasWantedAtRestart", infinispan.Status.ReplicasWantedAtRestart)

		infinispan.Spec.Replicas = infinispan.Status.ReplicasWantedAtRestart
		err = r.client.Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan Spec")
			return reconcile.Result{}, err
		}

		return reconcile.Result{Requeue: true}, nil
	}

	// List the pods for this infinispan's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(PodLabels(infinispan.Name))
	listOps := &client.ListOptions{Namespace: infinispan.Namespace, LabelSelector: labelSelector}
	err = r.client.List(context.TODO(), podList, listOps)
	if err != nil {
		reqLogger.Error(err, "failed to list pods", "Infinispan.Namespace")
		return reconcile.Result{}, err
	}

	result, err = r.scheduleUpgradeIfNeeded(infinispan, podList, reqLogger)
	if result != nil {
		return *result, err
	}

	user := consts.DefaultOperatorUser
	pass, err := users.PasswordFromSecret(user, infinispan.GetSecretName(), infinispan.GetNamespace(), kubernetes)
	if err != nil {
		return reconcile.Result{}, err
	}

	cluster := ispn.NewCluster(user, pass, request.Namespace, infinispan.GetEndpointScheme(), kubernetes)

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

	// If secretName for identities has changed reprocess all the
	// identities secrets and then upgrade the cluster
	res, err = r.reconcileEndpointSecret(infinispan, statefulSet, reqLogger)
	if res != nil {
		return *res, err
	}

	// Here where to reconcile with spec updates that reflect into
	// changes to statefulset.spec.container.
	res, err = r.reconcileContainerConf(infinispan, statefulSet, configMap, reqLogger)
	if res != nil {
		return *res, err
	}

	// If x-site enable configure the coordinator pods to be selected by the x-site service
	if infinispan.HasSites() {
		found := applyLabelsToCoordinatorsPod(podList, cluster, r.client, reqLogger)
		if !found {
			// If a coordinator is not found then requeue early
			return reconcile.Result{Requeue: true}, nil
		}
	}

	// Update the Infinispan status with the pod status
	// Wait until all pods have ips assigned
	// Without those ips, it's not possible to execute next calls
	if !kube.ArePodIPsReady(podList) {
		reqLogger.Info("Pods IPs are not ready yet")
		return reconcile.Result{}, nil
	}

	// All pods ready start autoscaler if needed
	if infinispan.Spec.Autoscale != nil && infinispan.IsCache() {
		addAutoscalingEquipment(types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, r)
	}
	// Inspect the system and get the current Infinispan conditions
	currConds := getInfinispanConditions(podList.Items, infinispan, string(infinispan.GetEndpointScheme()), cluster)

	// Before updating reload the resource to avoid problems with status update
	err = r.client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, infinispan)
	if err != nil {
		reqLogger.Error(err, "failed to reload Infinispan status")
		return reconcile.Result{}, err
	}

	// Update the Infinispan status with the pod status
	changed := infinispan.SetConditions(currConds)
	infinispan.Status.StatefulSetName = statefulSet.ObjectMeta.Name
	if changed {
		err := r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan status")
			return reconcile.Result{}, err
		}
	}

	// View didn't form, requeue until view has formed
	if infinispan.NotClusterFormed(len(podList.Items), int(infinispan.Spec.Replicas)) {
		reqLogger.Info("notClusterFormed")
		return reconcile.Result{Requeue: true, RequeueAfter: consts.DefaultWaitClusterNotWellFormed}, nil
	}

	// Below the code for a wellFormed cluster

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

func updateSecurity(infinispan *infinispanv1.Infinispan, client client.Client, reqLogger logr.Logger) error {
	err := client.Update(context.TODO(), infinispan)
	if err != nil {
		reqLogger.Error(err, "failed to update Infinispan Spec")
		return err
	}
	// Copy and update .Status.Security to match .Spec.Security
	infinispan.Spec.Security.DeepCopyInto(&infinispan.Status.Security)
	err = client.Status().Update(context.TODO(), infinispan)
	if err != nil {
		reqLogger.Error(err, "failed to update Infinispan Status", "Infinispan.Namespace", infinispan.Namespace)
		return err
	}
	reqLogger.Info("Security set",
		"Spec.EndpointSecretName", infinispan.Spec.Security.EndpointSecretName,
		"Status.EndpointSecretName", infinispan.Status.Security.EndpointSecretName)
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
				Name:      infinispan.ObjectMeta.Name,
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ServerConfigMapName(infinispan.ObjectMeta.Name),
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.ObjectMeta.Name,
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.ObjectMeta.Name + "-ping",
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetServiceExternalName(),
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetSiteServiceName(),
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func (r *ReconcileInfinispan) upgradeInfinispan(infinispan *infinispanv1.Infinispan) error {
	updateNeeded := false
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

		// Remove finalizer if it defined in the Infinispan CR
		infinispan.SetFinalizers(remove(infinispan.GetFinalizers(), consts.InfinispanFinalizer))
		updateNeeded = true
	}

	// Remove defined by default Spec.Image field for upgrade backward compatibility
	if infinispan.Spec.Image != nil {
		infinispan.Spec.Image = nil
		updateNeeded = true
	}
	sc := infinispan.Spec.Service.Container
	// Remove defined to "" Spec.Service.Container.Storage field for upgrade backward compatibility
	if sc != nil && sc.Storage != nil && *infinispan.Spec.Service.Container.Storage == "" {
		infinispan.Spec.Service.Container.Storage = nil
		updateNeeded = true
	}
	if updateNeeded {
		err := r.client.Update(context.TODO(), infinispan)
		if err != nil {
			return err
		}
	}
	return nil
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
		infinispan.Spec.Replicas = 0
		err := r.client.Update(context.TODO(), infinispan)
		if err != nil {
			logger.Error(err, "failed to update Infinispan Spec")
			return &reconcile.Result{}, err
		}
		infinispan.SetCondition(infinispanv1.ConditionUpgrade, metav1.ConditionTrue, "")
		err = r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			logger.Error(err, "failed to update Infinispan Status")
			return &reconcile.Result{}, err
		}
	}
	return nil, nil
}

// deploymentForInfinispan returns an infinispan Deployment object
func (r *ReconcileInfinispan) statefulSetForInfinispan(m *infinispanv1.Infinispan, secret *corev1.Secret, configMap *corev1.ConfigMap) (*appsv1.StatefulSet, error) {
	reqLogger := log.WithValues("Request.Namespace", m.Namespace, "Request.Name", m.Name)
	lsPod := PodLabels(m.ObjectMeta.Name)

	memory := resource.MustParse(m.Spec.Container.Memory)

	replicas := m.Spec.Replicas
	dep := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        m.ObjectMeta.Name,
			Namespace:   m.ObjectMeta.Namespace,
			Annotations: consts.DeploymentAnnotations,
			Labels:      map[string]string{"template": "infinispan-ephemeral"},
		},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: lsPod,
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      lsPod,
					Annotations: map[string]string{"updateDate": time.Now().String()},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:          m.ImageName(),
						Name:           "infinispan",
						Env:            PodEnv(m, &[]corev1.EnvVar{{Name: "CONFIG_HASH", Value: sha256String(configMap.Data[consts.ServerConfigFilename])}}),
						LivenessProbe:  PodLivenessProbe(m),
						Ports:          PodPortsWithXsite(m),
						ReadinessProbe: PodReadinessProbe(m),
						Resources:      PodResources(m.Spec.Container),
						VolumeMounts: []corev1.VolumeMount{{
							Name:      ConfigVolumeName,
							MountPath: consts.ServerConfigRoot,
						}, {
							Name:      IdentitiesVolumeName,
							MountPath: consts.ServerSecurityRoot,
						}, {
							Name:      m.Name,
							MountPath: DataMountPath,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: ConfigVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: configMap.Name},
							},
						},
					}, {
						Name: IdentitiesVolumeName,
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: secret.Name,
							},
						},
					}},
				},
			},
		},
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
			Name:      m.ObjectMeta.Name,
			Namespace: m.ObjectMeta.Namespace,
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

		AddServerDataChmodInitContainer(m.ObjectMeta.Name, &dep.Spec.Template.Spec)
	} else {
		volumes := &dep.Spec.Template.Spec.Volumes
		ephemeralVolume := corev1.Volume{
			Name: m.Name,
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

func PodPorts(i *infinispanv1.Infinispan) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{ContainerPort: consts.InfinispanPingPort, Name: consts.InfinispanPingPortName, Protocol: corev1.ProtocolTCP},
		{ContainerPort: consts.InfinispanPort, Name: consts.InfinispanPortName, Protocol: corev1.ProtocolTCP},
	}
	return ports
}

func PodPortsWithXsite(i *infinispanv1.Infinispan) []corev1.ContainerPort {
	ports := PodPorts(i)
	if i.HasSites() {
		ports = append(ports, corev1.ContainerPort{ContainerPort: consts.CrossSitePort, Name: consts.CrossSitePortName, Protocol: corev1.ProtocolTCP})
	}
	return ports
}

func PodLivenessProbe(i *infinispanv1.Infinispan) *corev1.Probe {
	return &corev1.Probe{
		Handler:             ispn.ClusterStatusHandler(i.GetEndpointScheme()),
		FailureThreshold:    5,
		InitialDelaySeconds: 10,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      80,
	}
}

func PodReadinessProbe(i *infinispanv1.Infinispan) *corev1.Probe {
	return &corev1.Probe{
		Handler:             ispn.ClusterStatusHandler(i.GetEndpointScheme()),
		FailureThreshold:    5,
		InitialDelaySeconds: 10,
		PeriodSeconds:       60,
		SuccessThreshold:    1,
		TimeoutSeconds:      80,
	}
}

func PodResources(spec infinispanv1.InfinispanContainerSpec) corev1.ResourceRequirements {
	memory := resource.MustParse(spec.Memory)
	cpuRequests, cpuLimits := spec.GetCpuResources()
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    cpuRequests,
			corev1.ResourceMemory: memory,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    cpuLimits,
			corev1.ResourceMemory: memory,
		},
	}
}

func PodEnv(i *infinispanv1.Infinispan, systemEnv *[]corev1.EnvVar) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: consts.ServerConfigPath},
		{Name: "IDENTITIES_PATH", Value: consts.ServerIdentitiesPath},
		{Name: "JAVA_OPTIONS", Value: i.GetJavaOptions()},
		{Name: "EXTRA_JAVA_OPTIONS", Value: i.Spec.Container.ExtraJvmOpts},
		{Name: "DEFAULT_IMAGE", Value: consts.DefaultImageName},
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

	if systemEnv != nil {
		envVars = append(envVars, *systemEnv...)
	}

	return envVars
}

// Adding an init container that run chmod if needed
func AddServerDataChmodInitContainer(volumeName string, spec *corev1.PodSpec) {
	if chmod, ok := os.LookupEnv("MAKE_DATADIR_WRITABLE"); ok && chmod == "true" {
		c := &spec.InitContainers
		*c = append(*c, ChmodInitContainer("chmod-pv", volumeName, DataMountPath))
	}
}

func ChmodInitContainer(containerName, volumeName, mountPath string) corev1.Container {
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

func setupServiceForEncryption(m *infinispanv1.Infinispan, ser *corev1.Service) {
	if m.IsEncryptionCertFromService() {
		if strings.Contains(m.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			// Using platform service. Only OpenShift is integrated atm
			secretName := m.GetEncryptionSecretName()
			if ser.ObjectMeta.Annotations == nil {
				ser.ObjectMeta.Annotations = map[string]string{}
			}
			ser.ObjectMeta.Annotations[m.Spec.Security.EndpointEncryption.CertServiceName+"/serving-cert-secret-name"] = secretName
		}
	}
}

func AddVolumeForEncryption(i *infinispanv1.Infinispan, pod *corev1.PodSpec) {
	secret := i.GetEncryptionSecretName()
	if secret == "" {
		return
	}

	v := &pod.Volumes
	*v = append(*v, corev1.Volume{Name: EncryptVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secret,
			},
		},
	})

	vm := &pod.Containers[0].VolumeMounts
	*vm = append(*vm, corev1.VolumeMount{
		Name:      EncryptVolumeName,
		MountPath: EncryptMountPath,
	})
}

func ConfigureServerEncryption(m *infinispanv1.Infinispan, c *config.InfinispanConfiguration, client client.Client) error {
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

func (r *ReconcileInfinispan) computeConfigMap(xsite *config.XSite, m *infinispanv1.Infinispan) (*corev1.ConfigMap, error) {
	name := m.ObjectMeta.Name
	namespace := m.ObjectMeta.Namespace

	loggingCategories := m.GetLogCategoriesForConfigMap()
	config := config.CreateInfinispanConfiguration(name, loggingCategories, namespace, xsite)
	// Explicitly set the numner of lock owners in order for zero-capacity nodes to be able to utilise clustered locks
	config.Infinispan.Locks.Owners = m.Spec.Replicas

	err := ConfigureServerEncryption(m, &config, r.client)
	if err != nil {
		return nil, err
	}
	configYaml, err := config.Yaml()
	if err != nil {
		return nil, err
	}
	lsConfigMap := LabelsResource(m.ObjectMeta.Name, "infinispan-configmap-configuration")
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServerConfigMapName(name),
			Namespace: namespace,
			Labels:    lsConfigMap,
		},
		Data: map[string]string{consts.ServerConfigFilename: string(configYaml)},
	}

	return configMap, nil
}

func ServerConfigMapName(name string) string {
	return name + "-configuration"
}

func (r *ReconcileInfinispan) findSecret(m *infinispanv1.Infinispan) (*corev1.Secret, error) {
	secretName := m.GetSecretName()
	return kubernetes.GetSecret(secretName, m.Namespace)
}

func (r *ReconcileInfinispan) secretForInfinispan(identities []byte, m *infinispanv1.Infinispan) *corev1.Secret {
	lsSecret := LabelsResource(m.ObjectMeta.Name, "infinispan-secret-identities")
	secretName := m.GetSecretName()
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsSecret,
		},
		Type:       corev1.SecretType("Opaque"),
		StringData: map[string]string{consts.ServerIdentitiesFilename: string(identities)},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, secret, r.scheme)
	return secret
}

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(pods []corev1.Pod, m *infinispanv1.Infinispan, protocol string, cluster ispn.ClusterInterface) []infinispanv1.InfinispanCondition {
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

func (r *ReconcileInfinispan) computeService(m *infinispanv1.Infinispan) *corev1.Service {
	lsService := LabelsResource(m.ObjectMeta.Name, "infinispan-service")
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name,
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsService,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: ServiceLabels(m.ObjectMeta.Name),
			Ports: []corev1.ServicePort{
				{
					Name: consts.InfinispanPortName,
					Port: consts.InfinispanPort,
				},
			},
		},
	}

	return service
}

func (r *ReconcileInfinispan) computePingService(m *infinispanv1.Infinispan) *corev1.Service {
	lsService := LabelsResource(m.ObjectMeta.Name, "infinispan-service-ping")
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name + "-ping",
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsService,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector:  ServiceLabels(m.ObjectMeta.Name),
			Ports: []corev1.ServicePort{
				{
					Name: consts.InfinispanPingPortName,
					Port: consts.InfinispanPingPort,
				},
			},
		},
	}

	return service
}

// computeServiceExternal compute the external service
func (r *ReconcileInfinispan) computeServiceExternal(m *infinispanv1.Infinispan) *corev1.Service {
	externalServiceType := corev1.ServiceType(m.Spec.Expose.Type)
	externalServiceName := m.GetServiceExternalName()
	exposeConf := m.Spec.Expose

	metadata := metav1.ObjectMeta{
		Name:      externalServiceName,
		Namespace: m.ObjectMeta.Namespace,
		Labels:    ExternalServiceLabels(m.ObjectMeta.Name),
	}
	if exposeConf.Annotations != nil && len(exposeConf.Annotations) > 0 {
		metadata.Annotations = exposeConf.Annotations
	}

	exposeSpec := corev1.ServiceSpec{
		Type:     externalServiceType,
		Selector: ServiceLabels(m.ObjectMeta.Name),
		Ports: []corev1.ServicePort{
			{
				Port:       int32(consts.InfinispanPort),
				TargetPort: intstr.FromInt(consts.InfinispanPort),
			},
		},
	}
	if exposeConf.NodePort > 0 {
		exposeSpec.Ports[0].NodePort = exposeConf.NodePort
	}

	externalService := &corev1.Service{
		ObjectMeta: metadata,
		Spec:       exposeSpec,
	}

	return externalService
}

// reconcileXSite creates the xsite service if needed
func (r *ReconcileInfinispan) reconcileXSite(ispn *infinispanv1.Infinispan, scheme *runtime.Scheme, logger logr.Logger) (*corev1.Service, error) {
	siteServiceName := ispn.GetSiteServiceName()
	siteService, err := GetOrCreateSiteService(siteServiceName, ispn, kubernetes.Client, scheme, logger)
	if err != nil {
		logger.Error(err, "could not get or create site service")
		return nil, err
	}
	return siteService, nil
}

// reconcileConfigMap creates the configMap for Infinispan if needed
func (r *ReconcileInfinispan) reconcileConfigMap(ispn *infinispanv1.Infinispan, configMap *corev1.ConfigMap, logger logr.Logger) error {
	configMapObject := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMap.Name,
			Namespace: configMap.Namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(context.TODO(), r.client, configMapObject, func() error {
		if configMapObject.CreationTimestamp.IsZero() {
			configMapObject.Data = configMap.Data
			configMapObject.Annotations = configMap.Annotations
			configMapObject.Labels = configMap.Labels
			// Set Infinispan instance as the owner and controller
			controllerutil.SetControllerReference(ispn, configMapObject, r.scheme)
		} else {
			configMapObject.Data[consts.ServerConfigFilename] = configMap.Data[consts.ServerConfigFilename]
		}
		return nil
	})
	if err == nil && (result == controllerutil.OperationResultCreated || result == controllerutil.OperationResultUpdated) {
		logger.Info(fmt.Sprintf("ConfigMap %s %s", configMap.Name, result))
	}
	return err
}

// reconcileService creates the service for Infinispan if needed
func (r *ReconcileInfinispan) reconcileService(ispn *infinispanv1.Infinispan, service *corev1.Service, logger logr.Logger) error {
	s := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, s)
	if errors.IsNotFound(err) {
		// Set Infinispan instance as the owner and controller
		controllerutil.SetControllerReference(ispn, service, r.scheme)
		err := r.client.Create(context.TODO(), service)
		if err != nil && !errors.IsAlreadyExists(err) {
			logger.Error(err, "failed to create Service", "Service", service)
			return err
		}
		return nil
	}
	return err
}

// reconcileExternalService creates the external service if needed
func (r *ReconcileInfinispan) reconcileExternalService(ispn *infinispanv1.Infinispan, service *corev1.Service, logger logr.Logger) (*reconcile.Result, error) {
	s := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: service.Namespace, Name: service.Name}, s)
	if errors.IsNotFound(err) {
		// Set Infinispan instance as the owner and controller
		controllerutil.SetControllerReference(ispn, service, r.scheme)
		err = r.client.Create(context.TODO(), service)
		if err != nil {
			logger.Error(err, "failed to create external Service", "Service", service)
			return &reconcile.Result{Requeue: true, RequeueAfter: consts.DefaultRequeueOnCreateExposeServiceDelay}, nil
		}
		if len(service.Spec.Ports) > 0 {
			ispn.Spec.Expose.NodePort = service.Spec.Ports[0].NodePort
			err = r.client.Update(context.TODO(), ispn)
			if err != nil {
				logger.Info("Failed to update Infinispan with service nodePort", "Service", service)
			}
		}
		logger.Info("Created External Service", "Service", service)
	}
	return nil, err
}

// computeRoute compute the Route object
func (r *ReconcileInfinispan) computeRoute(ispn *infinispanv1.Infinispan) *routev1.Route {
	route := routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    ExternalServiceLabels(ispn.ObjectMeta.Name),
		},
		Spec: routev1.RouteSpec{
			Host: ispn.Spec.Expose.Host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: ispn.Name},
			TLS: &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough},
		},
	}
	return &route
}

// reconcileRoute creates the external Route if needed
func (r *ReconcileInfinispan) reconcileRoute(ispn *infinispanv1.Infinispan, route *routev1.Route, logger logr.Logger) (*reconcile.Result, error) {
	ro := &routev1.Route{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: route.Namespace, Name: route.Name}, ro)
	if errors.IsNotFound(err) {
		controllerutil.SetControllerReference(ispn, route, r.scheme)
		err = r.client.Create(context.TODO(), route)
		if err != nil {
			logger.Error(err, "failed to create Route", "Route", route)
			return &reconcile.Result{Requeue: true, RequeueAfter: consts.DefaultRequeueOnCreateExposeServiceDelay}, nil
		}
		if route.Spec.Host != "" {
			ispn.Spec.Expose.Host = route.Spec.Host
			err = r.client.Update(context.TODO(), ispn)
			if err != nil {
				logger.Info("Failed to update Infinispan with route Host", "Route", route)
			}
		}
	}
	return nil, err
}

// computeIngress compute the Ingress object
func (r *ReconcileInfinispan) computeIngress(ispn *infinispanv1.Infinispan) *networkingv1beta1.Ingress {
	ingress := networkingv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    ExternalServiceLabels(ispn.ObjectMeta.Name),
		},
		Spec: networkingv1beta1.IngressSpec{
			TLS: []networkingv1beta1.IngressTLS{},
			Rules: []networkingv1beta1.IngressRule{
				{
					Host: ispn.Spec.Expose.Host,
					IngressRuleValue: networkingv1beta1.IngressRuleValue{
						HTTP: &networkingv1beta1.HTTPIngressRuleValue{
							Paths: []networkingv1beta1.HTTPIngressPath{
								{
									Path: "/",
									Backend: networkingv1beta1.IngressBackend{
										ServiceName: ispn.Name,
										ServicePort: intstr.IntOrString{IntVal: consts.InfinispanPort}}}}},
					}}},
		}}
	if ispn.GetEncryptionSecretName() != "" {
		ingress.Spec.TLS = []networkingv1beta1.IngressTLS{
			{
				Hosts: []string{ispn.Spec.Expose.Host},
			}}
	}
	return &ingress
}

// reconcileIngress creates the external Route if needed
func (r *ReconcileInfinispan) reconcileIngress(ispn *infinispanv1.Infinispan, ingress *networkingv1beta1.Ingress, logger logr.Logger) (*reconcile.Result, error) {
	ing := &networkingv1beta1.Ingress{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: ingress.Namespace, Name: ingress.Name}, ing)
	if errors.IsNotFound(err) {
		controllerutil.SetControllerReference(ispn, ingress, r.scheme)
		err = r.client.Create(context.TODO(), ingress)
		if err != nil {
			logger.Error(err, "failed to create Ingress", "Ingress", ingress)
			return &reconcile.Result{Requeue: true, RequeueAfter: consts.DefaultRequeueOnCreateExposeServiceDelay}, nil
		}
		if len(ingress.Spec.Rules) > 0 && ingress.Spec.Rules[0].Host != "" {
			ispn.Spec.Expose.Host = ingress.Spec.Rules[0].Host
			err = r.client.Update(context.TODO(), ispn)
			if err != nil {
				logger.Info("Failed to update Infinispan with ingress Host", "Route", ingress)
			}
		}
	}
	return nil, err
}

func (r *ReconcileInfinispan) reconcileGracefulShutdown(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet,
	podList *corev1.PodList, logger logr.Logger, cluster ispn.ClusterInterface) (*reconcile.Result, error) {
	if ispn.Spec.Replicas == 0 {
		logger.Info(".Spec.Replicas==0")
		updateStatus := false
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
			ispn.Status.ReplicasWantedAtRestart = *statefulSet.Spec.Replicas
			err := r.client.Status().Update(context.TODO(), ispn)
			if err != nil {
				logger.Error(err, "failed to update Infinispan status")
				return &reconcile.Result{}, err
			}
			zeroReplicas := int32(0)
			statefulSet.Spec.Replicas = &zeroReplicas
			err = r.client.Update(context.TODO(), statefulSet)
			if err != nil {
				logger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
				return &reconcile.Result{}, err
			}
		}
		if statefulSet.Status.CurrentReplicas == 0 {
			updateStatus = ispn.SetCondition(infinispanv1.ConditionGracefulShutdown, metav1.ConditionTrue, "") || updateStatus
			updateStatus = ispn.SetCondition(infinispanv1.ConditionStopping, metav1.ConditionFalse, "") || updateStatus
		}
		if updateStatus {
			err := r.client.Status().Update(context.TODO(), ispn)
			if err != nil {
				logger.Error(err, "failed to update Infinispan Status")
				return &reconcile.Result{}, err
			}
		}
		return &reconcile.Result{}, nil
	}
	if ispn.Spec.Replicas != 0 && ispn.IsConditionTrue(infinispanv1.ConditionGracefulShutdown) {
		logger.Info("Resuming from graceful shutdown")
		// If here we're resuming from graceful shutdown
		err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, ispn)
		if err != nil {
			logger.Error(err, "failed to get Infinispan")
			return &reconcile.Result{}, err
		}
		if ispn.Spec.Replicas != ispn.Status.ReplicasWantedAtRestart {
			return &reconcile.Result{Requeue: true}, fmt.Errorf("Spec.Replicas(%d) must be 0 or equal to Status.ReplicasWantedAtRestart(%d)", ispn.Spec.Replicas, ispn.Status.ReplicasWantedAtRestart)
		}
		ispn.Status.ReplicasWantedAtRestart = 0
		ispn.SetCondition(infinispanv1.ConditionGracefulShutdown, metav1.ConditionFalse, "")
		err = r.client.Status().Update(context.TODO(), ispn)
		if err != nil {
			logger.Error(err, "failed to update Infinispan Status")
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
			ispn.SetCondition(infinispanv1.ConditionStopping, metav1.ConditionTrue, "")
			ispn.SetCondition(infinispanv1.ConditionWellFormed, metav1.ConditionFalse, "")
			err = r.client.Status().Update(context.TODO(), ispn)
			if err != nil {
				logger.Error(err, "This should not happens but failed to update Infinispan Status")
				return &reconcile.Result{}, err
			}
			// Stop the work and requeue until cluster is down
			return &reconcile.Result{Requeue: true, RequeueAfter: time.Second}, nil
		}
	}
	return nil, nil
}

// reconcileEndpointSecret reconcile the enpointSecretName is changed in .Spec. This needs a cluster restart
func (r *ReconcileInfinispan) reconcileEndpointSecret(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet, logger logr.Logger) (*reconcile.Result, error) {
	if ispn.Spec.Security.EndpointSecretName != ispn.Status.Security.EndpointSecretName {
		logger.Info("Changed EndpointSecretName",
			"PrevName", ispn.Status.Security.EndpointSecretName,
			"NewName", ispn.Spec.Security.EndpointSecretName)
		secret, err := r.findSecret(ispn)
		if err != nil {
			logger.Error(err, "could not find secret", "Secret.Name", ispn.Spec.Security.EndpointSecretName)
			return &reconcile.Result{}, err
		}

		if secret == nil {
			// Generate the identities secret if not provided by the user
			identities, err := users.GetCredentials()
			if err != nil {
				logger.Error(err, "could not get identities for Secret")
				return &reconcile.Result{}, err
			}

			secret = r.secretForInfinispan(identities, ispn)
			logger.Info("Creating a new Secret", "Secret.Name", secret.Name)
			err = r.client.Create(context.TODO(), secret)
			if err != nil {
				logger.Error(err, "could not create a new Secret")
				return &reconcile.Result{}, err
			}
		}

		statefulSet.Spec.Template.ObjectMeta.Annotations["updateDate"] = time.Now().String()
		// Find and update secret in StatefulSet volume
		for i, volumes := range statefulSet.Spec.Template.Spec.Volumes {
			if volumes.Secret != nil && volumes.Name == IdentitiesVolumeName {
				statefulSet.Spec.Template.Spec.Volumes[i].Secret.SecretName = secret.GetName()
			}
		}

		err = r.client.Update(context.TODO(), statefulSet)
		if err != nil {
			logger.Error(err, "failed to update StatefulSet")
			return &reconcile.Result{}, err
		}
		// Get latest version of the infinispan crd
		err = r.client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, ispn)
		if err != nil {
			logger.Error(err, "failed to get Infinispan")
			return &reconcile.Result{}, err
		}
		// Copy and update .Status.Security to match .Spec.Security
		ispn.Spec.Security.DeepCopyInto(&ispn.Status.Security)
		err = r.client.Status().Update(context.TODO(), ispn)
		if err != nil {
			logger.Error(err, "failed to update Infinispan Status")
			return &reconcile.Result{}, err
		}
		logger.Info("Security set",
			"Spec.EndpointSecretName", ispn.Spec.Security.EndpointSecretName,
			"Status.EndpointSecretName", ispn.Status.Security.EndpointSecretName)

		return &reconcile.Result{}, nil
	}
	return nil, nil
}

// reconcileContainerConf reconcile the .Container struct is changed in .Spec. This needs a cluster restart
func (r *ReconcileInfinispan) reconcileContainerConf(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet, configMap *corev1.ConfigMap, logger logr.Logger) (*reconcile.Result, error) {
	updateNeeded := false
	// Ensure the deployment size is the same as the spec
	replicas := ispn.Spec.Replicas
	previousReplicas := *statefulSet.Spec.Replicas
	if previousReplicas != replicas {
		statefulSet.Spec.Replicas = &replicas
		logger.Info("replicas changed, update infinispan", "replicas", replicas, "previous replicas", previousReplicas)
		updateNeeded = true
	}

	// Changes to statefulset.spec.template.spec.containers[].resources
	res := &statefulSet.Spec.Template.Spec.Containers[0].Resources
	env := &statefulSet.Spec.Template.Spec.Containers[0].Env
	ispnContr := &ispn.Spec.Container
	if ispnContr.Memory != "" {
		quantity := resource.MustParse(ispnContr.Memory)
		previousMemory := res.Requests["memory"]
		if quantity.Cmp(previousMemory) != 0 {
			res.Requests["memory"] = quantity
			res.Limits["memory"] = quantity
			logger.Info("memory changed, update infinispan", "memory", quantity, "previous memory", previousMemory)
			updateNeeded = true
		}
	}
	if ispnContr.CPU != "" {
		cpuReq, cpuLim := ispn.Spec.Container.GetCpuResources()
		previousCPUReq := res.Requests["cpu"]
		previousCPULim := res.Limits["cpu"]
		if cpuReq.Cmp(previousCPUReq) != 0 || cpuLim.Cmp(previousCPULim) != 0 {
			res.Requests["cpu"] = cpuReq
			res.Limits["cpu"] = cpuLim
			logger.Info("cpu changed, update infinispan", "cpuLim", cpuLim, "cpuReq", cpuReq, "previous cpuLim", previousCPULim, "previous cpuReq", previousCPUReq)
			updateNeeded = true
		}
	}
	// Validate ConfigMap changes (by the hash of the infinispan.yaml key value)
	configHashIndex := kube.GetEnvVarIndex("CONFIG_HASH", env)
	previousConfigHash := (*env)[configHashIndex].Value
	configHash := sha256String(configMap.Data[consts.ServerConfigFilename])
	if previousConfigHash != configHash {
		(*env)[configHashIndex].Value = configHash
		updateNeeded = true
	}

	extraJavaOptionsIndex := kube.GetEnvVarIndex("EXTRA_JAVA_OPTIONS", env)
	extraJavaOptions := ispnContr.ExtraJvmOpts
	previousExtraJavaOptions := (*env)[extraJavaOptionsIndex].Value
	if extraJavaOptions != previousExtraJavaOptions {
		(*env)[extraJavaOptionsIndex].Value = ispnContr.ExtraJvmOpts
		javaOptionsIndex := kube.GetEnvVarIndex("JAVA_OPTIONS", env)
		newJavaOptions := ispn.GetJavaOptions()
		(*env)[javaOptionsIndex].Value = newJavaOptions
		logger.Info("extra jvm options changed, update infinispan",
			"extra java options", extraJavaOptions,
			"previous extra java options", previousExtraJavaOptions,
			"full java options", newJavaOptions,
		)
		updateNeeded = true
	}
	if updateNeeded {
		logger.Info("updateNeeded")
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

func ExternalServiceLabels(name string) map[string]string {
	return LabelsResource(name, "infinispan-service-external")
}

func sha256String(data string) string {
	return hex.EncodeToString(sha256.New().Sum([]byte(data)))
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
