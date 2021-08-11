/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

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

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnCtrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/caches"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/common/log"
	"honnef.co/go/tools/version"
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
	k8sctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ServerRoot                  = "/opt/infinispan/server"
	DataMountPath               = ServerRoot + "/data"
	DataMountVolume             = "data-volume"
	ConfigVolumeName            = "config-volume"
	EncryptKeystoreVolumeName   = "encrypt-volume"
	EncryptTruststoreVolumeName = "encrypt-trust-volume"
	IdentitiesVolumeName        = "identities-volume"
	AdminIdentitiesVolumeName   = "admin-identities-volume"
	ControllerName              = "controller_infinispan"

	EventReasonPrelimChecksFailed    = "PrelimChecksFailed"
	EventReasonLowPersistenceStorage = "LowPersistenceStorage"
	EventReasonEphemeralStorage      = "EphemeralStorageEnables"
	EventReasonParseValueProblem     = "ParseValueProblem"
	EventLoadBalancerUnsupported     = "LoadBalancerUnsupported"
)

// var log = logf.Log.WithName(ControllerName)
var infinispanEventRec record.EventRecorder

// Kubernetes object
var kubernetes *kube.Kubernetes

// // Add creates a new Infinispan Controller and adds it to the Manager. The Manager will set fields on the Controller
// // and Start it when the Manager is Started.
// func Add(mgr manager.Manager) error {
// 	return add(mgr, newReconciler(mgr))
// }

// // newReconciler returns a new reconcile.Reconciler
// func newReconciler(mgr manager.Manager) reconcile.Reconciler {
// 	kubernetes = kube.NewKubernetesFromController(mgr)
// 	eventRec = mgr.GetEventRecorderFor(ControllerName)
// 	return &ReconcileInfinispan{client: mgr.GetClient(), scheme: mgr.GetScheme()}
// }

var InfinispanPredicate predicate.Funcs = predicate.Funcs{
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
}

func secondaryResourceTypes() []client.Object {
	return []client.Object{
		&appsv1.StatefulSet{}, &corev1.ConfigMap{}, &corev1.Secret{},
	}
}

var supportedTypes = map[string]*resources.ReconcileType{
	consts.ExternalTypeRoute:   {ObjectType: &routev1.Route{}, GroupVersion: routev1.SchemeGroupVersion, GroupVersionSupported: false},
	consts.ExternalTypeIngress: {ObjectType: &ingressv1.Ingress{}, GroupVersion: ingressv1.SchemeGroupVersion, GroupVersionSupported: false},
	consts.ServiceMonitorType:  {ObjectType: &monitoringv1.ServiceMonitor{}, GroupVersion: monitoringv1.SchemeGroupVersion, GroupVersionSupported: false},
}

func isTypeSupported(kind string) bool {
	return supportedTypes[kind].GroupVersionSupported
}

// InfinispanReconciler reconciles a Infinispan object
type InfinispanReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infinispan.org,resources=infinispans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infinispan.org,resources=infinispans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infinispan.org,resources=infinispans/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Infinispan object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *InfinispanReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info(fmt.Sprintf("+++++ Reconciling Infinispan. Operator Version: %s", version.Version))
	defer reqLogger.Info("----- End Reconciling Infinispan.")

	// Fetch the Infinispan instance
	infinispan := &infinispanv1.Infinispan{}
	infinispan.Name = request.Name
	infinispan.Namespace = request.Namespace

	var preliminaryChecksResult *ctrl.Result
	var preliminaryChecksError error
	err := r.update(infinispan, func() {
		// Apply defaults and endpoint encryption settings if not already set
		infinispan.ApplyDefaults()
		if isTypeSupported(consts.ServiceMonitorType) {
			infinispan.ApplyMonitoringAnnotation()
		}
		infinispan.Spec.Affinity = podAffinity(infinispan, PodLabels(infinispan.Name))
		errLabel := infinispan.ApplyOperatorLabels()
		if errLabel != nil {
			reqLogger.Error(errLabel, "Error applying operator label")
		}
		infinispan.ApplyEndpointEncryptionSettings(kubernetes.GetServingCertsMode(), reqLogger)

		// Perform all the possible preliminary checks before go on
		preliminaryChecksResult, preliminaryChecksError = infinispan.PreliminaryChecks()
		if preliminaryChecksError != nil {
			infinispanEventRec.Event(infinispan, corev1.EventTypeWarning, EventReasonPrelimChecksFailed, preliminaryChecksError.Error())
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
	if result, err := kube.LookupResource(infinispan.GetConfigName(), infinispan.Namespace, configMap, r.Client, reqLogger, infinispanEventRec); result != nil {
		return *result, err
	}

	// Wait for the Secret to be created by secret-controller or provided by user
	var userSecret *corev1.Secret
	if infinispan.IsAuthenticationEnabled() {
		userSecret = &corev1.Secret{}
		if result, err := kube.LookupResource(infinispan.GetSecretName(), infinispan.Namespace, userSecret, r.Client, reqLogger, infinispanEventRec); result != nil {
			return *result, err
		}
	}

	adminSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(infinispan.GetAdminSecretName(), infinispan.Namespace, adminSecret, r.Client, reqLogger, infinispanEventRec); result != nil {
		return *result, err
	}

	var keystoreSecret *corev1.Secret
	if infinispan.IsEncryptionEnabled() {
		if infinispan.Spec.Security.EndpointEncryption.CertSecretName == "" {
			return ctrl.Result{}, fmt.Errorf("Field 'certSecretName' must be provided for certificateSourceType=%s to be configured", infinispanv1.CertificateSourceTypeSecret)
		}
		keystoreSecret = &corev1.Secret{}
		if result, err := kube.LookupResource(infinispan.GetKeystoreSecretName(), infinispan.Namespace, keystoreSecret, r.Client, reqLogger, infinispanEventRec); result != nil {
			return *result, err
		}
	}

	var trustSecret *corev1.Secret
	if infinispan.IsClientCertEnabled() {
		trustSecret = &corev1.Secret{}
		if result, err := kube.LookupResource(infinispan.GetTruststoreSecretName(), infinispan.Namespace, trustSecret, r.Client, reqLogger, infinispanEventRec); result != nil {
			return *result, err
		}
	}

	// Reconcile the StatefulSet
	// Check if the StatefulSet already exists, if not create a new one
	statefulSet := &appsv1.StatefulSet{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, statefulSet)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Configuring the StatefulSet")

		// Define a new StatefulSet
		statefulSet, err = r.statefulSetForInfinispan(infinispan, adminSecret, userSecret, keystoreSecret, trustSecret, configMap)
		if err != nil {
			reqLogger.Error(err, "failed to configure new StatefulSet")
			return ctrl.Result{}, err
		}
		reqLogger.Info("Creating a new StatefulSet", "StatefulSet.Name", statefulSet.Name)
		err = r.Client.Create(context.TODO(), statefulSet)
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

	// Update Pod's status for the OLM
	if err := r.update(infinispan, func() {
		infinispan.Status.PodStatus = GetSingleStatefulSetStatus(*statefulSet)
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Wait for the cluster Service to be created by service-controller
	if result, err := kube.LookupResource(infinispan.Name, infinispan.Namespace, &corev1.Service{}, r.Client, reqLogger, infinispanEventRec); result != nil {
		return *result, err
	}

	// Wait for the cluster ping Service to be created by service-controller
	if result, err := kube.LookupResource(infinispan.GetPingServiceName(), infinispan.Namespace, &corev1.Service{}, r.Client, reqLogger, infinispanEventRec); result != nil {
		return *result, err
	}

	if infinispan.IsUpgradeNeeded(reqLogger) {
		reqLogger.Info("Upgrade needed")
		err = r.destroyResources(infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to delete resources before upgrade")
			return ctrl.Result{}, err
		}

		if err := r.update(infinispan, func() {
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
	podList, err := PodList(infinispan)
	if err != nil {
		reqLogger.Error(err, "failed to list pods")
		return ctrl.Result{}, err
	}

	// Recover Pods with updated init containers in case of fails
	for _, pod := range podList.Items {
		if !kube.IsInitContainersEqual(statefulSet.Spec.Template.Spec.InitContainers, pod.Spec.InitContainers) {
			if kube.InitContainerFailed(pod.Status.InitContainerStatuses) {
				if err = r.Client.Delete(context.TODO(), &pod); err != nil {
					return ctrl.Result{}, err
				}
			}
		}
	}

	if err = r.updatePodsLabels(infinispan, podList); err != nil {
		return ctrl.Result{}, err
	}

	result, err := r.scheduleUpgradeIfNeeded(infinispan, podList, reqLogger)
	if result != nil {
		return *result, err
	}

	cluster, err := ispnCtrl.NewCluster(infinispan, kubernetes)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If user set Spec.replicas=0 we need to perform a graceful shutdown
	// to preserve the data
	var res *ctrl.Result
	res, err = r.reconcileGracefulShutdown(infinispan, statefulSet, podList, reqLogger, cluster)
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
	res, err = r.reconcileContainerConf(infinispan, statefulSet, configMap, adminSecret, userSecret, keystoreSecret, trustSecret, reqLogger)
	if res != nil {
		return *res, err
	}

	// Update the Infinispan status with the pod status
	// Wait until all pods have IPs assigned
	// Without those IPs, it's not possible to execute next calls

	if !kube.ArePodIPsReady(podList) {
		reqLogger.Info("Pods IPs are not ready yet")
		return ctrl.Result{}, r.update(infinispan, func() {
			infinispan.SetCondition(infinispanv1.ConditionWellFormed, metav1.ConditionUnknown, "Pods are not ready")
			infinispan.RemoveCondition(infinispanv1.ConditionCrossSiteViewFormed)
			infinispan.Status.StatefulSetName = statefulSet.Name
		})
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
		return ctrl.Result{}, err
	}

	// View didn't form, requeue until view has formed
	if infinispan.NotClusterFormed(len(podList.Items), int(infinispan.Spec.Replicas)) {
		reqLogger.Info("notClusterFormed")
		return ctrl.Result{RequeueAfter: consts.DefaultWaitClusterNotWellFormed}, nil
	}

	// Below the code for a wellFormed cluster
	err = configureLoggers(podList, cluster, infinispan)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Create default cache if it doesn't exists.
	if infinispan.IsCache() {
		if existsCache, err := cluster.ExistsCache(consts.DefaultCacheName, podList.Items[0].Name); err != nil {
			reqLogger.Error(err, "failed to validate default cache for cache service")
			return ctrl.Result{}, err
		} else if !existsCache {
			reqLogger.Info("createDefaultCache")
			if err = caches.CreateCacheFromDefault(podList.Items[0].Name, infinispan, cluster, reqLogger); err != nil {
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
			if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalService, r.Client, reqLogger, infinispanEventRec); result != nil {
				return *result, err
			}
			if len(externalService.Spec.Ports) > 0 && infinispan.GetExposeType() == infinispanv1.ExposeTypeNodePort {
				if exposeHost, err := kubernetes.GetNodeHost(reqLogger); err != nil {
					return ctrl.Result{}, err
				} else {
					exposeAddress = fmt.Sprintf("%s:%d", exposeHost, externalService.Spec.Ports[0].NodePort)
				}
			} else if infinispan.GetExposeType() == infinispanv1.ExposeTypeLoadBalancer {
				// Waiting for LoadBalancer cloud provider to update the configured hostname inside Status field
				if exposeAddress = kubernetes.GetExternalAddress(externalService); exposeAddress == "" {
					if !helpers.HasLBFinalizer(externalService) {
						errMsg := "LoadBalancer expose type is not supported on the target platform"
						infinispanEventRec.Event(externalService, corev1.EventTypeWarning, EventLoadBalancerUnsupported, errMsg)
						reqLogger.Info(errMsg)
						return ctrl.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
					}
					reqLogger.Info("LoadBalancer address not ready yet. Waiting on value in reconcile loop")
					return ctrl.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
				}
			}
		case infinispanv1.ExposeTypeRoute:
			if isTypeSupported(consts.ExternalTypeRoute) {
				externalRoute := &routev1.Route{}
				if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalRoute, r.Client, reqLogger, infinispanEventRec); result != nil {
					return *result, err
				}
				exposeAddress = externalRoute.Spec.Host
			} else if isTypeSupported(consts.ExternalTypeIngress) {
				externalIngress := &ingressv1.Ingress{}
				if result, err := kube.LookupResource(infinispan.GetServiceExternalName(), infinispan.Namespace, externalIngress, r.Client, reqLogger, infinispanEventRec); result != nil {
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
			return ctrl.Result{}, err
		}
	} else {
		if err := r.update(infinispan, func() {
			infinispan.Status.ConsoleUrl = nil
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// If x-site enable configure the coordinator pods to be selected by the x-site service
	if infinispan.HasSites() {
		crossSiteViewCondition, err := r.applyLabelsToCoordinatorsPod(podList, infinispan.GetSiteLocationsName(), cluster)
		if err != nil {
			return ctrl.Result{}, err
		}
		// ISPN-13116 If xsite view has been formed, then we must perform state-transfer to all sites if a SFS recovery has occurred
		if crossSiteViewCondition.Status == metav1.ConditionTrue {
			podName := podList.Items[0].Name
			logs, err := kubernetes.Logs(podName, infinispan.Namespace)
			if err != nil {
				log.Error(err, fmt.Sprintf("Unable to retrive logs for infinispan pod %s", podName))
			}
			if strings.Contains(logs, "ISPN000643") {
				if err := cluster.XsitePushAllState(podName); err != nil {
					log.Error(err, "Unable to push xsite state after SFS data recovery")
				}
			}
		}
		err = r.update(infinispan, func() {
			infinispan.SetConditions([]infinispanv1.InfinispanCondition{*crossSiteViewCondition})
		})
		if err != nil || crossSiteViewCondition.Status != metav1.ConditionTrue {
			return ctrl.Result{RequeueAfter: consts.DefaultWaitOnCluster}, err
		}
	}

	return ctrl.Result{}, nil
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

func (r *InfinispanReconciler) destroyResources(infinispan *infinispanv1.Infinispan) error {
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

	err = r.Client.Delete(context.TODO(),
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.Name,
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(context.TODO(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetConfigName(),
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.Name,
				Namespace: infinispan.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.Client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.GetPingServiceName(),
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

	if isTypeSupported(consts.ExternalTypeRoute) {
		err = r.Client.Delete(context.TODO(),
			&routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      infinispan.GetServiceExternalName(),
					Namespace: infinispan.Namespace,
				},
			})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	} else if isTypeSupported(consts.ExternalTypeIngress) {
		err = r.Client.Delete(context.TODO(),
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

	err = r.Client.Delete(context.TODO(),
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

func (r *InfinispanReconciler) upgradeInfinispan(infinispan *infinispanv1.Infinispan) error {
	// Remove controller owner reference from the custom Secrets
	for _, secretName := range []string{infinispan.GetKeystoreSecretName(), infinispan.GetTruststoreSecretName()} {
		if err := r.dropSecretOwnerReference(secretName, infinispan); err != nil {
			return err
		}
	}

	if contains(infinispan.GetFinalizers(), consts.InfinispanFinalizer) {
		// Set Infinispan CR as owner reference for PVC if it not defined
		pvcs := &corev1.PersistentVolumeClaimList{}
		err := kubernetes.ResourcesList(infinispan.Namespace, LabelsResource(infinispan.Name, ""), pvcs)
		if err != nil {
			return err
		}

		for _, pvc := range pvcs.Items {
			if !metav1.IsControlledBy(&pvc, infinispan) {
				if err = controllerutil.SetControllerReference(infinispan, &pvc, r.Scheme); err != nil {
					return err
				}
				pvc.OwnerReferences[0].BlockOwnerDeletion = pointer.BoolPtr(false)
				err := r.Client.Update(context.TODO(), &pvc)
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

func (r *InfinispanReconciler) dropSecretOwnerReference(secretName string, infinispan *infinispanv1.Infinispan) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: infinispan.Namespace,
		},
	}
	_, err := k8sctrlutil.CreateOrPatch(context.TODO(), r.Client, secret, func() error {
		if secret.CreationTimestamp.IsZero() {
			return errors.NewNotFound(corev1.Resource(""), secretName)
		}
		kube.RemoveOwnerReference(secret, infinispan)
		return nil
	})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *InfinispanReconciler) scheduleUpgradeIfNeeded(infinispan *infinispanv1.Infinispan, podList *corev1.PodList, logger logr.Logger) (*ctrl.Result, error) {
	if upgrade, err := upgradeRequired(infinispan, podList); upgrade || err != nil {
		if err := r.update(infinispan, func() {
			podDefaultImage := kube.GetPodDefaultImage(podList.Items[0].Spec.Containers[0])
			logger.Info("schedule an Infinispan cluster upgrade", "pod default image", podDefaultImage, "desired image", consts.DefaultImageName)
			infinispan.SetCondition(infinispanv1.ConditionUpgrade, metav1.ConditionTrue, "")
			infinispan.Spec.Replicas = 0
		}); err != nil {
			return &ctrl.Result{}, err
		}
	}
	return nil, nil
}

func upgradeRequired(infinispan *infinispanv1.Infinispan, podList *corev1.PodList) (bool, error) {
	if len(podList.Items) == 0 {
		return false, nil
	}
	if infinispan.IsUpgradeCondition() {
		return false, nil
	}

	// All pods need to be ready for the upgrade to be scheduled
	// Handles brief window during whichstatefulSetForInfinispan resources have been removed,
	//and old ones terminating while new ones are being created.
	// We don't want yet another upgrade to be scheduled then.
	if !kube.AreAllPodsReady(podList) {
		return false, nil
	}

	// Get default Infinispan image for a running Infinispan pod
	podDefaultImage := kube.GetPodDefaultImage(podList.Items[0].Spec.Containers[0])

	// Get Infinispan image that the operator creates
	desiredImage := consts.DefaultImageName

	// If the operator's default image differs from the pod's default image,
	// schedule an upgrade by gracefully shutting down the current cluster.
	return podDefaultImage != desiredImage, nil
}

func IsUpgradeRequired(infinispan *infinispanv1.Infinispan) (bool, error) {
	podList, err := PodList(infinispan)
	if err != nil {
		return false, err
	}
	return upgradeRequired(infinispan, podList)
}

func PodList(infinispan *infinispanv1.Infinispan) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	return podList, kubernetes.ResourcesList(infinispan.Namespace, PodLabels(infinispan.Name), podList)
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

func GetSingleStatefulSetStatus(ss appsv1.StatefulSet) v1.DeploymentStatus {
	return getSingleDeploymentStatus(ss.Name, getInt32(ss.Spec.Replicas), ss.Status.Replicas, ss.Status.ReadyReplicas)
}

func getInt32(pointer *int32) int32 {
	if pointer == nil {
		return 0
	} else {
		return *pointer
	}

}
func getSingleDeploymentStatus(name string, requestedCount int32, targetCount int32, readyCount int32) v1.DeploymentStatus {
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
	return v1.DeploymentStatus{
		Stopped:  stopped,
		Starting: starting,
		Ready:    ready,
	}

}

func (r *InfinispanReconciler) updatePodsLabels(m *infinispanv1.Infinispan, podList *corev1.PodList) error {
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

		_, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, &pod, func() error {
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
func (r *InfinispanReconciler) statefulSetForInfinispan(m *infinispanv1.Infinispan, adminSecret, userSecret, keystoreSecret, trustSecret *corev1.Secret,
	configMap *corev1.ConfigMap) (*appsv1.StatefulSet, error) {
	reqLogger := r.Log.WithValues("Request.Namespace", m.Namespace, "Request.Name", m.Name)
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
		infinispanEventRec.Event(m, corev1.EventTypeWarning, EventReasonParseValueProblem, err.Error())
		reqLogger.Info(err.Error())
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
							{Name: "ADMIN_IDENTITIES_HASH", Value: HashByte(adminSecret.Data[consts.ServerIdentitiesFilename])},
						}),
						LivenessProbe:  ispnCtrl.PodLivenessProbe(),
						Ports:          ispnCtrl.PodPortsWithXsite(m),
						ReadinessProbe: ispnCtrl.PodReadinessProbe(),
						StartupProbe:   ispnCtrl.PodStartupProbe(),
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
				Value: HashByte(userSecret.Data[consts.ServerIdentitiesFilename]),
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
				errMsg := "Persistent volume size is less than memory size. Graceful shutdown may not work."
				infinispanEventRec.Event(m, corev1.EventTypeWarning, EventReasonLowPersistenceStorage, errMsg)
				reqLogger.Info(errMsg, "Volume Size", pvSize, "Memory", memory)
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

		if err = controllerutil.SetControllerReference(m, pvc, r.Scheme); err != nil {
			return nil, err
		}
		pvc.OwnerReferences[0].BlockOwnerDeletion = pointer.BoolPtr(false)
		// Set a storage class if it specified
		if storageClassName := m.StorageClassName(); storageClassName != "" {
			if _, err := kube.FindStorageClass(storageClassName, r.Client); err != nil {
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
		infinispanEventRec.Event(m, corev1.EventTypeWarning, EventReasonEphemeralStorage, errMsg)
		reqLogger.Info(errMsg)
	}

	if _, err := applyExternalArtifactsDownload(m, &dep.Spec.Template.Spec); err != nil {
		return nil, err
	}

	applyExternalDependenciesVolume(m, &dep.Spec.Template.Spec)
	if m.IsEncryptionEnabled() {
		AddVolumesForEncryption(m, &dep.Spec.Template.Spec)
		spec.Containers[0].Env = append(spec.Containers[0].Env,
			corev1.EnvVar{
				Name:  "KEYSTORE_HASH",
				Value: HashMap(keystoreSecret.Data),
			})

		if m.IsClientCertEnabled() {
			spec.Containers[0].Env = append(spec.Containers[0].Env,
				corev1.EnvVar{
					Name:  "TRUSTSTORE_HASH",
					Value: HashMap(trustSecret.Data),
				})
		}
	}

	// Set Infinispan instance as the owner and controller
	if err = controllerutil.SetControllerReference(m, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

// AddVolumeForUserAuthentication returns true if the volume has been added
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

// func PodPorts() []corev1.ContainerPort {
// 	ports := []corev1.ContainerPort{
// 		{ContainerPort: consts.InfinispanAdminPort, Name: consts.InfinispanAdminPortName, Protocol: corev1.ProtocolTCP},
// 		{ContainerPort: consts.InfinispanPingPort, Name: consts.InfinispanPingPortName, Protocol: corev1.ProtocolTCP},
// 		{ContainerPort: consts.InfinispanUserPort, Name: consts.InfinispanUserPortName, Protocol: corev1.ProtocolTCP},
// 	}
// 	return ports
// }

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

// AddVolumeChmodInitContainer adds an init container that run chmod if needed
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

func AddVolumesForEncryption(i *infinispanv1.Infinispan, spec *corev1.PodSpec) {
	addSecretVolume := func(secretName, volumeName, mountPath string, spec *corev1.PodSpec) {
		v := &spec.Volumes

		if _, index := findSecretInVolume(spec, volumeName); index < 0 {
			*v = append(*v, corev1.Volume{Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretName,
					},
				},
			})
		}

		volumeMount := corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
		}

		index := -1
		volumeMounts := &spec.Containers[0].VolumeMounts
		for i, vm := range *volumeMounts {
			if vm.Name == volumeName {
				index = i
				break
			}
		}

		if index < 0 {
			*volumeMounts = append(*volumeMounts, volumeMount)
		} else {
			(*volumeMounts)[index] = volumeMount
		}
	}

	addSecretVolume(i.GetKeystoreSecretName(), EncryptKeystoreVolumeName, consts.ServerEncryptKeystoreRoot, spec)

	if i.IsClientCertEnabled() {
		addSecretVolume(i.GetTruststoreSecretName(), EncryptTruststoreVolumeName, consts.ServerEncryptTruststoreRoot, spec)
	}
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

func (r *InfinispanReconciler) reconcileGracefulShutdown(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet,
	podList *corev1.PodList, logger logr.Logger, cluster ispn.ClusterInterface) (*ctrl.Result, error) {
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
						return &ctrl.Result{Requeue: true, RequeueAfter: time.Second}, nil
					}
				}
			}

			// If here all the pods are unready, set statefulset replicas and ispn.replicas to 0
			if err := r.update(ispn, func() {
				ispn.Status.ReplicasWantedAtRestart = *statefulSet.Spec.Replicas
			}); err != nil {
				return &ctrl.Result{}, err
			}
			statefulSet.Spec.Replicas = pointer.Int32Ptr(0)
			err := r.Client.Update(context.TODO(), statefulSet)
			if err != nil {
				logger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
				return &ctrl.Result{}, err
			}
		}

		return &ctrl.Result{Requeue: true}, r.update(ispn, func() {
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

		if err := r.update(ispn, func() {
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
func (r *InfinispanReconciler) gracefulShutdownReq(ispn *infinispanv1.Infinispan, podList *corev1.PodList,
	logger logr.Logger, cluster ispn.ClusterInterface) (*ctrl.Result, error) {
	logger.Info("Sending graceful shutdown request")
	// Send a graceful shutdown to the first ready pod. If no pods are ready, then there's nothing to shutdown
	for _, pod := range podList.Items {
		if kube.IsPodReady(pod) {
			if err := cluster.GracefulShutdown(pod.GetName()); err != nil {
				logger.Error(err, "Error encountered on cluster shutdown")

				// ISPN-13141 causes GracefulShutdown to fail if there are issues with the server
				logger.Info("Cluster Shutdown failed. Attempting to execute GracefulShutdownTask")
				if err := cluster.GracefulShutdownTask(pod.GetName()); err != nil {
					logger.Error(err, fmt.Sprintf("Error encountered using GracefulShutdownTask on pod %s", pod.Name))
					continue
				}
			} else {
				logger.Info("Executed graceful shutdown on pod: ", "Pod.Name", pod.Name)
				break
			}
		}
	}

	logger.Info("GracefulShutdown executed. Deleting all pods")
	deleteOptions := []client.DeleteAllOfOption{client.MatchingLabels(PodLabels(ispn.Name)), client.InNamespace(ispn.Namespace)}
	if err := r.Client.DeleteAllOf(context.TODO(), &corev1.Pod{}, deleteOptions...); err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Error encountered deleting all Pods on GracefulShutdown")
	}

	if err := r.update(ispn, func() {
		ispn.SetCondition(infinispanv1.ConditionStopping, metav1.ConditionTrue, "")
		ispn.SetCondition(infinispanv1.ConditionWellFormed, metav1.ConditionFalse, "")
	}); err != nil {
		return &ctrl.Result{}, err
	}
	// Stop the work and requeue until cluster is down
	return &ctrl.Result{Requeue: true, RequeueAfter: time.Second}, nil
}

// reconcileContainerConf reconcile the .Container struct is changed in .Spec. This needs a cluster restart
func (r *InfinispanReconciler) reconcileContainerConf(ispn *infinispanv1.Infinispan, statefulSet *appsv1.StatefulSet,
	configMap *corev1.ConfigMap, adminSecret, userSecret, keystoreSecret, trustSecret *corev1.Secret, logger logr.Logger) (*ctrl.Result, error) {
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
			return &ctrl.Result{}, err
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
			return &ctrl.Result{}, err
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
	updateNeeded = updateStatefulSetEnv(statefulSet, "ADMIN_IDENTITIES_HASH", HashByte(adminSecret.Data[consts.ServerIdentitiesFilename])) || updateNeeded

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
			spec.Containers[0].Env = append(spec.Containers[0].Env,
				corev1.EnvVar{Name: "IDENTITIES_HASH", Value: HashByte(userSecret.Data[consts.ServerIdentitiesFilename])},
				corev1.EnvVar{Name: "IDENTITIES_PATH", Value: consts.ServerUserIdentitiesPath},
			)
			updateNeeded = true
		} else {
			// Validate Secret changes (by the hash of the identities.yaml key value)
			updateNeeded = updateStatefulSetEnv(statefulSet, "IDENTITIES_HASH", HashByte(userSecret.Data[consts.ServerIdentitiesFilename])) || updateNeeded
		}
	}

	if ispn.IsEncryptionEnabled() {
		AddVolumesForEncryption(ispn, spec)
		updateNeeded = updateStatefulSetEnv(statefulSet, "KEYSTORE_HASH", HashMap(keystoreSecret.Data)) || updateNeeded

		if ispn.IsClientCertEnabled() {
			updateNeeded = updateStatefulSetEnv(statefulSet, "TRUSTSTORE_HASH", HashMap(trustSecret.Data)) || updateNeeded
		}
	}

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
		err := r.Client.Update(context.TODO(), statefulSet)
		if err != nil {
			logger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
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

func (r *InfinispanReconciler) update(ispn *infinispanv1.Infinispan, update UpdateFn, ignoreNotFound ...bool) error {
	_, err := k8sctrlutil.CreateOrPatch(context.TODO(), r.Client, ispn, func() error {
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

// func ServiceLabels(name string) map[string]string {
// 	return map[string]string{
// 		"clusterName": name,
// 		"app":         "infinispan-pod",
// 	}
// }

func hashString(data string) string {
	hash := sha1.New()
	hash.Write([]byte(data))
	return hex.EncodeToString(hash.Sum(nil))
}

func HashByte(data []byte) string {
	hash := sha1.New()
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func HashMap(m map[string][]byte) string {
	hash := sha1.New()
	// Sort the map keys to ensure that the iteration order is the same on each call
	// Without this the computed sha will be different if the iteration order of the keys changes
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		hash.Write([]byte(k))
		hash.Write(v)
	}
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

// SetupWithManager sets up the controller with the Manager.
func (r *InfinispanReconciler) SetupWithManager(mgr ctrl.Manager) error {

	var err error
	kubernetes = kube.NewKubernetesFromController(mgr)
	infinispanEventRec = mgr.GetEventRecorderFor(ControllerName)

	// Add Secret name fields to the index for caching
	if err = mgr.GetFieldIndexer().IndexField(context.TODO(), &v1.Infinispan{}, "spec.security.endpointSecretName", func(obj client.Object) []string {
		return []string{obj.(*infinispanv1.Infinispan).GetSecretName()}
	}); err != nil {
		return err
	}
	if err = mgr.GetFieldIndexer().IndexField(context.TODO(), &v1.Infinispan{}, "spec.security.endpointEncryption.certSecretName", func(obj client.Object) []string {
		return []string{obj.(*infinispanv1.Infinispan).GetKeystoreSecretName()}
	}); err != nil {
		return err
	}
	if err = mgr.GetFieldIndexer().IndexField(context.TODO(), &v1.Infinispan{}, "spec.security.endpointEncryption.clientCertSecretName", func(obj client.Object) []string {
		return []string{obj.(*infinispanv1.Infinispan).GetTruststoreSecretName()}
	}); err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&infinispanv1.Infinispan{})

	for index, obj := range supportedTypes {
		// Validate that GroupVersion is supported on runtime platform
		ok, err := kubernetes.IsGroupVersionSupported(obj.GroupVersion.String(), obj.Kind())
		if err != nil {
			log.Error(err, fmt.Sprintf("Failed to check if GVK '%s' is supported", obj.GroupVersionKind()))
			continue
		}
		supportedTypes[index].GroupVersionSupported = ok
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Infinispan
	for _, secondaryResource := range secondaryResourceTypes() {
		builder.Owns(secondaryResource)
	}
	builder.WithEventFilter(InfinispanPredicate)

	builder.Watches(
		&source.Kind{Type: &corev1.Secret{}},
		handler.EnqueueRequestsFromMapFunc(
			func(a client.Object) []reconcile.Request {
				var requests []reconcile.Request
				// Lookup only Secrets not controlled by Infinispan CR GVK. This means it's a custom defined Secret
				if !kube.IsControlledByGVK(a.GetOwnerReferences(), v1.SchemeBuilder.GroupVersion.WithKind(reflect.TypeOf(infinispanv1.Infinispan{}).Name())) {
					for _, field := range []string{"spec.security.endpointSecretName", "spec.security.endpointEncryption.certSecretName", "spec.security.endpointEncryption.clientCertSecretName"} {
						ispnList := &infinispanv1.InfinispanList{}
						if err := kubernetes.ResourcesListByField(a.GetNamespace(), field, a.GetName(), ispnList); err != nil {
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
	return builder.Complete(r)
}
