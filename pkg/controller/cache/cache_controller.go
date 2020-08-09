package cache

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	infinispanv2alpha1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/cache"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	"github.com/infinispan/infinispan-operator/version"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileCache) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info(fmt.Sprintf("+++++ Reconciling Cache. Operator Version: %s", version.Version))
	defer reqLogger.Info("----- End Reconciling Cache.")

	// Fetch the Cache instance
	instance := &infinispanv2alpha1.Cache{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
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

	// Reconcile cache
	reqLogger.Info("Identify the target cluster")
	// Fetch the Infinispan cluster info
	if instance.Spec.ClusterName == "" {
		// Create dedicated Infinispan CR
		ispnInstance := &infinispanv1.Infinispan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instance.Name,
				Namespace: instance.Namespace,
			},
			Spec: infinispanv1.InfinispanSpec{Replicas: 1,
				Expose: &infinispanv1.ExposeSpec{Type: infinispanv1.ExposeTypeRoute}},
		}
		err := r.client.Create(context.TODO(), ispnInstance)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Error on creating Infinispan cluster %s", ispnInstance.Name))
			return reconcile.Result{RequeueAfter: constants.DefaultWaitOnClusterForCache}, err
		}
		instance.Spec.ClusterName = instance.Name
		err = r.client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to update Cache: %s", instance.Name))
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	if instance.Spec.AdminAuth.SecretName == "" {
		// Create auth secret from Infinispan CR
		ispnInstance := &infinispanv1.Infinispan{}
		nsName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.ClusterName}
		err = r.client.Get(context.TODO(), nsName, ispnInstance)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Error(err, fmt.Sprintf("Infinispan cluster %s not found", ispnInstance.Name))
				return reconcile.Result{RequeueAfter: constants.DefaultWaitOnClusterForCache}, err
			}
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}

		pass, err := cluster.GetPassword("operator", ispnInstance.GetSecretName(), ispnInstance.Namespace)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Authentication secret %s not found", instance.Name))
			return reconcile.Result{}, err
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      instance.Name + "-secret",
				Namespace: instance.Namespace,
			},
			Type: corev1.SecretType("Opaque"),
			StringData: map[string]string{"username": "operator",
				"password": pass},
		}
		err = r.client.Create(context.TODO(), secret)
		if err != nil && !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, fmt.Sprintf("Error on creating cache secret %s", instance.Name))
			return reconcile.Result{}, err
		}
		instance.Spec.AdminAuth = infinispanv2alpha1.AdminAuth{SecretName: secret.Name,
			Username: corev1.SecretKeySelector{Key: "username"},
			Password: corev1.SecretKeySelector{Key: "password"}}
		err = r.client.Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to update Cache: %s", instance.Name))
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	ispnInstance := &infinispanv1.Infinispan{}
	nsName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.ClusterName}
	err = r.client.Get(context.TODO(), nsName, ispnInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Infinispan cluster %s not found", ispnInstance.Name))
			return reconcile.Result{RequeueAfter: constants.DefaultWaitOnClusterForCache}, err
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Cluster must be well formed
	if !ispnInstance.IsWellFormed() {
		reqLogger.Info(fmt.Sprintf("Infinispan cluster %s not well formed", ispnInstance.Name))
		return reconcile.Result{RequeueAfter: constants.DefaultWaitOnClusterForCache}, err
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
		reqLogger.Error(err, "failed to list pods")
		return reconcile.Result{}, err
	} else if len(podList.Items) == 0 {
		reqLogger.Error(err, "No Infinispan pods found")
		return reconcile.Result{}, nil
	}

	existsCache, err := cluster.ExistsCache(user, pass, instance.GetCacheName(), podList.Items[0].Name, instance.Namespace, string(ispnInstance.GetEndpointScheme()))
	if err == nil {
		if existsCache {
			reqLogger.Info(fmt.Sprintf("Cache %s already exists", instance.GetCacheName()))
			// Check if template matches?
		} else {
			reqLogger.Info(fmt.Sprintf("Cache %s doesn't exist, create it", instance.GetCacheName()))
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
					xmlTemplate, err = cache.GetDefaultCacheTemplateXML(podName, ispnInstance, cluster, reqLogger)
				}
				if err != nil {
					reqLogger.Error(err, "Error getting default XML")
					return reconcile.Result{}, err
				}
				reqLogger.Info(xmlTemplate)
				err = cluster.CreateCacheWithTemplate(user, pass, instance.GetCacheName(), xmlTemplate, podName, instance.Namespace, string(ispnInstance.GetEndpointScheme()))
				if err != nil {
					reqLogger.Error(err, "Error in creating cache")
					return reconcile.Result{}, err
				}
			}
		}

		if !metav1.IsControlledBy(instance, ispnInstance) {
			reqLogger.Info("Update CR with owner reference info")
			controllerutil.SetControllerReference(ispnInstance, instance, r.scheme)
			err = r.client.Update(context.TODO(), instance)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Unable to update Cache: %s", instance.Name))
				return reconcile.Result{}, err
			}
		}
	} else {
		reqLogger.Error(err, "Error validating cache exist")
		return reconcile.Result{}, err
	}

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
	statusUpdate = instance.SetCondition("Ready", metav1.ConditionTrue, "") || statusUpdate
	if statusUpdate {
		reqLogger.Info("Update CR status with connection info")
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to update Cache %s status", instance.Name))
			return reconcile.Result{}, err
		}
	}
	r.reconcileQuickstart(instance, ispnInstance, reqLogger)
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
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name}, userSecret)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Secret %s not found", name))
			return nil, reconcile.Result{RequeueAfter: constants.DefaultWaitOnCreateSecret}, err
		}
	}
	// Error reading the object - requeue the request.
	return userSecret, reconcile.Result{}, err
}

func (r *ReconcileCache) reconcileQuickstart(cache *infinispanv2alpha1.Cache, ispn *infinispanv1.Infinispan, logger logr.Logger) (reconcile.Request, error) {
	confMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: cache.Namespace, Name: cache.Name}}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: confMap.Namespace, Name: confMap.Name}, confMap)
	if err != nil {
		if errors.IsNotFound(err) {
			restInfo, err := r.getRestInfo(cache, ispn, logger)
			if err != nil {
				return reconcile.Request{}, err
			}
			confMap.Data = map[string]string{"restInfo": restInfo}
			fmt.Printf(restInfo)
			err = r.client.Create(context.TODO(), confMap)
			return reconcile.Request{}, err
		}
		logger.Error(err, fmt.Sprintf("Error in finding ConfigMap %s", cache.Name))
		return reconcile.Request{}, err
	}
	return reconcile.Request{}, nil
}

// getRestInfo needs
// 1. namespace
// 2. secret name
// 3. scheme
// 4. url
// 5. cachename
func (r *ReconcileCache) getRestInfo(cache *infinispanv2alpha1.Cache, ispn *infinispanv1.Infinispan, logger logr.Logger) (string, error) {
	infoTemplate := `
You should know you username:password credential, if not you can list
cluster credential running:
kubectl -n %s  get secret %s -o jsonpath="{.data.identities\.yaml}" | base64 --decode | yq -r .credentials
Now set:
export USER="your user"
export PASS="your password"
Access your cache like this:
1. Get cache info
curl -v -u $USER:$PASS %s %s://%s/rest/v2/caches/%s
2. Put entry
curl -v -u $USER:$PASS %s -H 'Content-type: text/plain' -d 'test-value' %s://%s/rest/v2/caches/%s/test-key
3. Get entry
curl -v -u $USER:$PASS %s %s://%s/rest/v2/caches/%s/test-key
`
	ns := cache.Namespace
	secretName := ispn.GetSecretName()
	scheme := ispn.GetEndpointScheme()
	unsafe := ""
	if scheme == corev1.URISchemeHTTPS {
		unsafe = "-k"
	}
	cacheName := cache.GetCacheName()
	url := ""
	ok, _ := common.IsGroupVersionSupported(routev1.GroupVersion.String(), "Route", kubernetes.RestConfig, logger)
	if ok {
		// Route is supported
		route := &routev1.Route{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetServiceExternalName()}, route)
		if err != nil {
			logger.Error(err, fmt.Sprintf("Cannot find Route %s", ispn.GetServiceExternalName()))
			return "", err
		}
		if len(route.Status.Ingress) > 0 {
			url = route.Status.Ingress[0].Host
		} else {
			logger.Error(err, fmt.Sprintf("Cannot find url for Ingress %s", ispn.GetServiceExternalName()))
			return "", err
		}
	} else {
		// Ingress is supported
		ingress := &networkingv1beta1.Ingress{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetServiceExternalName()}, ingress)
		if err != nil {
			logger.Error(err, fmt.Sprintf("Cannot find Ingress %s", ispn.GetServiceExternalName()))
			return "", err
		}
		if len(ingress.Status.LoadBalancer.Ingress) > 0 {
			if ingress.Status.LoadBalancer.Ingress[0].Hostname != "" {
				url = ingress.Status.LoadBalancer.Ingress[0].Hostname
			} else {
				url = ingress.Status.LoadBalancer.Ingress[0].IP
			}
		}
		if url == "" {
			logger.Error(err, fmt.Sprintf("Cannot find url for Ingress %s", ispn.GetServiceExternalName()))
			return "", err
		}
	}
	return fmt.Sprintf(infoTemplate, ns, secretName, unsafe, scheme, url, cacheName, unsafe, scheme, url, cacheName, unsafe, scheme, url, cacheName), nil
}
