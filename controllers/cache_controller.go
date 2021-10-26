package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	infinispanv2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	caches "github.com/infinispan/infinispan-operator/pkg/infinispan/caches"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// CacheReconciler reconciles a Cache object
type CacheReconciler struct {
	client.Client
	log        logr.Logger
	scheme     *runtime.Scheme
	kubernetes *kube.Kubernetes
	eventRec   record.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *CacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName("Cache")
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor("cache-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&infinispanv2alpha1.Cache{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=infinispan.org,namespace=infinispan-operator-system,resources=caches;caches/status;caches/finalizers,verbs=get;list;watch;create;update;patch

func (r *CacheReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("+++++ Reconciling Cache.")
	defer reqLogger.Info("----- End Reconciling Cache.")

	// Fetch the Cache instance
	instance := &infinispanv2alpha1.Cache{}
	err := r.Client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Cache resource not found. Ignoring it since cache deletion is not supported")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Spec.AdminAuth != nil {
		reqLogger.Info("Ignoring and removing 'spec.AdminAuth' field. The operator's admin credentials are now used to perform cache operations")
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, instance, func() error {
			instance.Spec.AdminAuth = nil
			return nil
		})
		return reconcile.Result{}, err
	}

	// Reconcile cache
	reqLogger.Info("Identify the target cluster")
	// Fetch the Infinispan cluster info
	ispnInstance := &infinispanv1.Infinispan{}
	nsName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.ClusterName}
	err = r.Client.Get(ctx, nsName, ispnInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Infinispan cluster %s not found", ispnInstance.Name))
			return reconcile.Result{RequeueAfter: constants.DefaultWaitOnCluster}, err
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Cluster must be well formed
	if !ispnInstance.IsWellFormed() {
		reqLogger.Info(fmt.Sprintf("Infinispan cluster %s not well formed", ispnInstance.Name))
		return reconcile.Result{RequeueAfter: constants.DefaultWaitOnCluster}, err
	}
	// List the pods for this infinispan's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(LabelsResource(ispnInstance.Name, ""))
	listOps := &client.ListOptions{Namespace: ispnInstance.GetClusterName(), LabelSelector: labelSelector}
	err = r.Client.List(ctx, podList, listOps)
	if err != nil || (len(podList.Items) == 0) {
		reqLogger.Error(err, "failed to list pods")
		return reconcile.Result{}, err
	} else if len(podList.Items) == 0 {
		reqLogger.Error(err, "No Infinispan pods found")
		return reconcile.Result{}, nil
	}

	cluster, err := NewCluster(ispnInstance, r.kubernetes, ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	existsCache, err := cluster.ExistsCache(instance.GetCacheName(), podList.Items[0].Name)
	if err == nil {
		if existsCache {
			reqLogger.Info(fmt.Sprintf("Cache %s already exists", instance.GetCacheName()))
			// Check if template matches?
		} else {
			reqLogger.Info(fmt.Sprintf("Cache %s doesn't exist, create it", instance.GetCacheName()))
			podName := podList.Items[0].Name
			templateName := instance.Spec.TemplateName
			if ispnInstance.Spec.Service.Type == infinispanv1.ServiceTypeCache && (templateName != "" || instance.Spec.Template != "") {
				errTemplate := fmt.Errorf("cannot create a cache with a template in a CacheService cluster")
				reqLogger.Error(errTemplate, "Error creating cache")
				return reconcile.Result{}, err
			}
			if templateName != "" {
				err = cluster.CreateCacheWithTemplateName(instance.Spec.Name, templateName, podName)
				if err != nil {
					reqLogger.Error(err, "Error creating cache with template name")
					return reconcile.Result{}, err
				}
			} else {
				xmlTemplate := instance.Spec.Template
				if xmlTemplate == "" {
					xmlTemplate, err = caches.DefaultCacheTemplateXML(podName, ispnInstance, cluster, reqLogger)
				}
				if err != nil {
					reqLogger.Error(err, "Error getting default XML")
					return reconcile.Result{}, err
				}
				reqLogger.Info(xmlTemplate)
				err = cluster.CreateCacheWithTemplate(instance.Spec.Name, xmlTemplate, podName)
				if err != nil {
					reqLogger.Error(err, "Error in creating cache")
					return reconcile.Result{}, err
				}
			}
		}
	} else {
		reqLogger.Error(err, "Error validating cache exist")
		return reconcile.Result{}, err
	}

	// Search the service associated to the cluster
	serviceList := &corev1.ServiceList{}
	labelSelector = labels.SelectorFromSet(LabelsResource(ispnInstance.Name, "infinispan-service"))
	listOps = &client.ListOptions{Namespace: ispnInstance.Namespace, LabelSelector: labelSelector}
	err = r.Client.List(ctx, serviceList, listOps)
	if err != nil {
		reqLogger.Error(err, "failed to select cluster service")
		return reconcile.Result{}, err
	}

	statusUpdate := false
	if instance.Status.ServiceName != serviceList.Items[0].Name {
		instance.Status.ServiceName = serviceList.Items[0].Name
		statusUpdate = true
	}
	statusUpdate = instance.SetCondition("Ready", metav1.ConditionTrue, "") || statusUpdate
	if statusUpdate {
		reqLogger.Info("Update CR status with connection info")
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to update Cache %s status", instance.Name))
			return reconcile.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}
