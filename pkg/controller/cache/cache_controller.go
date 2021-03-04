package cache

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/types"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	ispnv2alpha1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	"github.com/infinispan/infinispan-operator/pkg/controller/base"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/caches"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const ControllerName = "cache-controller"

// Kubernetes object
var kubernetes *kube.Kubernetes

// Add creates a new Cache Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	kubernetes = kube.NewKubernetesFromController(mgr)
	return &ReconcileCache{base.NewReconcilerBaseFromManager(ControllerName, mgr)}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(ControllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Cache
	err = c.Watch(&source.Kind{Type: &ispnv2alpha1.Cache{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileCache implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileCache{}

// ReconcileCache reconciles a Cache object
type ReconcileCache struct {
	*base.ReconcilerBase
}

// Reconcile reads that state of the cluster for a Cache object and makes changes based on the state read
// and what is in the Cache.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileCache) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	r.InitLogger("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.Logger().Info(fmt.Sprintf("+++++ Reconciling Cache. Operator Version: %s", version.Version))
	defer r.Logger().Info("----- End Reconciling Cache.")

	// Fetch the Cache instance
	instance := &ispnv2alpha1.Cache{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			r.Logger().Info("Cache resource not found. Ignoring it since cache deletion is not supported")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Spec.AdminAuth != nil {
		r.Logger().Info("Ignoring and removing 'spec.AdminAuth' field. The operator's admin credentials are now used to perform cache operations")
		_, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, instance, func() error {
			instance.Spec.AdminAuth = nil
			return nil
		})
		return reconcile.Result{}, err
	}

	// Reconcile cache
	r.Logger().Info("Identify the target cluster")
	// Fetch the Infinispan cluster info
	ispnInstance := &ispnv1.Infinispan{}
	nsName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.ClusterName}
	err = r.Get(context.TODO(), nsName, ispnInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Logger().Error(err, fmt.Sprintf("Infinispan cluster %s not found", ispnInstance.Name))
			return reconcile.Result{RequeueAfter: constants.DefaultWaitOnCluster}, err
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Cluster must be well formed
	if !ispnInstance.IsWellFormed() {
		r.Logger().Info(fmt.Sprintf("Infinispan cluster %s not well formed", ispnInstance.Name))
		return reconcile.Result{RequeueAfter: constants.DefaultWaitOnCluster}, err
	}
	// List the pods for this Infinispan's deployment
	podList := &corev1.PodList{}
	if err = r.ResourcesList(ispnInstance.Namespace, kube.LabelsResource(ispnInstance.Name, ""), podList); err != nil {
		r.Logger().Error(err, "failed to list pods")
		return reconcile.Result{}, err
	} else if len(podList.Items) == 0 {
		r.Logger().Error(err, "No Infinispan pods found")
		return reconcile.Result{}, nil
	}

	cluster, err := ispnctrl.NewCluster(ispnInstance, kubernetes)
	if err != nil {
		return reconcile.Result{}, err
	}
	existsCache, err := cluster.ExistsCache(instance.GetCacheName(), podList.Items[0].Name)
	if err == nil {
		if existsCache {
			r.Logger().Info(fmt.Sprintf("Cache %s already exists", instance.GetCacheName()))
			// Check if template matches?
		} else {
			r.Logger().Info(fmt.Sprintf("Cache %s doesn't exist, create it", instance.GetCacheName()))
			podName := podList.Items[0].Name
			templateName := instance.Spec.TemplateName
			if ispnInstance.Spec.Service.Type == ispnv1.ServiceTypeCache && (templateName != "" || instance.Spec.Template != "") {
				errTemplate := fmt.Errorf("Cannot create a cache with a template in a CacheService cluster")
				r.Logger().Error(errTemplate, "Error creating cache")
				return reconcile.Result{}, err
			}
			if templateName != "" {
				err = cluster.CreateCacheWithTemplateName(instance.Spec.Name, templateName, podName)
				if err != nil {
					r.Logger().Error(err, "Error creating cache with template name")
					return reconcile.Result{}, err
				}
			} else {
				xmlTemplate := instance.Spec.Template
				if xmlTemplate == "" {
					xmlTemplate, err = caches.DefaultCacheTemplateXML(podName, ispnInstance, cluster, r.Logger())
				}
				if err != nil {
					r.Logger().Error(err, "Error getting default XML")
					return reconcile.Result{}, err
				}
				r.Logger().Info(xmlTemplate)
				err = cluster.CreateCacheWithTemplate(instance.Spec.Name, xmlTemplate, podName)
				if err != nil {
					r.Logger().Error(err, "Error in creating cache")
					return reconcile.Result{}, err
				}
			}
		}
	} else {
		r.Logger().Error(err, "Error validating cache exist")
		return reconcile.Result{}, err
	}

	// Search the service associated to the cluster
	serviceList := &corev1.ServiceList{}
	if err = r.ResourcesList(ispnInstance.Namespace, kube.LabelsResource(ispnInstance.Name, "infinispan-service"), serviceList); err != nil {
		r.Logger().Error(err, "failed to select cluster service")
		return reconcile.Result{}, err
	}

	statusUpdate := false
	if instance.Status.ServiceName != serviceList.Items[0].Name {
		instance.Status.ServiceName = serviceList.Items[0].Name
		statusUpdate = true
	}
	statusUpdate = instance.SetCondition("Ready", metav1.ConditionTrue, "") || statusUpdate
	if statusUpdate {
		r.Logger().Info("Update CR status with service and condition info")
		err = r.Status().Update(context.TODO(), instance)
		if err != nil {
			r.Logger().Error(err, fmt.Sprintf("Unable to update Cache %s status", instance.Name))
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}
