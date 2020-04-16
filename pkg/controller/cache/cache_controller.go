package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	infinispanv2alpha1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_cache")

// Kubernetes object
var kubernetes *ispnutil.Kubernetes

// Cluster object
var cluster ispnutil.ClusterInterface

// Add creates a new Cache Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	kubernetes = ispnutil.NewKubernetesFromController(mgr)
	cluster = ispnutil.NewCluster(kubernetes)
	return &ReconcileCache{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("cache-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Cache
	err = c.Watch(&source.Kind{Type: &infinispanv2alpha1.Cache{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Cache
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &infinispanv2alpha1.Cache{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileCache implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileCache{}

// ReconcileCache reconciles a Cache object
type ReconcileCache struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Cache object and makes changes based on the state read
// and what is in the Cache.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileCache) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Cache")

	// Fetch the Cache instance
	instance := &infinispanv2alpha1.Cache{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Reconcile cache
	reqLogger.Info("Identify the target cluster")
	// Fetch the Infinispan cluster info
	ispnInstance := &infinispanv1.Infinispan{}
	nsName := types.NamespacedName{Name: instance.Spec.ClusterName, Namespace: instance.Namespace}
	err = r.client.Get(context.TODO(), nsName, ispnInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, "Infinispan cluster "+ispnInstance.Name+" not found")
			return reconcile.Result{RequeueAfter: time.Second * 10}, err
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Cluster must be well formed
	if !ispnInstance.IsWellFormed() {
		reqLogger.Info("Infinispan cluster " + ispnInstance.Name + " not well formed")
		return reconcile.Result{RequeueAfter: time.Second * 10}, err
	}
	// Authentication is required to go on from here
	user, pass, result, err := getCredentials(r, reqLogger, instance.CopyWithDefaultsForEmptyVals())
	if err != nil {
		return result, err
	}
	// List the pods for this infinispan's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(common.LabelsResource(ispnInstance.Name, ""))
	listOps := &client.ListOptions{Namespace: ispnInstance.GetClusterName(), LabelSelector: labelSelector}
	err = r.client.List(context.TODO(), podList, listOps)
	if err != nil || (len(podList.Items) == 0) {
		reqLogger.Error(err, "failed to list pods", "Infinispan.Namespace")
		return reconcile.Result{}, err
	}
	cacheName := instance.Name
	if instance.Spec.Name != "" {
		cacheName = instance.Spec.Name
	}
	existsCache, err := cluster.ExistsCacheWithAuth(user, pass, cacheName, podList.Items[0].Name, instance.Namespace, string(ispnInstance.GetEndpointScheme()))
	if err == nil {
		if existsCache {
			reqLogger.Info("Cache already exists")
			// Check if template matches?
		} else {
			reqLogger.Info("Cache doesn't exist, create it")
			podName := podList.Items[0].Name
			templateName := instance.Spec.TemplateName
			if ispnInstance.Spec.Service.Type == infinispanv1.ServiceTypeCache && (templateName != "" || instance.Spec.Template != "") {
				errTemplate := fmt.Errorf("Cannot create a cache with a template in a CacheService cluster")
				reqLogger.Error(errTemplate, "Error creating cache")
				return reconcile.Result{}, err
			}
			if templateName != "" {
				err = cluster.CreateCacheWithTemplateName(user, pass, instance.Spec.Name, templateName, podName, instance.Namespace, string(ispnInstance.GetEndpointScheme()))
				if err != nil {
					reqLogger.Error(err, "Error creating cache with template name")
					return reconcile.Result{}, err
				}
			} else {
				xmlTemplate := instance.Spec.Template
				if xmlTemplate == "" {
					xmlTemplate, err = ispnctrl.GetDefaultCacheTemplateXML(podName, ispnInstance, cluster, reqLogger)
				}
				if err != nil {
					reqLogger.Error(err, "Error getting default XML")
					return reconcile.Result{}, err
				}
				reqLogger.Info(xmlTemplate)
				err = cluster.CreateCacheWithAuth(user, pass, instance.Spec.Name, xmlTemplate, podName, instance.Namespace, string(ispnInstance.GetEndpointScheme()))
				if err != nil {
					reqLogger.Error(err, "Error in creating cache")
					return reconcile.Result{}, err
				}
			}
		}
	} else {
		reqLogger.Error(err, "Error getting caches list")
		return reconcile.Result{}, err
	}

	reqLogger.Info("Update CR status with connection info")

	// Search the service associated to the cluster
	serviceList := &corev1.ServiceList{}
	labelSelector = labels.SelectorFromSet(common.LabelsResource(ispnInstance.Name, "infinispan-service"))
	listOps = &client.ListOptions{Namespace: ispnInstance.Namespace, LabelSelector: labelSelector}
	err = r.client.List(context.TODO(), serviceList, listOps)
	if err != nil {
		reqLogger.Error(err, "failed to select cluster service")
		return reconcile.Result{}, err
	}

	statusUpdate := false
	if instance.Status.ServiceName != serviceList.Items[0].Name {
		instance.Status.ServiceName = serviceList.Items[0].Name
		statusUpdate = true
	}
	statusUpdate = statusUpdate || instance.SetCondition("Ready", "True", "")
	if statusUpdate {
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "Unable to update Cache "+instance.Name+" status")
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func getCredentials(r *ReconcileCache, reqLogger logr.Logger, instance *infinispanv2alpha1.Cache) (string, string, reconcile.Result, error) {
	userSecret, result, err := getSecret(r, reqLogger, instance.Spec.AdminAuth.Username.Name, instance.Namespace)
	if err != nil {
		return "", "", result, err
	}
	passSecret := userSecret
	if instance.Spec.AdminAuth.Username.Name != instance.Spec.AdminAuth.Password.Name {
		passSecret, result, err = getSecret(r, reqLogger, instance.Spec.AdminAuth.Username.Name, instance.Namespace)
		if err != nil {
			return "", "", result, err
		}
	}
	user := string(userSecret.Data[instance.Spec.AdminAuth.Username.Key])
	pass := string(passSecret.Data[instance.Spec.AdminAuth.Password.Key])
	return user, pass, reconcile.Result{}, nil
}

func getSecret(r *ReconcileCache, reqLogger logr.Logger, name, ns string) (*corev1.Secret, reconcile.Result, error) {
	userSecret := &corev1.Secret{}
	userNsName := types.NamespacedName{Name: name, Namespace: ns}
	err := r.client.Get(context.TODO(), userNsName, userSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, "Secret "+name+" not found")
			return nil, reconcile.Result{RequeueAfter: time.Second * 2}, err
		}
		// Error reading the object - requeue the request.
	}
	return userSecret, reconcile.Result{}, err
}
