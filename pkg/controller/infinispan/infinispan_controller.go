package infinispan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/cache"
	ispncom "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/infinispan/configuration"
	"github.com/infinispan/infinispan-operator/version"
	routev1 "github.com/openshift/api/route/v1"
	"gopkg.in/yaml.v2"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_infinispan")

// Kubernetes object
var kubernetes *ispnutil.Kubernetes

// Cluster object
var cluster ispnutil.ClusterInterface

// Add creates a new Infinispan Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	kubernetes = ispnutil.NewKubernetesFromController(mgr)
	cluster = ispnutil.NewCluster(kubernetes)
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
		if infinispan.SetCondition("preliminaryChecksFailed", "true", err.Error()) {
			err1 := r.client.Status().Update(context.TODO(), infinispan)
			if err1 != nil {
				reqLogger.Error(err1, "Could not update error conditions")
			}
		}
		return *result, err
	}
	if infinispan.RemoveCondition("preliminaryChecksFailed") {
		err = r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "Could not update error conditions")
		}
	}

	infinispan.ApplyEndpointEncryptionSettings(kubernetes.GetServingCertsMode(), reqLogger)

	xsite := &configuration.XSite{}
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

		err = xsite.ComputeXSite(infinispan, kubernetes, siteService, reqLogger)
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
			identities, err := ispnutil.GetCredentials()
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

		infinispan.SetCondition("upgrade", metav1.ConditionFalse, "")
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
	labelSelector := labels.SelectorFromSet(ispncom.LabelsResource(infinispan.Name, "infinispan-pod"))
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

	protocol := infinispan.GetEndpointScheme()
	// If user set Spec.replicas=0 we need to perform a graceful shutdown
	// to preserve the data
	var res *reconcile.Result
	res, err = r.reconcileGracefulShutdown(infinispan, statefulSet, podList, reqLogger)
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

	res, err = r.reconcileContainerConf(infinispan, statefulSet, reqLogger)
	if res != nil {
		return *res, err
	}

	// If x-site enable configure the coordinator pods to be selected by the x-site service
	if infinispan.HasSites() {
		found := configuration.ApplyLabelsToCoordinatorsPod(podList, infinispan, cluster, r.client, reqLogger)
		if !found {
			// If a coordinator is not found then requeue early
			return reconcile.Result{Requeue: true}, nil
		}
	}

	// Update the Infinispan status with the pod status
	// Wait until all pods have ips assigned
	// Without those ips, it's not possible to execute next calls
	if !ispncom.ArePodIPsReady(podList) {
		reqLogger.Info("Pods IPs are not ready yet")
		return reconcile.Result{}, nil
	}

	// All pods ready start autoscaler if needed
	if infinispan.Spec.Autoscale != nil && infinispan.IsCache() {
		addAutoscalingEquipment(types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, r)
	}
	// Inspect the system and get the current Infinispan conditions
	currConds := getInfinispanConditions(podList.Items, infinispan, string(protocol), cluster)

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
		return reconcile.Result{Requeue: true, RequeueAfter: 15 * time.Second}, nil
	}

	// Below the code for a wellFormed cluster

	// Create default cache if it doesn't exists.
	if infinispan.IsCache() {
		existsCache, err := cache.ExistsCacheServiceDefaultCache(podList.Items[0].Name, infinispan, kubernetes, cluster)
		if err != nil {
			reqLogger.Error(err, "failed to validate default cache for cache service")
			return reconcile.Result{}, err
		}
		if !existsCache {
			reqLogger.Info("createDefaultCache")
			err = cache.CreateCacheServiceDefaultCache(podList.Items[0].Name, infinispan, kubernetes, cluster, reqLogger)
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
	err := r.client.Delete(context.TODO(),
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
				Name:      infinispan.ObjectMeta.Name + "-configuration",
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
	if !ispncom.AreAllPodsReady(podList) {
		return nil, nil
	}

	// Get default Infinispan image for a running Infinispan pod
	podDefaultImage := ispncom.GetPodDefaultImage(podList.Items[0].Spec.Containers[0])

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
		infinispan.SetCondition("upgrade", metav1.ConditionTrue, "")
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
	lsPod := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-pod")

	memory := resource.MustParse(m.Spec.Container.Memory)
	cpuRequests, cpuLimits := m.GetCpuResources()

	javaOptions, err := m.GetJavaOptions()
	if err != nil {
		return nil, err
	}

	envVars := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: "/etc/config/infinispan.yaml"},
		{Name: "IDENTITIES_PATH", Value: "/etc/security/identities.yaml"},
		{Name: "JAVA_OPTIONS", Value: javaOptions},
		{Name: "EXTRA_JAVA_OPTIONS", Value: m.Spec.Container.ExtraJvmOpts},
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
	replicas := m.Spec.Replicas
	protocolScheme := m.GetEndpointScheme()
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
						Image: m.Spec.Image,
						Name:  "infinispan",
						Env:   envVars,
						LivenessProbe: &corev1.Probe{
							Handler:             ispnutil.ClusterStatusHandler(protocolScheme),
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       60,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Ports: []corev1.ContainerPort{
							{ContainerPort: consts.InfinispanPingPort, Name: "ping", Protocol: corev1.ProtocolTCP},
							{ContainerPort: consts.InfinispanPort, Name: "infinispan", Protocol: corev1.ProtocolTCP},
						},
						ReadinessProbe: &corev1.Probe{
							Handler:             ispnutil.ClusterStatusHandler(protocolScheme),
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"cpu":    cpuRequests,
								"memory": memory,
							},
							Limits: corev1.ResourceList{
								"cpu":    cpuLimits,
								"memory": memory,
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config-volume",
							MountPath: "/etc/config",
						}, {
							Name:      "identities-volume",
							MountPath: "/etc/security",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config-volume",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: configMap.Name},
							},
						},
					}, {
						Name: "identities-volume",
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

	// Persistent vol size must exceed memory size
	// so that it can contain all the in memory data
	pvSize := consts.DefaultPVSize
	if pvSize.Cmp(memory) < 0 {
		pvSize = memory
	}

	if m.IsDataGrid() && m.Spec.Service.Container.Storage != "" {
		var pvErr error
		pvSize, pvErr = resource.ParseQuantity(m.Spec.Service.Container.Storage)
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
				"ReadWriteOnce",
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: pvSize,
				},
			},
		},
	}

	blockOwnerDeletion := false
	controllerutil.SetControllerReference(m, pvc, r.scheme)
	pvc.OwnerReferences[0].BlockOwnerDeletion = &blockOwnerDeletion
	dep.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{*pvc}

	// Adding persistent volume mount
	v := &dep.Spec.Template.Spec.Containers[0].VolumeMounts
	*v = append(*v, corev1.VolumeMount{
		Name:      m.ObjectMeta.Name,
		MountPath: "/opt/infinispan/server/data",
	})
	// Adding an init container that run chmod if needed
	if chmod, ok := os.LookupEnv("MAKE_DATADIR_WRITABLE"); ok && chmod == "true" {
		initContainerImage := consts.InitContainerImageName
		dep.Spec.Template.Spec.InitContainers = []corev1.Container{{
			Image:   initContainerImage,
			Name:    "chmod-pv",
			Command: []string{"sh", "-c", "chmod -R g+w /opt/infinispan/server/data"},
			VolumeMounts: []corev1.VolumeMount{{
				Name:      m.ObjectMeta.Name,
				MountPath: "/opt/infinispan/server/data",
			}},
		}}
	}
	setupVolumesForEncryption(m, dep)
	//	appendVolumes(m, dep)

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, dep, r.scheme)
	return dep, nil
}

func setupServiceForEncryption(m *infinispanv1.Infinispan, ser *corev1.Service) {
	ee := m.Spec.Security.EndpointEncryption
	if strings.EqualFold(ee.Type, "Service") {
		if strings.Contains(ee.CertServiceName, "openshift.io") {
			// Using platform service. Only OpenShift is integrated atm
			secretName := m.GetEncryptionSecretName()
			if ser.ObjectMeta.Annotations == nil {
				ser.ObjectMeta.Annotations = map[string]string{}
			}
			ser.ObjectMeta.Annotations[ee.CertServiceName+"/serving-cert-secret-name"] = secretName
		}
	}
}

func setupVolumesForEncryption(m *infinispanv1.Infinispan, dep *appsv1.StatefulSet) {
	secretName := m.GetEncryptionSecretName()
	if secretName != "" {
		v := &dep.Spec.Template.Spec.Volumes
		vm := &dep.Spec.Template.Spec.Containers[0].VolumeMounts
		*vm = append(*vm, corev1.VolumeMount{Name: "encrypt-volume", MountPath: "/etc/encrypt"})
		*v = append(*v, corev1.Volume{Name: "encrypt-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				}}})
	}
}

func setupConfigForEncryption(m *infinispanv1.Infinispan, c *configuration.InfinispanConfiguration, client client.Client) error {
	ee := m.Spec.Security.EndpointEncryption
	if strings.EqualFold(ee.Type, "Service") {
		if strings.Contains(ee.CertServiceName, "openshift.io") {
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
			reqLogger := log.WithValues("Infinispan.Namespace", m.Namespace, "Infinispan.Name", m.Name)
			if errors.IsNotFound(err) {
				reqLogger.Error(err, "Secret %s for endpoint encryption not found.", tlsSecretName)
				return err
			}
			reqLogger.Error(err, "Error in getting secret %s for endpoint encryption.", tlsSecretName)
			return err
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

func (r *ReconcileInfinispan) computeConfigMap(xsite *configuration.XSite, m *infinispanv1.Infinispan) (*corev1.ConfigMap, error) {
	name := m.ObjectMeta.Name
	namespace := m.ObjectMeta.Namespace

	loggingCategories := m.CopyLoggingCategories()
	config := configuration.CreateInfinispanConfiguration(name, loggingCategories, namespace, xsite)
	err := setupConfigForEncryption(m, &config, r.client)
	if err != nil {
		return nil, err
	}
	configYaml, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	lsConfigMap := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-configmap-configuration")
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-configuration",
			Namespace: namespace,
			Labels:    lsConfigMap,
		},
		Data: map[string]string{"infinispan.yaml": string(configYaml)},
	}

	return configMap, nil
}

func (r *ReconcileInfinispan) findSecret(m *infinispanv1.Infinispan) (*corev1.Secret, error) {
	secretName := m.GetSecretName()
	return kubernetes.GetSecret(secretName, m.Namespace)
}

func (r *ReconcileInfinispan) secretForInfinispan(identities []byte, m *infinispanv1.Infinispan) *corev1.Secret {
	lsSecret := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-secret-identities")
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
		StringData: map[string]string{"identities.yaml": string(identities)},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, secret, r.scheme)
	return secret
}

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(pods []corev1.Pod, m *infinispanv1.Infinispan, protocol string, cluster ispnutil.ClusterInterface) []infinispanv1.InfinispanCondition {
	var status []infinispanv1.InfinispanCondition
	clusterViews := make(map[string]bool)
	var errors []string
	// Avoid to inspect the system if we're still waiting for the pods
	if int32(len(pods)) < m.Spec.Replicas {
		errors = append(errors, fmt.Sprintf("Running %d pods. Needed %d", len(pods), m.Spec.Replicas))
	} else {
		secretName := m.GetSecretName()
		for _, pod := range pods {
			var clusterView string
			var members []string
			if ispncom.IsPodReady(pod) {
				// If pod is ready query it for the cluster members
				pass, err := cluster.GetPassword(consts.DefaultOperatorUser, secretName, m.GetNamespace())
				if err == nil {
					members, err = cluster.GetClusterMembers(consts.DefaultOperatorUser, pass, pod.Name, pod.Namespace, protocol)
					clusterView = strings.Join(members, ",")
					if err == nil {
						clusterViews[clusterView] = true
					}
				}
				if err != nil {
					errors = append(errors, pod.Name+": "+err.Error())
				}
			} else {
				// Pod not ready, no need to query
				errors = append(errors, pod.Name+": pod not ready")
			}
		}
	}
	// Evaluating WellFormed condition
	wellformed := infinispanv1.InfinispanCondition{Type: "wellFormed"}
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
	lsPodSelector := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-pod")
	lsService := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-service")
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
			Selector: lsPodSelector,
			Ports: []corev1.ServicePort{
				{
					Port: consts.InfinispanPort,
				},
			},
		},
	}

	return service
}

func (r *ReconcileInfinispan) computePingService(m *infinispanv1.Infinispan) *corev1.Service {
	lsPodSelector := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-pod")
	lsService := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-service-ping")
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
			ClusterIP: "None",
			Selector:  lsPodSelector,
			Ports: []corev1.ServicePort{
				{
					Name: "ping",
					Port: consts.InfinispanPingPort,
				},
			},
		},
	}

	return service
}

// serviceExternal creates an external service that's linked to the internal service
// This suppose that m.Spec.Expose is not null
func (r *ReconcileInfinispan) computeServiceExternal(m *infinispanv1.Infinispan) *corev1.Service {
	lsPodSelector := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-pod")
	lsService := ispncom.LabelsResource(m.ObjectMeta.Name, "infinispan-service-external")
	externalServiceType := corev1.ServiceType(m.Spec.Expose.Type)
	externalServiceName := m.GetServiceExternalName()
	externalService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalServiceName,
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsService,
		},
		Spec: corev1.ServiceSpec{
			Type:     externalServiceType,
			Selector: lsPodSelector,
			Ports: []corev1.ServicePort{
				{
					Port:       int32(consts.InfinispanPort),
					TargetPort: intstr.FromInt(consts.InfinispanPort),
				},
			},
		},
	}

	if m.Spec.Expose.NodePort > 0 {
		externalService.Spec.Ports[0].NodePort = m.Spec.Expose.NodePort
	}
	return externalService
}

// reconcileXSite creates the xsite service if needed
func (r *ReconcileInfinispan) reconcileXSite(ispn *infinispanv1.Infinispan, scheme *runtime.Scheme, logger logr.Logger) (*corev1.Service, error) {
	siteServiceName := ispn.GetSiteServiceName()
	siteService, err := configuration.GetOrCreateSiteService(siteServiceName, ispn, kubernetes.Client, scheme, logger)
	if err != nil {
		logger.Error(err, "could not get or create site service")
		return nil, err
	}
	return siteService, nil
}

// reconcileConfigMap creates the configMap for Infinispan if needed
func (r *ReconcileInfinispan) reconcileConfigMap(ispn *infinispanv1.Infinispan, configMap *corev1.ConfigMap, logger logr.Logger) error {
	cm := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, cm)
	if errors.IsNotFound(err) {
		// Set Infinispan instance as the owner and controller
		controllerutil.SetControllerReference(ispn, configMap, r.scheme)
		err := r.client.Create(context.TODO(), configMap)
		if err != nil {
			logger.Error(err, "failed to create new ConfigMap")
			return err
		}
		return nil
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
	lsService := ispncom.LabelsResource(ispn.ObjectMeta.Name, "infinispan-service-external")
	route := routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    lsService,
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
	lsService := ispncom.LabelsResource(ispn.ObjectMeta.Name, "infinispan-service-external")
	ingress := networkingv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    lsService,
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

func (r *ReconcileInfinispan) reconcileGracefulShutdown(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet, podList *corev1.PodList, logger logr.Logger) (*reconcile.Result, error) {
	if ispn.Spec.Replicas == 0 {
		logger.Info(".Spec.Replicas==0")
		updateStatus := false
		if *statefulSet.Spec.Replicas != 0 {
			logger.Info("StatefulSet.Spec.Replicas!=0")
			// If cluster hasn't a `stopping` condition or it's false then send a graceful shutdown
			if cond := ispn.GetCondition("stopping"); cond == nil || *cond != "True" {
				res, err := r.gracefulShutdownReq(ispn, podList, logger)
				if res != nil {
					return res, err
				}
			} else {
				// cluster has a `stopping` wait for all the pods becomes unready
				logger.Info("Waiting that all the pods become unready")
				for _, pod := range podList.Items {
					if ispncom.IsPodReady(pod) {
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
			updateStatus = true
			zeroReplicas := int32(0)
			statefulSet.Spec.Replicas = &zeroReplicas
			err = r.client.Update(context.TODO(), statefulSet)
			if err != nil {
				logger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
				return &reconcile.Result{}, err
			}
		}
		if statefulSet.Status.CurrentReplicas == 0 {
			updateStatus = updateStatus || ispn.SetCondition("gracefulShutdown", metav1.ConditionTrue, "")
			updateStatus = updateStatus || ispn.SetCondition("stopping", metav1.ConditionFalse, "")
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
	isGracefulShutdown := ispn.GetCondition("gracefulShutdown")
	if ispn.Spec.Replicas != 0 && isGracefulShutdown != nil && *isGracefulShutdown == "True" {
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
		ispn.SetCondition("gracefulShutdown", metav1.ConditionFalse, "")
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
func (r *ReconcileInfinispan) gracefulShutdownReq(ispn *infinispanv1.Infinispan, podList *corev1.PodList, logger logr.Logger) (*reconcile.Result, error) {
	logger.Info("Sending graceful shutdown request")
	// send a graceful shutdown to the first ready pod
	// if there's no ready pod we're in trouble
	for _, pod := range podList.Items {
		if ispncom.IsPodReady(pod) {
			pass, err := kubernetes.GetPassword(consts.DefaultOperatorUser, ispn.GetSecretName(), ispn.GetNamespace())
			if err == nil {
				err = cluster.GracefulShutdown(consts.DefaultOperatorUser, pass, pod.GetName(), pod.GetNamespace(), string(ispn.GetEndpointScheme()))
			}
			if err != nil {
				logger.Error(err, "failed to exec shutdown command on pod")
				continue
			}
			logger.Info("Executed graceful shutdown on pod: ", "Pod.Name", pod.Name)
			ispn.SetCondition("stopping", metav1.ConditionTrue, "")
			ispn.SetCondition("wellFormed", metav1.ConditionFalse, "")
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
			identities, err := ispnutil.GetCredentials()
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
			if volumes.Secret != nil && volumes.Name == "identities-volume" {
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
func (r *ReconcileInfinispan) reconcileContainerConf(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet, logger logr.Logger) (*reconcile.Result, error) {
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
		cpuReq, cpuLim := ispn.GetCpuResources()
		previousCPUReq := res.Requests["cpu"]
		previousCPULim := res.Limits["cpu"]
		if cpuReq.Cmp(previousCPUReq) != 0 || cpuLim.Cmp(previousCPULim) != 0 {
			res.Requests["cpu"] = cpuReq
			res.Limits["cpu"] = cpuLim
			logger.Info("cpu changed, update infinispan", "cpuLim", cpuLim, "cpuReq", cpuReq, "previous cpuLim", previousCPULim, "previous cpuReq", previousCPUReq)
			updateNeeded = true
		}
	}

	extraJavaOptionsIndex := ispncom.GetEnvVarIndex("EXTRA_JAVA_OPTIONS", env)
	extraJavaOptions := ispnContr.ExtraJvmOpts
	previousExtraJavaOptions := (*env)[extraJavaOptionsIndex].Value
	if extraJavaOptions != previousExtraJavaOptions {
		(*env)[extraJavaOptionsIndex].Value = ispnContr.ExtraJvmOpts
		javaOptionsIndex := ispncom.GetEnvVarIndex("JAVA_OPTIONS", env)
		newJavaOptions, _ := ispn.GetJavaOptions()
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
