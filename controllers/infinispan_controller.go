package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	hash "github.com/infinispan/infinispan-operator/pkg/hash"
	"github.com/infinispan/infinispan-operator/pkg/http/curl"
	ispnApi "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/common/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	DataMountPath                = consts.ServerRoot + "/data"
	OperatorConfMountPath        = consts.ServerRoot + "/conf/operator"
	DataMountVolume              = "data-volume"
	ConfigVolumeName             = "config-volume"
	EncryptKeystoreVolumeName    = "encrypt-volume"
	EncryptTruststoreVolumeName  = "encrypt-trust-volume"
	IdentitiesVolumeName         = "identities-volume"
	AdminIdentitiesVolumeName    = "admin-identities-volume"
	UserConfVolumeName           = "user-conf-volume"
	InfinispanSecurityVolumeName = "infinispan-security-volume"
	OverlayConfigMountPath       = consts.ServerRoot + "/conf/user"

	EventReasonPrelimChecksFailed    = "PrelimChecksFailed"
	EventReasonLowPersistenceStorage = "LowPersistenceStorage"
	EventReasonEphemeralStorage      = "EphemeralStorageEnables"
	EventReasonParseValueProblem     = "ParseValueProblem"
	EventLoadBalancerUnsupported     = "LoadBalancerUnsupported"

	SiteTransportKeystoreVolumeName = "encrypt-transport-site-tls-volume"
	SiteRouterKeystoreVolumeName    = "encrypt-router-site-tls-volume"
	SiteTruststoreVolumeName        = "encrypt-truststore-site-tls-volume"
)

// InfinispanReconciler reconciles a Infinispan object
type InfinispanReconciler struct {
	client.Client
	log            logr.Logger
	scheme         *runtime.Scheme
	kubernetes     *kube.Kubernetes
	eventRec       record.EventRecorder
	supportedTypes map[string]*reconcileType
}

// Struct for wrapping reconcile request data
type infinispanRequest struct {
	*InfinispanReconciler
	ctx        context.Context
	req        ctrl.Request
	infinispan *infinispanv1.Infinispan
	reqLogger  logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfinispanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName("Infinispan")
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor("controller-infinispan")
	r.supportedTypes = map[string]*reconcileType{
		consts.ExternalTypeRoute:   {ObjectType: &routev1.Route{}, GroupVersion: routev1.SchemeGroupVersion, GroupVersionSupported: false},
		consts.ExternalTypeIngress: {ObjectType: &ingressv1.Ingress{}, GroupVersion: ingressv1.SchemeGroupVersion, GroupVersionSupported: false},
		consts.ServiceMonitorType:  {ObjectType: &monitoringv1.ServiceMonitor{}, GroupVersion: monitoringv1.SchemeGroupVersion, GroupVersionSupported: false},
	}

	ctx := context.TODO()
	var err error
	// Add Secret name fields to the index for caching
	if err = mgr.GetFieldIndexer().IndexField(ctx, &infinispanv1.Infinispan{}, "spec.security.endpointSecretName", func(obj client.Object) []string {
		return []string{obj.(*infinispanv1.Infinispan).GetSecretName()}
	}); err != nil {
		return err
	}
	if err = mgr.GetFieldIndexer().IndexField(ctx, &infinispanv1.Infinispan{}, "spec.security.endpointEncryption.certSecretName", func(obj client.Object) []string {
		return []string{obj.(*infinispanv1.Infinispan).GetKeystoreSecretName()}
	}); err != nil {
		return err
	}
	if err = mgr.GetFieldIndexer().IndexField(ctx, &infinispanv1.Infinispan{}, "spec.security.endpointEncryption.clientCertSecretName", func(obj client.Object) []string {
		return []string{obj.(*infinispanv1.Infinispan).GetTruststoreSecretName()}
	}); err != nil {
		return err
	}

	if err = mgr.GetFieldIndexer().IndexField(ctx, &infinispanv1.Infinispan{}, "spec.configMapName", func(obj client.Object) []string {
		return []string{obj.(*infinispanv1.Infinispan).Spec.ConfigMapName}
	}); err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&infinispanv1.Infinispan{})

	for _, obj := range r.supportedTypes {
		// Validate that GroupVersion is supported on runtime platform
		ok, err := r.kubernetes.IsGroupVersionSupported(obj.GroupVersion.String(), obj.Kind())
		if err != nil {
			log.Error(err, fmt.Sprintf("Failed to check if GVK '%s' is supported", obj.GroupVersionKind()))
			continue
		}
		obj.GroupVersionSupported = ok
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Infinispan
	secondaryResourceTypes := []client.Object{&appsv1.StatefulSet{}, &corev1.ConfigMap{}, &corev1.Secret{}, &appsv1.Deployment{}}
	for _, secondaryResource := range secondaryResourceTypes {
		builder.Owns(secondaryResource)
	}
	builder.WithEventFilter(predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			switch e.Object.(type) {
			case *appsv1.StatefulSet:
				return false
			case *corev1.ConfigMap:
				return false
			}
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			switch e.Object.(type) {
			case *corev1.ConfigMap:
				return false
			}
			return true
		},
	})

	builder.Watches(
		&source.Kind{Type: &corev1.Secret{}},
		handler.EnqueueRequestsFromMapFunc(
			func(a client.Object) []reconcile.Request {
				var requests []reconcile.Request
				// Lookup only Secrets not controlled by Infinispan CR GVK. This means it's a custom defined Secret
				if !kube.IsControlledByGVK(a.GetOwnerReferences(), infinispanv1.SchemeBuilder.GroupVersion.WithKind(reflect.TypeOf(infinispanv1.Infinispan{}).Name())) {
					for _, field := range []string{"spec.security.endpointSecretName", "spec.security.endpointEncryption.certSecretName", "spec.security.endpointEncryption.clientCertSecretName"} {
						ispnList := &infinispanv1.InfinispanList{}
						if err := r.kubernetes.ResourcesListByField(a.GetNamespace(), field, a.GetName(), ispnList, ctx); err != nil {
							log.Error(err, "failed to list Infinispan CR")
						}
						for _, item := range ispnList.Items {
							requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: item.GetNamespace(), Name: item.GetName()}})
						}
						if len(requests) > 0 {
							return requests
						}
					}
				}
				return nil
			}),
	)
	builder.Watches(
		&source.Kind{Type: &corev1.ConfigMap{}},
		handler.EnqueueRequestsFromMapFunc(
			func(a client.Object) []reconcile.Request {
				var requests []reconcile.Request
				// Lookup only ConfigMap not controlled by Infinispan CR GVK. This means it's a custom defined ConfigMap
				if !kube.IsControlledByGVK(a.GetOwnerReferences(), infinispanv1.SchemeBuilder.GroupVersion.WithKind(reflect.TypeOf(infinispanv1.Infinispan{}).Name())) {
					ispnList := &infinispanv1.InfinispanList{}
					if err := r.kubernetes.ResourcesListByField(a.GetNamespace(), "spec.configMapName", a.GetName(), ispnList, ctx); err != nil {
						log.Error(err, "failed to list Infinispan CR")
					}
					for _, item := range ispnList.Items {
						requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: item.GetNamespace(), Name: item.GetName()}})
					}
					if len(requests) > 0 {
						return requests
					}
				}
				return nil
			}),
	)
	return builder.Complete(r)
}

// +kubebuilder:rbac:groups=infinispan.org,namespace=infinispan-operator-system,resources=infinispans;infinispans/status;infinispans/finalizers,verbs=get;list;watch;create;update;patch

// +kubebuilder:rbac:groups=core,namespace=infinispan-operator-system,resources=persistentvolumeclaims;services;services/finalizers;endpoints;configmaps;pods;secrets,verbs=get;list;watch;create;update;delete;patch;deletecollection
// +kubebuilder:rbac:groups=core,namespace=infinispan-operator-system,resources=pods/logs,verbs=get
// +kubebuilder:rbac:groups=core,namespace=infinispan-operator-system,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core;events.k8s.io,namespace=infinispan-operator-system,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=create;delete

// +kubebuilder:rbac:groups=apps,namespace=infinispan-operator-system,resources=deployments,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=apps,namespace=infinispan-operator-system,resources=replicasets,verbs=get
// +kubebuilder:rbac:groups=apps,namespace=infinispan-operator-system,resources=deployments/finalizers;statefulsets,verbs=get;list;watch;create;update;delete

// +kubebuilder:rbac:groups=networking.k8s.io,namespace=infinispan-operator-system,resources=ingresses,verbs=get;list;watch;create;delete;deletecollection;update
// +kubebuilder:rbac:groups=networking.k8s.io,namespace=infinispan-operator-system,resources=customresourcedefinitions;customresourcedefinitions/status,verbs=get;list

// +kubebuilder:rbac:groups=route.openshift.io,namespace=infinispan-operator-system,resources=routes;routes/custom-host,verbs=get;list;watch;create;delete;deletecollection;update

// +kubebuilder:rbac:groups=monitoring.coreos.com,namespace=infinispan-operator-system,resources=servicemonitors,verbs=get;list;watch;create;delete;update

// +kubebuilder:rbac:groups=core,resources=nodes;serviceaccounts,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions;customresourcedefinitions/status,verbs=get;list;watch

func (reconciler *InfinispanReconciler) Reconcile(ctx context.Context, ctrlRequest ctrl.Request) (ctrl.Result, error) {
	reqLogger := reconciler.log.WithValues("Request.Namespace", ctrlRequest.Namespace, "Request.Name", ctrlRequest.Name)
	reqLogger.Info("+++++ Reconciling Infinispan.")
	defer reqLogger.Info("----- End Reconciling Infinispan.")

	// Fetch the Infinispan instance
	infinispan := &infinispanv1.Infinispan{}
	infinispan.Name = ctrlRequest.Name
	infinispan.Namespace = ctrlRequest.Namespace

	r := &infinispanRequest{
		InfinispanReconciler: reconciler,
		ctx:                  ctx,
		req:                  ctrlRequest,
		infinispan:           infinispan,
		reqLogger:            reqLogger,
	}

	var preliminaryChecksResult *ctrl.Result
	var preliminaryChecksError error
	err := r.update(func() {
		// Apply defaults and endpoint encryption settings if not already set
		infinispan.ApplyDefaults()
		if r.isTypeSupported(consts.ServiceMonitorType) {
			infinispan.ApplyMonitoringAnnotation()
		}
		infinispan.Spec.Affinity = podAffinity(infinispan, PodLabels(infinispan.Name))
		errLabel := infinispan.ApplyOperatorLabels()
		if errLabel != nil {
			reqLogger.Error(errLabel, "Error applying operator label")
		}
		infinispan.ApplyEndpointEncryptionSettings(r.kubernetes.GetServingCertsMode(ctx), reqLogger)

		// Perform all the possible preliminary checks before go on
		preliminaryChecksResult, preliminaryChecksError = r.preliminaryChecks()
		if preliminaryChecksError != nil {
			r.eventRec.Event(infinispan, corev1.EventTypeWarning, EventReasonPrelimChecksFailed, preliminaryChecksError.Error())
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
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		reqLogger.Info("Error reading the object")
		return ctrl.Result{}, err
	}

	if preliminaryChecksResult != nil {
		return *preliminaryChecksResult, preliminaryChecksError
	}

	// Wait for the ConfigMap to be created by config-controller
	configMap := &corev1.ConfigMap{}
	if result, err := kube.LookupResource(infinispan.GetConfigName(), infinispan.Namespace, configMap, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
		return *result, err
	}

	// If an overlay configuration is specified wait for the related ConfigMap
	overlayConfigMap := &corev1.ConfigMap{}
	var overlayConfigMapKey string
	if infinispan.Spec.ConfigMapName != "" {
		if result, err := kube.LookupResource(infinispan.Spec.ConfigMapName, infinispan.Namespace, overlayConfigMap, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
			return *result, err
		}
		var foundKey bool
		// Loop through the data looking for something like xml,json or yaml
		for overlayConfigMapKey = range overlayConfigMap.Data {
			if overlayConfigMapKey == "infinispan-config.xml" || overlayConfigMapKey == "infinispan-config.json" || overlayConfigMapKey == "infinispan-config.yaml" {
				foundKey = true
				break
			}
		}
		if !foundKey {
			err := fmt.Errorf("infinispan-copnfig.[xml|yaml|json] configuration not found in ConfigMap: %s", overlayConfigMap.Name)
			return reconcile.Result{}, err
		}
	}

	// Wait for the Secret to be created by secret-controller or provided by user
	var userSecret *corev1.Secret
	if infinispan.IsAuthenticationEnabled() {
		userSecret = &corev1.Secret{}
		if result, err := kube.LookupResource(infinispan.GetSecretName(), infinispan.Namespace, userSecret, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
			return *result, err
		}
	}

	adminSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(infinispan.GetAdminSecretName(), infinispan.Namespace, adminSecret, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
		return *result, err
	}

	var keystoreSecret *corev1.Secret
	if infinispan.IsEncryptionEnabled() {
		if infinispan.Spec.Security.EndpointEncryption.CertSecretName == "" {
			return ctrl.Result{}, fmt.Errorf("field 'certSecretName' must be provided for certificateSourceType=%s to be configured", infinispanv1.CertificateSourceTypeSecret)
		}
		keystoreSecret = &corev1.Secret{}
		if result, err := kube.LookupResource(infinispan.GetKeystoreSecretName(), infinispan.Namespace, keystoreSecret, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
			return *result, err
		}
	}

	var trustSecret *corev1.Secret
	if infinispan.IsClientCertEnabled() {
		trustSecret = &corev1.Secret{}
		if result, err := kube.LookupResource(infinispan.GetTruststoreSecretName(), infinispan.Namespace, trustSecret, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
			return *result, err
		}
	}

	if infinispan.HasSites() {
		var gossipRouterTLSSecret *corev1.Secret
		if infinispan.IsSiteTLSEnabled() {
			// Keystore for Gossip Router
			gossipRouterTLSSecret = &corev1.Secret{}
			if result, err := kube.LookupResource(infinispan.GetSiteRouterSecretName(), infinispan.Namespace, gossipRouterTLSSecret, r.infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
				return *result, err
			}

			// Keystore for Infinispan pods (JGroups)
			transportTLSSecret := &corev1.Secret{}
			if result, err := kube.LookupResource(infinispan.GetSiteTransportSecretName(), infinispan.Namespace, transportTLSSecret, r.infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
				return *result, err
			}
		}

		tunnelDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetGossipRouterDeploymentName(),
				Namespace: infinispan.Namespace,
			},
		}
		result, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, tunnelDeployment, func() error {
			tunnel, err := r.GetGossipRouterDeployment(infinispan, gossipRouterTLSSecret)
			if err != nil {
				return err
			}
			if tunnelDeployment.CreationTimestamp.IsZero() {
				tunnelDeployment.Spec = tunnel.Spec
				tunnelDeployment.Labels = tunnel.Labels
				return controllerutil.SetControllerReference(r.infinispan, tunnelDeployment, r.scheme)
			} else {
				tunnelDeployment.Spec = tunnel.Spec
			}
			return nil
		})

		if err != nil {
			if errors.IsConflict(err) {
				return reconcile.Result{Requeue: true}, nil
			} else {
				reqLogger.Error(err, "Failed to configure Cross-Site Deployment")
				return reconcile.Result{}, err
			}
		}
		if result != controllerutil.OperationResultNone {
			reqLogger.Info(fmt.Sprintf("Cross-site deployment '%s' %s", tunnelDeployment.Name, string(result)))
		}

		gossipRouterPods, err := GossipRouterPodList(infinispan, r.kubernetes, r.ctx)
		if err != nil {
			reqLogger.Error(err, "Failed to fetch Gossip Router pod")
			return reconcile.Result{}, err
		}
		if len(gossipRouterPods.Items) == 0 || !kube.AreAllPodsReady(gossipRouterPods) {
			reqLogger.Info("Gossip Router pod is not ready")
			return reconcile.Result{}, r.update(func() {
				r.infinispan.SetCondition(infinispanv1.ConditionGossipRouterReady, metav1.ConditionFalse, "Gossip Router pod not ready")
			})
		}
		if err = r.update(func() {
			r.infinispan.SetCondition(infinispanv1.ConditionGossipRouterReady, metav1.ConditionTrue, "")
		}); err != nil {
			reqLogger.Error(err, "Failed to set Gossip Router pod condition")
			return reconcile.Result{}, err
		}
	} else {
		tunnelDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetGossipRouterDeploymentName(),
				Namespace: infinispan.Namespace,
			},
		}
		if err := r.Client.Delete(r.ctx, tunnelDeployment); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
	}

	// Reconcile the StatefulSet
	// Check if the StatefulSet already exists, if not create a new one
	statefulSet := &appsv1.StatefulSet{}
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.GetStatefulSetName()}, statefulSet)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Configuring the StatefulSet")

		// Define a new StatefulSet
		statefulSet, err = r.statefulSetForInfinispan(adminSecret, userSecret, keystoreSecret, trustSecret, configMap, overlayConfigMap, overlayConfigMapKey)
		if err != nil {
			reqLogger.Error(err, "failed to configure new StatefulSet")
			return ctrl.Result{}, err
		}
		reqLogger.Info("Creating a new StatefulSet", "StatefulSet.Name", statefulSet.Name)
		err = r.Client.Create(ctx, statefulSet)
		if err != nil {
			reqLogger.Error(err, "failed to create new StatefulSet", "StatefulSet.Name", statefulSet.Name)
			return ctrl.Result{}, err
		}
		// StatefulSet created successfully
		reqLogger.Info("End of the StatefulSet creation")
	}
	if err != nil {
		reqLogger.Error(err, "failed to get StatefulSet")
		return ctrl.Result{}, err
	}

	// Update Pod's status for the OLM and the statefulSet name
	if err := r.update(func() {
		infinispan.Status.StatefulSetName = statefulSet.Name
		infinispan.Status.PodStatus = GetSingleStatefulSetStatus(*statefulSet)
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Wait for the cluster Service to be created by service-controller
	if result, err := kube.LookupResource(infinispan.Name, infinispan.Namespace, &corev1.Service{}, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
		return *result, err
	}

	// Wait for the cluster ping Service to be created by service-controller
	if result, err := kube.LookupResource(infinispan.GetPingServiceName(), infinispan.Namespace, &corev1.Service{}, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
		return *result, err
	}

	if infinispan.IsUpgradeNeeded(reqLogger) {
		reqLogger.Info("Upgrade needed")
		err = r.destroyResources()
		if err != nil {
			reqLogger.Error(err, "failed to delete resources before upgrade")
			return ctrl.Result{}, err
		}

		if err := r.update(func() {
			infinispan.SetCondition(infinispanv1.ConditionUpgrade, metav1.ConditionFalse, "")
			if infinispan.Spec.Replicas != infinispan.Status.ReplicasWantedAtRestart {
				reqLogger.Info("removed Infinispan resources, force an upgrade now", "replicasWantedAtRestart", infinispan.Status.ReplicasWantedAtRestart)
				infinispan.Spec.Replicas = infinispan.Status.ReplicasWantedAtRestart
			}
		}); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{Requeue: true}, nil
	}

	// List the pods for this infinispan's deployment
	podList, err := PodList(infinispan, r.kubernetes, ctx)
	if err != nil {
		reqLogger.Error(err, "failed to list pods")
		return ctrl.Result{}, err
	}

	// Recover Pods with updated init containers in case of fails
	for _, pod := range podList.Items {
		if !kube.IsInitContainersEqual(statefulSet.Spec.Template.Spec.InitContainers, pod.Spec.InitContainers) {
			if kube.InitContainerFailed(pod.Status.InitContainerStatuses) {
				if err = r.Client.Delete(ctx, &pod); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	}

	if err = r.updatePodsLabels(podList); err != nil {
		return ctrl.Result{}, err
	}

	result, err := r.scheduleUpgradeIfNeeded(podList)
	if result != nil {
		return *result, err
	}

	// If user set Spec.replicas=0 we need to perform a graceful shutdown
	// to preserve the data
	var res *ctrl.Result
	res, err = r.reconcileGracefulShutdown(statefulSet, podList, reqLogger)
	if res != nil {
		return *res, err
	}
	// If upgrade required, do not process any further and handle upgrades
	if infinispan.IsUpgradeCondition() {
		reqLogger.Info("IsUpgradeCondition")
		return ctrl.Result{Requeue: true}, nil
	}

	// Here where to reconcile with spec updates that reflect into
	// changes to statefulset.spec.container.
	res, err = r.reconcileContainerConf(statefulSet, configMap, overlayConfigMap, overlayConfigMapKey, adminSecret, userSecret, keystoreSecret, trustSecret)
	if res != nil {
		return *res, err
	}

	// Update the Infinispan status with the pod status
	// Wait until all pods have IPs assigned
	// Without those IPs, it's not possible to execute next calls

	if !kube.ArePodIPsReady(podList) {
		reqLogger.Info("Pods IPs are not ready yet")
		return ctrl.Result{Requeue: true, RequeueAfter: consts.DefaultWaitClusterPodsNotReady}, r.update(func() {
			infinispan.SetCondition(infinispanv1.ConditionWellFormed, metav1.ConditionUnknown, "Pods are not ready")
			infinispan.RemoveCondition(infinispanv1.ConditionCrossSiteViewFormed)
			infinispan.Status.StatefulSetName = statefulSet.Name
		})
	}

	// All pods ready start autoscaler if needed
	if infinispan.Spec.Autoscale != nil && infinispan.IsCache() {
		r.addAutoscalingEquipment()
	}

	curl, err := NewCurlClient(ctx, podList.Items[0].Name, infinispan, r.kubernetes)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Inspect the system and get the current Infinispan conditions
	currConds := getInfinispanConditions(podList.Items, infinispan, curl)

	// Update the Infinispan status with the pod status
	if err := r.update(func() {
		infinispan.SetConditions(currConds)
	}); err != nil {
		return ctrl.Result{}, err
	}

	// View didn't form, requeue until view has formed
	if infinispan.NotClusterFormed(len(podList.Items), int(infinispan.Spec.Replicas)) {
		reqLogger.Info("notClusterFormed")
		return ctrl.Result{RequeueAfter: consts.DefaultWaitClusterNotWellFormed}, nil
	}

	// Below the code for a wellFormed cluster
	if err = configureLoggers(podList, infinispan, curl); err != nil {
		return ctrl.Result{}, err
	}

	// Create the ConfigListener Deployment if enabled
	if infinispan.IsConfigListenerEnabled() {
		if err := r.ReconcileConfigListener(); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.DeleteConfigListener(); err != nil {
			return ctrl.Result{}, err
		}
	}

	ispnClient := ispnApi.New(curl)
	// Create default cache if it doesn't exists.
	if infinispan.IsCache() {
		cacheClient := ispnClient.Cache(consts.DefaultCacheName)
		if existsCache, err := cacheClient.Exists(); err != nil {
			reqLogger.Error(err, "failed to validate default cache for cache service")
			return ctrl.Result{}, err
		} else if !existsCache {
			reqLogger.Info("createDefaultCache")
			defaultXml, err := DefaultCacheTemplateXML(podList.Items[0].Name, infinispan, r.kubernetes, reqLogger)
			if err != nil {
				return ctrl.Result{}, err
			}

			if err = cacheClient.Create(defaultXml, mime.ApplicationXml); err != nil {
				reqLogger.Error(err, "failed to create default cache for cache service")
				return ctrl.Result{}, err
			}
		}
	}

	if infinispan.IsExposed() {
		var exposeAddress string
		switch infinispan.GetExposeType() {
		case infinispanv1.ExposeTypeLoadBalancer, infinispanv1.ExposeTypeNodePort:
			// Wait for the cluster external Service to be created by service-controller
			externalService := &corev1.Service{}
			if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalService, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
				return *result, err
			}
			if len(externalService.Spec.Ports) > 0 && infinispan.GetExposeType() == infinispanv1.ExposeTypeNodePort {
				if exposeHost, err := r.kubernetes.GetNodeHost(reqLogger, ctx); err != nil {
					return ctrl.Result{}, err
				} else {
					exposeAddress = fmt.Sprintf("%s:%d", exposeHost, externalService.Spec.Ports[0].NodePort)
				}
			} else if infinispan.GetExposeType() == infinispanv1.ExposeTypeLoadBalancer {
				// Waiting for LoadBalancer cloud provider to update the configured hostname inside Status field
				if exposeAddress = r.kubernetes.GetExternalAddress(externalService); exposeAddress == "" {
					if !helpers.HasLBFinalizer(externalService) {
						errMsg := "LoadBalancer expose type is not supported on the target platform"
						r.eventRec.Event(externalService, corev1.EventTypeWarning, EventLoadBalancerUnsupported, errMsg)
						reqLogger.Info(errMsg)
						return ctrl.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
					}
					reqLogger.Info("LoadBalancer address not ready yet. Waiting on value in reconcile loop")
					return ctrl.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
				}
			}
		case infinispanv1.ExposeTypeRoute:
			if r.isTypeSupported(consts.ExternalTypeRoute) {
				externalRoute := &routev1.Route{}
				if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalRoute, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
					return *result, err
				}
				exposeAddress = externalRoute.Spec.Host
			} else if r.isTypeSupported(consts.ExternalTypeIngress) {
				externalIngress := &ingressv1.Ingress{}
				if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalIngress, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
					return *result, err
				}
				if len(externalIngress.Spec.Rules) > 0 {
					exposeAddress = externalIngress.Spec.Rules[0].Host
				}
			}
		}
		if err := r.update(func() {
			if exposeAddress == "" {
				infinispan.Status.ConsoleUrl = nil
			} else {
				infinispan.Status.ConsoleUrl = pointer.StringPtr(fmt.Sprintf("%s://%s/console", infinispan.GetEndpointScheme(), exposeAddress))
			}
		}); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.update(func() {
			infinispan.Status.ConsoleUrl = nil
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	if infinispan.HasSites() {
		crossSiteViewCondition, err := r.GetCrossSiteViewCondition(podList, infinispan.GetSiteLocationsName(), curl)
		if err != nil {
			return ctrl.Result{}, err
		}
		// ISPN-13116 If xsite view has been formed, then we must perform state-transfer to all sites if a SFS recovery has occurred
		if crossSiteViewCondition.Status == metav1.ConditionTrue {
			podName := podList.Items[0].Name
			logs, err := r.kubernetes.Logs(podName, infinispan.Namespace, ctx)
			if err != nil {
				log.Error(err, fmt.Sprintf("Unable to retrive logs for infinispan pod %s", podName))
			}
			if strings.Contains(logs, "ISPN000643") {
				if err := InfinispanForPod(podName, curl).Container().Xsite().PushAllState(); err != nil {
					log.Error(err, "Unable to push xsite state after SFS data recovery")
				}
			}
		}
		err = r.update(func() {
			infinispan.SetConditions([]infinispanv1.InfinispanCondition{*crossSiteViewCondition})
		})
		if err != nil || crossSiteViewCondition.Status != metav1.ConditionTrue {
			return ctrl.Result{RequeueAfter: consts.DefaultWaitOnCluster}, err
		}
	}

	return ctrl.Result{}, nil
}

// PreliminaryChecks performs all the possible initial checks
func (r *infinispanRequest) preliminaryChecks() (*ctrl.Result, error) {
	// If a CacheService is requested, checks that the pods have enough memory
	spec := r.infinispan.Spec
	if spec.Service.Type == infinispanv1.ServiceTypeCache {
		_, memoryQ, err := spec.Container.GetMemoryResources()
		if err != nil {
			return &ctrl.Result{
				Requeue:      false,
				RequeueAfter: consts.DefaultRequeueOnWrongSpec,
			}, err
		}
		memory := memoryQ.Value()
		nativeMemoryOverhead := (memory * consts.CacheServiceJvmNativePercentageOverhead) / 100
		occupiedMemory := (consts.CacheServiceJvmNativeMb * 1024 * 1024) +
			(consts.CacheServiceFixedMemoryXmxMb * 1024 * 1024) +
			nativeMemoryOverhead
		if memory < occupiedMemory {
			return &ctrl.Result{
				Requeue:      false,
				RequeueAfter: consts.DefaultRequeueOnWrongSpec,
			}, fmt.Errorf("not enough memory. Increase infinispan.spec.container.memory. Now is %s, needed at least %d", memoryQ.String(), occupiedMemory)
		}
	}
	return nil, nil
}

func configureLoggers(pods *corev1.PodList, infinispan *infinispanv1.Infinispan, curl *curl.Client) error {
	if infinispan.Spec.Logging == nil || len(infinispan.Spec.Logging.Categories) == 0 {
		return nil
	}

	for _, pod := range pods.Items {
		logging := InfinispanForPod(pod.Name, curl).Logging()
		serverLoggers, err := logging.GetLoggers()
		if err != nil {
			return err
		}
		for category, level := range infinispan.Spec.Logging.Categories {
			serverLevel, ok := serverLoggers[category]
			if !(ok && string(level) == serverLevel) {
				if err := logging.SetLogger(category, string(level)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *infinispanRequest) destroyResources() error {
	// TODO destroying all upgradable resources for recreation is too manual
	// Labels cannot easily be used to remove all resources with a given label.
	// Resource controller could be used to make this easier.
	// If all upgradable resources are controlled by the Stateful Set,
	// removing the Stateful Set should remove the rest.
	// Then, stateful set could be controlled by Infinispan to keep current logic.

	// Remove finalizer (we don't use it anymore) if it present and set owner reference for old PVCs
	infinispan := r.infinispan
	err := r.upgradeInfinispan()
	if err != nil {
		return err
	}

	if r.infinispan.IsConfigListenerEnabled() {
		if err = r.DeleteConfigListener(); err != nil {
			return err
		}
	}

	err = r.Client.Delete(r.ctx,
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetStatefulSetName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(r.ctx,
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetGossipRouterDeploymentName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(r.ctx,
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetConfigName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(r.ctx,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.Name,
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(r.ctx,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetPingServiceName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(r.ctx,
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetAdminServiceName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetServiceExternalName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if r.isTypeSupported(consts.ExternalTypeRoute) {
		err = r.Client.Delete(r.ctx,
			&routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      infinispan.GetServiceExternalName(),
					Namespace: infinispan.Namespace,
				},
			})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	} else if r.isTypeSupported(consts.ExternalTypeIngress) {
		err = r.Client.Delete(r.ctx,
			&ingressv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      infinispan.GetServiceExternalName(),
					Namespace: infinispan.Namespace,
				},
			})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	err = r.Client.Delete(r.ctx,
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

func (r *infinispanRequest) upgradeInfinispan() error {
	infinispan := r.infinispan
	// Remove controller owner reference from the custom Secrets
	for _, secretName := range []string{infinispan.GetKeystoreSecretName(), infinispan.GetTruststoreSecretName()} {
		if err := r.dropSecretOwnerReference(secretName); err != nil {
			return err
		}
	}

	if controllerutil.ContainsFinalizer(infinispan, consts.InfinispanFinalizer) {
		// Set Infinispan CR as owner reference for PVC if it not defined
		pvcs := &corev1.PersistentVolumeClaimList{}
		err := r.kubernetes.ResourcesList(infinispan.Namespace, LabelsResource(infinispan.Name, ""), pvcs, r.ctx)
		if err != nil {
			return err
		}

		for _, pvc := range pvcs.Items {
			if !metav1.IsControlledBy(&pvc, infinispan) {
				if err = controllerutil.SetControllerReference(infinispan, &pvc, r.scheme); err != nil {
					return err
				}
				pvc.OwnerReferences[0].BlockOwnerDeletion = pointer.BoolPtr(false)
				err := r.Client.Update(r.ctx, &pvc)
				if err != nil {
					return err
				}
			}
		}
	}

	return r.update(func() {
		// Remove finalizer if it defined in the Infinispan CR
		controllerutil.RemoveFinalizer(infinispan, consts.InfinispanFinalizer)

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

func (r *infinispanRequest) dropSecretOwnerReference(secretName string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.infinispan.Namespace,
		},
	}
	_, err := kube.CreateOrPatch(r.ctx, r.Client, secret, func() error {
		if secret.CreationTimestamp.IsZero() {
			return errors.NewNotFound(corev1.Resource(""), secretName)
		}
		kube.RemoveOwnerReference(secret, r.infinispan)
		return nil
	})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *infinispanRequest) scheduleUpgradeIfNeeded(podList *corev1.PodList) (*ctrl.Result, error) {
	infinispan := r.infinispan
	if shutdownUpgradeRequired(infinispan, podList) {
		if err := r.update(func() {
			podDefaultImage := kube.GetPodDefaultImage(podList.Items[0].Spec.Containers[0])
			r.reqLogger.Info("schedule an Infinispan cluster upgrade", "pod default image", podDefaultImage, "desired image", consts.DefaultImageName)
			infinispan.SetCondition(infinispanv1.ConditionUpgrade, metav1.ConditionTrue, "")
			infinispan.Spec.Replicas = 0
		}); err != nil {
			return &ctrl.Result{}, err
		}
	}
	return nil, nil
}

func shutdownUpgradeRequired(infinispan *infinispanv1.Infinispan, podList *corev1.PodList) bool {
	if len(podList.Items) == 0 {
		return false
	}
	if infinispan.IsUpgradeCondition() {
		return false
	}

	if infinispan.Spec.Upgrades != nil && infinispan.Spec.Upgrades.Type != infinispanv1.UpgradeTypeShutdown {
		return false
	}

	// All pods need to be ready for the upgrade to be scheduled
	// Handles brief window during which statefulSetForInfinispan resources have been removed,
	//and old ones terminating while new ones are being created.
	// We don't want yet another upgrade to be scheduled then.
	if !kube.AreAllPodsReady(podList) {
		return false
	}

	return isImageOutdated(podList)
}

func isImageOutdated(podList *corev1.PodList) bool {
	// Get default Infinispan image for a running Infinispan pod
	podDefaultImage := kube.GetPodDefaultImage(podList.Items[0].Spec.Containers[0])

	// Get Infinispan image that the operator creates
	desiredImage := consts.DefaultImageName

	// If the operator's default image differs from the pod's default image,
	// schedule an upgrade by gracefully shutting down the current cluster.
	return podDefaultImage != desiredImage
}

func IsUpgradeRequired(infinispan *infinispanv1.Infinispan, kube *kube.Kubernetes, ctx context.Context) (bool, error) {
	podList, err := PodList(infinispan, kube, ctx)
	if err != nil {
		return false, err
	}
	return shutdownUpgradeRequired(infinispan, podList), nil
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
						TopologyKey: "r.kubernetes.io/hostname",
					},
				}},
			},
		}
	}
	return i.Spec.Affinity
}

func GetSingleStatefulSetStatus(ss appsv1.StatefulSet) infinispanv1.DeploymentStatus {
	return getSingleDeploymentStatus(ss.Name, getInt32(ss.Spec.Replicas), ss.Status.Replicas, ss.Status.ReadyReplicas)
}

func getInt32(pointer *int32) int32 {
	if pointer == nil {
		return 0
	} else {
		return *pointer
	}

}
func getSingleDeploymentStatus(name string, requestedCount int32, targetCount int32, readyCount int32) infinispanv1.DeploymentStatus {
	var ready, starting, stopped []string
	if requestedCount == 0 || targetCount == 0 {
		stopped = append(stopped, name)
	} else {
		for i := int32(0); i < targetCount; i++ {
			instanceName := fmt.Sprintf("%s-%d", name, i+1)
			if i < readyCount {
				ready = append(ready, instanceName)
			} else {
				starting = append(starting, instanceName)
			}
		}
	}
	log.Info("Found deployments with status ", "stopped", stopped, "starting", starting, "ready", ready)
	return infinispanv1.DeploymentStatus{
		Stopped:  stopped,
		Starting: starting,
		Ready:    ready,
	}

}

func (r *infinispanRequest) updatePodsLabels(podList *corev1.PodList) error {
	if len(podList.Items) == 0 {
		return nil
	}

	ispn := r.infinispan
	labelsForPod := PodLabels(ispn.Name)
	ispn.AddOperatorLabelsForPods(labelsForPod)
	ispn.AddLabelsForPods(labelsForPod)

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

		_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, &pod, func() error {
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
func (r *infinispanRequest) statefulSetForInfinispan(adminSecret, userSecret, keystoreSecret, trustSecret *corev1.Secret,
	configMap, overlayConfigMap *corev1.ConfigMap, overlayConfigMapKey string) (*appsv1.StatefulSet, error) {
	ispn := r.infinispan
	reqLogger := r.log.WithValues("Request.Namespace", ispn.Namespace, "Request.Name", ispn.Name)
	lsPod := PodLabels(ispn.Name)
	labelsForPod := PodLabels(ispn.Name)
	ispn.AddOperatorLabelsForPods(labelsForPod)
	ispn.AddLabelsForPods(labelsForPod)
	ispn.AddStatefulSetLabelForPods(labelsForPod)

	pvcs := &corev1.PersistentVolumeClaimList{}
	err := r.kubernetes.ResourcesList(ispn.Namespace, LabelsResource(ispn.Name, ""), pvcs, r.ctx)
	if err != nil {
		return nil, err
	}
	dataVolumeName := DataMountVolume
	for _, pvc := range pvcs.Items {
		if strings.HasPrefix(pvc.Name, fmt.Sprintf("%s-%s", ispn.Name, ispn.Name)) {
			dataVolumeName = ispn.Name
			break
		}
	}

	memory, err := resource.ParseQuantity(ispn.Spec.Container.Memory)
	if err != nil {
		r.eventRec.Event(ispn, corev1.EventTypeWarning, EventReasonParseValueProblem, err.Error())
		reqLogger.Info(err.Error())
		return nil, err
	}
	replicas := ispn.Spec.Replicas
	volumeMounts := []corev1.VolumeMount{{
		Name:      ConfigVolumeName,
		MountPath: OperatorConfMountPath,
	}, {
		Name:      InfinispanSecurityVolumeName,
		MountPath: consts.ServerOperatorSecurity,
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
				SecretName: ispn.GetAdminSecretName(),
			},
		},
	}, {
		Name: InfinispanSecurityVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: ispn.GetInfinispanSecuritySecretName(),
			},
		},
	},
	}

	// Adding overlay config file if present
	if overlayConfigMap.Name != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      UserConfVolumeName,
			MountPath: OverlayConfigMountPath,
		})
		volumes = append(volumes, corev1.Volume{
			Name: UserConfVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: overlayConfigMap.Name},
				},
			}})
	}

	podResources, err := PodResources(ispn.Spec.Container)
	if err != nil {
		return nil, err
	}
	dep := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ispn.GetStatefulSetName(),
			Namespace:   ispn.Namespace,
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
					Affinity: ispn.Spec.Affinity,
					Containers: []corev1.Container{{
						Image: ispn.ImageName(),
						Name:  "infinispan",
						Env: PodEnv(ispn, &[]corev1.EnvVar{
							{Name: "CONFIG_HASH", Value: hash.HashString(configMap.Data[consts.ServerConfigFilename])},
							{Name: "ADMIN_IDENTITIES_HASH", Value: hash.HashByte(adminSecret.Data[consts.ServerIdentitiesFilename])},
							{Name: "IDENTITIES_BATCH", Value: consts.ServerOperatorSecurity + "/" + consts.ServerIdentitiesCliFilename},
						}),
						LivenessProbe:  PodLivenessProbe(),
						Ports:          PodPortsWithXsite(ispn),
						ReadinessProbe: PodReadinessProbe(),
						StartupProbe:   PodStartupProbe(),
						Resources:      *podResources,
						VolumeMounts:   volumeMounts,
						Args:           buildStartupArgs(overlayConfigMapKey, "false"),
					}},
					Volumes: volumes,
				},
			},
		},
	}

	// Only append IDENTITIES_HASH and secret volume if authentication is enabled
	spec := &dep.Spec.Template.Spec
	if AddVolumeForUserAuthentication(ispn, spec) {
		spec.Containers[0].Env = append(spec.Containers[0].Env,
			corev1.EnvVar{
				Name:  "IDENTITIES_HASH",
				Value: hash.HashByte(userSecret.Data[consts.ServerIdentitiesFilename]),
			})
	}
	if overlayConfigMapKey != "" {
		dep.Annotations = make(map[string]string)
		dep.Annotations["checksum/overlayConfig"] = hash.HashString(overlayConfigMap.Data[overlayConfigMapKey])
	}
	if !ispn.IsEphemeralStorage() {
		// Persistent vol size must exceed memory size
		// so that it can contain all the in memory data
		pvSize := consts.DefaultPVSize
		if pvSize.Cmp(memory) < 0 {
			pvSize = memory
		}

		if ispn.IsDataGrid() && ispn.StorageSize() != "" {
			var pvErr error
			pvSize, pvErr = resource.ParseQuantity(ispn.StorageSize())
			if pvErr != nil {
				return nil, pvErr
			}
			if pvSize.Cmp(memory) < 0 {
				errMsg := "Persistent volume size is less than memory size. Graceful shutdown may not work."
				r.eventRec.Event(ispn, corev1.EventTypeWarning, EventReasonLowPersistenceStorage, errMsg)
				reqLogger.Info(errMsg, "Volume Size", pvSize, "Memory", memory)
			}
		}

		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{
			Name:      dataVolumeName,
			Namespace: ispn.Namespace,
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

		if err = controllerutil.SetControllerReference(ispn, pvc, r.scheme); err != nil {
			return nil, err
		}
		pvc.OwnerReferences[0].BlockOwnerDeletion = pointer.BoolPtr(false)
		// Set a storage class if it specified
		if storageClassName := ispn.StorageClassName(); storageClassName != "" {
			if _, err := kube.FindStorageClass(storageClassName, r.Client, r.ctx); err != nil {
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
		errMsg := "Ephemeral storage configured. All data will be lost on cluster shutdown and restart."
		r.eventRec.Event(ispn, corev1.EventTypeWarning, EventReasonEphemeralStorage, errMsg)
		reqLogger.Info(errMsg)
	}

	if _, err := applyExternalArtifactsDownload(ispn, &dep.Spec.Template.Spec); err != nil {
		return nil, err
	}

	applyExternalDependenciesVolume(ispn, &dep.Spec.Template.Spec)
	if ispn.IsEncryptionEnabled() {
		AddVolumesForEncryption(ispn, &dep.Spec.Template.Spec)
		spec.Containers[0].Env = append(spec.Containers[0].Env,
			corev1.EnvVar{
				Name:  "KEYSTORE_HASH",
				Value: hash.HashMap(keystoreSecret.Data),
			})

		if ispn.IsClientCertEnabled() {
			spec.Containers[0].Env = append(spec.Containers[0].Env,
				corev1.EnvVar{
					Name:  "TRUSTSTORE_HASH",
					Value: hash.HashMap(trustSecret.Data),
				})
		}
	}

	if ispn.IsSiteTLSEnabled() {
		AddSecretVolume(ispn.GetSiteTransportSecretName(), SiteTransportKeystoreVolumeName, consts.SiteTransportKeyStoreRoot, spec)
		secret, err := FindSiteTrustStoreSecret(ispn, r.Client, r.ctx)
		if err != nil {
			return nil, err
		}
		if secret != nil {
			AddSecretVolume(ispn.GetSiteTrustoreSecretName(), SiteTruststoreVolumeName, consts.SiteTrustStoreRoot, spec)
		}
	}

	// Set Infinispan instance as the owner and controller
	if err = controllerutil.SetControllerReference(ispn, dep, r.scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(pods []corev1.Pod, m *infinispanv1.Infinispan, curl *curl.Client) []infinispanv1.InfinispanCondition {
	var status []infinispanv1.InfinispanCondition
	clusterViews := make(map[string]bool)
	var errors []string
	// Avoid to inspect the system if we're still waiting for the pods
	if int32(len(pods)) < m.Spec.Replicas {
		errors = append(errors, fmt.Sprintf("Running %d pods. Needed %d", len(pods), m.Spec.Replicas))
	} else {
		for _, pod := range pods {
			if kube.IsPodReady(pod) {
				if members, err := InfinispanForPod(pod.Name, curl).Container().Members(); err == nil {
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

func (r *infinispanRequest) reconcileGracefulShutdown(statefulSet *appsv1.StatefulSet, podList *corev1.PodList,
	logger logr.Logger) (*ctrl.Result, error) {
	ispn := r.infinispan
	if ispn.Spec.Replicas == 0 {
		logger.Info(".Spec.Replicas==0")
		if *statefulSet.Spec.Replicas != 0 {
			logger.Info("StatefulSet.Spec.Replicas!=0")
			// If cluster hasn't a `stopping` condition or it's false then send a graceful shutdown
			if !ispn.IsConditionTrue(infinispanv1.ConditionStopping) {
				return r.gracefulShutdownReq(podList, logger)
			}

			// If the cluster is stopping, then set statefulset replicas and ispn.replicas to 0
			if err := r.update(func() {
				ispn.Status.ReplicasWantedAtRestart = *statefulSet.Spec.Replicas
			}); err != nil {
				return &ctrl.Result{}, err
			}
			statefulSet.Spec.Replicas = pointer.Int32Ptr(0)
			if err := r.Client.Update(r.ctx, statefulSet); err != nil {
				if errors.IsConflict(err) {
					logger.Info("Requeuing request due to conflict on StatefulSet replicas update.")
					return &ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
				return &ctrl.Result{}, err
			}
		}

		return &ctrl.Result{Requeue: true}, r.update(func() {
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
			return &ctrl.Result{Requeue: true}, fmt.Errorf("Spec.Replicas(%d) must be 0 or equal to Status.ReplicasWantedAtRestart(%d)", ispn.Spec.Replicas, ispn.Status.ReplicasWantedAtRestart)
		}

		if err := r.update(func() {
			ispn.SetCondition(infinispanv1.ConditionGracefulShutdown, metav1.ConditionFalse, "")
			ispn.Status.ReplicasWantedAtRestart = 0
		}); err != nil {
			return &ctrl.Result{}, err
		}

		return &ctrl.Result{Requeue: true}, nil
	}
	return nil, nil
}

// gracefulShutdownReq send a graceful shutdown request to the cluster
func (r *infinispanRequest) gracefulShutdownReq(podList *corev1.PodList, logger logr.Logger) (*ctrl.Result, error) {
	ispn := r.infinispan
	logger.Info("Sending graceful shutdown request")
	// Send a graceful shutdown to the first ready pod. If no pods are ready, then there's nothing to shutdown
	var shutdownExecuted bool
	for _, pod := range podList.Items {
		if kube.IsPodReady(pod) {
			ispnClient, err := NewInfinispanForPod(r.ctx, pod.Name, r.infinispan, r.kubernetes)
			if err != nil {
				return &ctrl.Result{}, fmt.Errorf("unable to create Infinispan client for ready pod '%s': %w", pod.Name, err)
			}

			// This will fail on 12.x servers as the method does not exist
			if err := ispnClient.Container().Shutdown(); err != nil {
				logger.Error(err, "Error encountered on container shutdown. Attempting to execute GracefulShutdownTask")

				if err := ispnClient.Container().ShutdownTask(); err != nil {
					logger.Error(err, fmt.Sprintf("Error encountered using GracefulShutdownTask on pod %s", pod.Name))
					continue
				} else {
					shutdownExecuted = true
					break
				}
			} else {
				shutdownExecuted = true
				logger.Info("Executed graceful shutdown on pod: ", "Pod.Name", pod.Name)
				break
			}
		}
	}

	if shutdownExecuted {
		logger.Info("GracefulShutdown executed")
		if err := r.update(func() {
			ispn.SetCondition(infinispanv1.ConditionStopping, metav1.ConditionTrue, "")
			ispn.SetCondition(infinispanv1.ConditionWellFormed, metav1.ConditionFalse, "")
		}); err != nil {
			return &ctrl.Result{}, err
		}
	}
	// Stop the work and requeue until cluster is down
	return &ctrl.Result{Requeue: true, RequeueAfter: time.Second}, nil
}

// reconcileContainerConf reconcile the .Container struct is changed in .Spec. This needs a cluster restart
func (r *infinispanRequest) reconcileContainerConf(statefulSet *appsv1.StatefulSet, configMap, overlayConfigMap *corev1.ConfigMap, overlayConfigMapKey string, adminSecret,
	userSecret, keystoreSecret, trustSecret *corev1.Secret) (*ctrl.Result, error) {
	ispn := r.infinispan
	updateNeeded := false
	rollingUpgrade := true
	// Ensure the deployment size is the same as the spec
	replicas := ispn.Spec.Replicas
	previousReplicas := *statefulSet.Spec.Replicas
	if previousReplicas != replicas {
		statefulSet.Spec.Replicas = &replicas
		r.reqLogger.Info("replicas changed, update infinispan", "replicas", replicas, "previous replicas", previousReplicas)
		updateNeeded = true
		rollingUpgrade = false
	}

	// Changes to statefulset.spec.template.spec.containers[].resources
	spec := &statefulSet.Spec.Template.Spec
	res := spec.Containers[0].Resources
	ispnContr := &ispn.Spec.Container
	if ispnContr.Memory != "" {
		memRequests, memLimits, err := ispn.Spec.Container.GetMemoryResources()
		if err != nil {
			return &ctrl.Result{}, err
		}
		previousMemRequests := res.Requests["memory"]
		previousMemLimits := res.Limits["memory"]
		if memRequests.Cmp(previousMemRequests) != 0 || memLimits.Cmp(previousMemLimits) != 0 {
			res.Requests["memory"] = memRequests
			res.Limits["memory"] = memLimits
			r.reqLogger.Info("memory changed, update infinispan", "memLim", memLimits, "cpuReq", memRequests, "previous cpuLim", previousMemLimits, "previous cpuReq", previousMemRequests)
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			updateNeeded = true
		}
	}
	if ispnContr.CPU != "" {
		cpuReq, cpuLim, err := ispn.Spec.Container.GetCpuResources()
		if err != nil {
			return &ctrl.Result{}, err
		}
		previousCPUReq := res.Requests["cpu"]
		previousCPULim := res.Limits["cpu"]
		if cpuReq.Cmp(previousCPUReq) != 0 || cpuLim.Cmp(previousCPULim) != 0 {
			res.Requests["cpu"] = cpuReq
			res.Limits["cpu"] = cpuLim
			r.reqLogger.Info("cpu changed, update infinispan", "cpuLim", cpuLim, "cpuReq", cpuReq, "previous cpuLim", previousCPULim, "previous cpuReq", previousCPUReq)
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			updateNeeded = true
		}
	}

	if !reflect.DeepEqual(spec.Affinity, ispn.Spec.Affinity) {
		spec.Affinity = ispn.Spec.Affinity
		updateNeeded = true
	}

	// Validate ConfigMap changes (by the hash of the infinispan.yaml key value)
	updateNeeded = updateStatefulSetEnv(statefulSet, "CONFIG_HASH", hash.HashString(configMap.Data[consts.ServerConfigFilename])) || updateNeeded
	updateNeeded = updateStatefulSetEnv(statefulSet, "ADMIN_IDENTITIES_HASH", hash.HashByte(adminSecret.Data[consts.ServerIdentitiesFilename])) || updateNeeded

	if updateCmdArgs, err := updateStartupArgs(statefulSet, overlayConfigMapKey, "false"); err != nil {
		return &ctrl.Result{}, err
	} else {
		updateNeeded = updateCmdArgs || updateNeeded
	}
	var hashVal string
	if overlayConfigMapKey != "" {
		hashVal = hash.HashString(overlayConfigMap.Data[overlayConfigMapKey])
	}
	updateNeeded = updateStatefulSetAnnotations(statefulSet, "checksum/overlayConfig", hashVal) || updateNeeded
	updateNeeded = applyOverlayConfigVolume(overlayConfigMap, &statefulSet.Spec.Template.Spec) || updateNeeded

	externalArtifactsUpd, err := applyExternalArtifactsDownload(ispn, &statefulSet.Spec.Template.Spec)
	if err != nil {
		return &ctrl.Result{}, err
	}
	updateNeeded = externalArtifactsUpd || updateNeeded
	updateNeeded = applyExternalDependenciesVolume(ispn, &statefulSet.Spec.Template.Spec) || updateNeeded

	// Validate identities Secret name changes
	if secretName, secretIndex := findSecretInVolume(&statefulSet.Spec.Template.Spec, IdentitiesVolumeName); secretIndex >= 0 && secretName != ispn.GetSecretName() {
		// Update new Secret name inside StatefulSet.Spec.Template
		statefulSet.Spec.Template.Spec.Volumes[secretIndex].Secret.SecretName = ispn.GetSecretName()
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		updateNeeded = true
	}

	if ispn.IsAuthenticationEnabled() {
		if AddVolumeForUserAuthentication(ispn, spec) {
			if userSecret == nil {
				return &ctrl.Result{}, fmt.Errorf("user secret is nil. Requeueing")
			}
			spec.Containers[0].Env = append(spec.Containers[0].Env,
				corev1.EnvVar{Name: "IDENTITIES_HASH", Value: hash.HashByte(userSecret.Data[consts.ServerIdentitiesFilename])},
			)
			updateNeeded = true
		} else {
			// Validate Secret changes (by the hash of the identities.yaml key value)
			updateNeeded = updateStatefulSetEnv(statefulSet, "IDENTITIES_HASH", hash.HashByte(userSecret.Data[consts.ServerIdentitiesFilename])) || updateNeeded
		}
	}

	if ispn.IsEncryptionEnabled() {
		AddVolumesForEncryption(ispn, spec)
		if keystoreSecret == nil {
			// ispn, has been modified in the while, keystore
			// can't be nil if encryption is enabled
			return &reconcile.Result{}, fmt.Errorf("keystoreSecret is nil with encryption enabled")
		}
		updateNeeded = updateStatefulSetEnv(statefulSet, "KEYSTORE_HASH", hash.HashMap(keystoreSecret.Data)) || updateNeeded

		if ispn.IsClientCertEnabled() {
			if trustSecret == nil {
				// ispn, has been modified in the while, keystore
				// can't be nil if encryption is enabled
				return &reconcile.Result{}, fmt.Errorf("trustSecret is nil with encryption enabled")
			}
			updateNeeded = updateStatefulSetEnv(statefulSet, "TRUSTSTORE_HASH", hash.HashMap(trustSecret.Data)) || updateNeeded
		}
	}

	// Validate extra Java options changes
	if updateStatefulSetEnv(statefulSet, "EXTRA_JAVA_OPTIONS", ispnContr.ExtraJvmOpts) {
		updateStatefulSetEnv(statefulSet, "JAVA_OPTIONS", ispn.GetJavaOptions())
		updateNeeded = true
	}

	if updateNeeded {
		r.reqLogger.Info("updateNeeded")
		// If updating the parameters results in a rolling upgrade, we can update the labels here too
		if rollingUpgrade {
			labelsForPod := PodLabels(ispn.Name)
			ispn.AddOperatorLabelsForPods(labelsForPod)
			ispn.AddLabelsForPods(labelsForPod)
			ispn.AddStatefulSetLabelForPods(labelsForPod)
			statefulSet.Spec.Template.Labels = labelsForPod
		}
		err := r.Client.Update(r.ctx, statefulSet)
		if err != nil {
			r.reqLogger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
			return &ctrl.Result{}, err
		}
		// Spec updated - return and requeue
		return &ctrl.Result{Requeue: true}, nil
	}
	return nil, nil
}

func updateStatefulSetEnv(statefulSet *appsv1.StatefulSet, envName, newValue string) bool {
	env := &statefulSet.Spec.Template.Spec.Containers[0].Env
	envIndex := kube.GetEnvVarIndex(envName, env)
	if envIndex < 0 {
		// The env variable previously didn't exist, so append newValue to the end of the []EnvVar
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(*env, corev1.EnvVar{
			Name:  envName,
			Value: newValue,
		})
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		return true
	}
	prevEnvValue := (*env)[envIndex].Value
	if prevEnvValue != newValue {
		(*env)[envIndex].Value = newValue
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		return true
	}
	return false
}

func updateStatefulSetAnnotations(statefulSet *appsv1.StatefulSet, name, value string) bool {
	// Annotation has non-empty value
	if value != "" {
		// map doesn't exists, must be created
		if statefulSet.Annotations == nil {
			statefulSet.Annotations = make(map[string]string)
		}
		if statefulSet.Annotations[name] != value {
			statefulSet.Annotations[name] = value
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			return true
		}
	} else {
		// Annotation doesn't exist
		if statefulSet.Annotations == nil || statefulSet.Annotations[name] == "" {
			return false
		}
		// delete it
		delete(statefulSet.Annotations, name)
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

func (r *infinispanRequest) update(update UpdateFn, ignoreNotFound ...bool) error {
	ispn := r.infinispan
	_, err := kube.CreateOrPatch(r.ctx, r.Client, ispn, func() error {
		if ispn.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("infinispan.infinispan.org"), ispn.Name)
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

func (reconciler *InfinispanReconciler) isTypeSupported(kind string) bool {
	return reconciler.supportedTypes[kind].GroupVersionSupported
}

// GossipRouterPodList returns a list of pods where JGroups Gossip Router is running
func GossipRouterPodList(infinispan *infinispanv1.Infinispan, kube *kube.Kubernetes, ctx context.Context) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	return podList, kube.ResourcesList(infinispan.Namespace, GossipRouterPodLabels(infinispan.Name), podList, ctx)
}

func buildStartupArgs(overlayConfigMapKey string, zeroCapacity string) []string {
	var args []string
	if overlayConfigMapKey != "" {
		args = []string{"-Dinfinispan.zero-capacity-node=" + zeroCapacity, "-l", OperatorConfMountPath + "/log4j.xml", "-c", "user/" + overlayConfigMapKey, "-c", "operator/infinispan.xml"}
	} else {
		args = []string{"-Dinfinispan.zero-capacity-node=" + zeroCapacity, "-l", OperatorConfMountPath + "/log4j.xml", "-c", "operator/infinispan.xml"}
	}
	return args
}

func updateStartupArgs(statefulSet *appsv1.StatefulSet, overlayConfigMapKey, zeroCapacity string) (bool, error) {
	newArgs := buildStartupArgs(overlayConfigMapKey, zeroCapacity)
	if len(newArgs) == len(statefulSet.Spec.Template.Spec.Containers[0].Args) {
		var changed bool
		for i := range newArgs {
			if newArgs[i] != statefulSet.Spec.Template.Spec.Containers[0].Args[i] {
				changed = true
				break
			}
		}
		if !changed {
			return false, nil
		}
	}
	statefulSet.Spec.Template.Spec.Containers[0].Args = newArgs
	return true, nil
}

func applyOverlayConfigVolume(configMap *corev1.ConfigMap, spec *corev1.PodSpec) bool {
	volumes := &spec.Volumes
	volumeMounts := &spec.Containers[0].VolumeMounts
	volumePosition := findVolume(*volumes, UserConfVolumeName)
	if configMap.Name != "" {
		// Add the overlay volume if needed
		if volumePosition < 0 {
			*volumeMounts = append(*volumeMounts, corev1.VolumeMount{Name: UserConfVolumeName, MountPath: OverlayConfigMountPath})
			*volumes = append(*volumes, corev1.Volume{
				Name: UserConfVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: configMap.Name}}}})
			return true
		} else {
			// Update the overlay volume if needed
			if (*volumes)[volumePosition].VolumeSource.ConfigMap.Name != configMap.Name {
				(*volumes)[volumePosition].VolumeSource.ConfigMap.Name = configMap.Name
				return true
			}
		}
	}
	// Delete overlay volume mount if no more needed
	if configMap.Name == "" && volumePosition >= 0 {
		volumeMountPosition := findVolumeMount(*volumeMounts, UserConfVolumeName)
		*volumes = append(spec.Volumes[:volumePosition], spec.Volumes[volumePosition+1:]...)
		*volumeMounts = append(spec.Containers[0].VolumeMounts[:volumeMountPosition], spec.Containers[0].VolumeMounts[volumeMountPosition+1:]...)
		return true
	}
	return false
}
