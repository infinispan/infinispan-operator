package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/manage"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/go-logr/logr"
	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// CacheReconciler reconciles a Cache object
type CacheReconciler struct {
	client.Client
	log            logr.Logger
	scheme         *runtime.Scheme
	kubernetes     *kube.Kubernetes
	eventRec       record.EventRecorder
	versionManager *version.Manager
}

type CacheListener struct {
	// The Infinispan cluster to listen to in the configured namespace
	Infinispan     *v1.Infinispan
	Ctx            context.Context
	Kubernetes     *kube.Kubernetes
	Log            *zap.SugaredLogger
	VersionManager *version.Manager
}

type cacheRequest struct {
	*CacheReconciler
	ctx        context.Context
	cache      *v2alpha1.Cache
	infinispan *v1.Infinispan
	ispnClient api.Infinispan
	reqLogger  logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *CacheReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) (err error) {
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName("Cache")
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor("cache-controller")

	r.versionManager, err = version.ManagerFromEnv(v1.OperatorOperandVersionEnvVarName)
	if err != nil {
		return
	}

	if err = mgr.GetFieldIndexer().IndexField(ctx, &v2alpha1.Cache{}, "spec.clusterName", func(obj client.Object) []string {
		return []string{obj.(*v2alpha1.Cache).Spec.ClusterName}
	}); err != nil {
		return
	}

	builder := ctrl.NewControllerManagedBy(mgr).For(&v2alpha1.Cache{})
	builder.Watches(
		&source.Kind{Type: &v1.Infinispan{}},
		handler.EnqueueRequestsFromMapFunc(
			func(a client.Object) []reconcile.Request {
				i := a.(*v1.Infinispan)
				// Only enqueue requests once a Infinispan CR has the WellFormed condition or it has been deleted
				if !i.HasCondition(v1.ConditionWellFormed) || !a.GetDeletionTimestamp().IsZero() {
					return nil
				}

				var requests []reconcile.Request
				cacheList := &v2alpha1.CacheList{}
				if err := r.kubernetes.ResourcesListByField(a.GetNamespace(), "spec.clusterName", a.GetName(), cacheList, ctx); err != nil {
					r.log.Error(err, "watches failed to list Cache CRs")
				}

				for _, item := range cacheList.Items {
					requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: item.GetNamespace(), Name: item.GetName()}})
				}
				return requests
			}),
	)
	return builder.Complete(r)
}

// +kubebuilder:rbac:groups=infinispan.org,namespace=infinispan-operator-system,resources=caches;caches/status;caches/finalizers,verbs=get;list;watch;create;update;patch;delete

func (r *CacheReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("+++++ Reconciling Cache.")
	defer reqLogger.Info("----- End Reconciling Cache.")

	// Fetch the Cache instance
	instance := &v2alpha1.Cache{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Cache resource not found. Ignoring it since cache deletion is not supported")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	infinispan := &v1.Infinispan{}
	cache := &cacheRequest{
		CacheReconciler: r,
		ctx:             ctx,
		cache:           instance,
		infinispan:      infinispan,
		reqLogger:       reqLogger,
	}

	if cache.markedForDeletion() {
		reqLogger.Info("Cache CR marked for deletion. Attempting to remove.")
		// The ConfigListener has marked this resource for deletion
		// Remove finalizer and delete CR. No need to update the server as the cache has already been removed
		if err := cache.removeFinalizer(); err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Unable to remove Finalizer as Cache CR not found.")
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		if err := cache.kubernetes.Client.Delete(ctx, instance); err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Cache CR does not exist, nothing todo.")
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		reqLogger.Info("Cache CR Removed.")
		return ctrl.Result{}, nil
	}

	crDeleted := instance.GetDeletionTimestamp() != nil

	// Add the "retain" update strategy for Cache CRs created with an older Operator version
	if !crDeleted && (instance.Spec.Updates == nil || instance.Spec.Updates.Strategy == "") {
		reqLogger.Info("Cache CR created with older operator version, adding update strategy to maintain previous controller behaviour", "strategy", v2alpha1.CacheUpdateRetain)
		return ctrl.Result{Requeue: true}, cache.update(func() error {
			instance.Spec.Updates = &v2alpha1.CacheUpdateSpec{
				Strategy: v2alpha1.CacheUpdateRetain,
			}
			return nil
		})
	}

	// Fetch the Infinispan cluster
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.ClusterName}, infinispan); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Infinispan cluster %s not found", instance.Spec.ClusterName))
			if crDeleted {
				return ctrl.Result{}, cache.removeFinalizer()
			}
			// No need to requeue request here as the Infinispan watch ensures that a request is queued when the cluster is updated
			return ctrl.Result{}, cache.update(func() error {
				// Set CacheConditionReady to false in case the cluster was previously WellFormed
				instance.SetCondition(v2alpha1.CacheConditionReady, metav1.ConditionFalse, "")
				return nil
			})
		}
		return ctrl.Result{}, err
	}

	// Cluster must be well formed
	if !infinispan.IsWellFormed() {
		reqLogger.Info(fmt.Sprintf("Infinispan cluster %s not well formed", infinispan.Name))
		// No need to requeue request here as the Infinispan watch ensures that a request is queued when the cluster is updated
		return ctrl.Result{}, nil
	}

	ispnClient, err := NewInfinispan(ctx, infinispan, r.versionManager, r.kubernetes)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to create Infinispan client: %w", err)
	}
	cache.ispnClient = ispnClient

	if crDeleted {
		if controllerutil.ContainsFinalizer(instance, constants.InfinispanFinalizer) {
			// Remove Deleted caches from the server before removing the Finalizer
			cacheName := instance.GetCacheName()
			if err := ispnClient.Cache(cacheName).Delete(); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, cache.removeFinalizer()
		}
		return ctrl.Result{}, nil
	}

	// Don't contact the Infinispan server for resources created by the ConfigListener
	if cache.reconcileOnServer() {
		if result, err := cache.ispnCreateOrUpdate(); result != nil {
			if err != nil {
				return *result, cache.update(func() error {
					instance.SetCondition(v2alpha1.CacheConditionReady, metav1.ConditionFalse, err.Error())
					return nil
				})
			}
			return *result, err
		}
	}

	err = cache.update(func() error {
		instance.SetCondition(v2alpha1.CacheConditionReady, metav1.ConditionTrue, "")
		// Add finalizer so that the Cache is removed on the server when the Cache CR is deleted
		if !controllerutil.ContainsFinalizer(instance, constants.InfinispanFinalizer) {
			controllerutil.AddFinalizer(instance, constants.InfinispanFinalizer)
		}
		return nil
	})
	return ctrl.Result{}, err
}

func (r *cacheRequest) update(mutate func() error) error {
	return updateCache(r.cache, r.ctx, r.Client, mutate)
}

// Determine if reconciliation was triggered by the ConfigListener
func (r *cacheRequest) reconcileOnServer() bool {
	if val, exists := r.cache.ObjectMeta.Annotations[constants.ListenerAnnotationGeneration]; exists {
		generation, _ := strconv.ParseInt(val, 10, 64)
		return generation < r.cache.GetGeneration()
	}
	return true
}

func (r *cacheRequest) markedForDeletion() bool {
	_, exists := r.cache.ObjectMeta.Annotations[constants.ListenerAnnotationDelete]
	return exists
}

func (r *cacheRequest) removeFinalizer() error {
	if controllerutil.ContainsFinalizer(r.cache, constants.InfinispanFinalizer) {
		return r.update(func() error {
			controllerutil.RemoveFinalizer(r.cache, constants.InfinispanFinalizer)
			return nil
		})
	}
	return nil
}

func (r *cacheRequest) ispnCreateOrUpdate() (*ctrl.Result, error) {
	cacheName := r.cache.GetCacheName()
	cacheClient := r.ispnClient.Cache(cacheName)

	cacheExists, err := cacheClient.Exists()
	if err != nil {
		err := fmt.Errorf("unable to determine if cache exists: %w", err)
		r.reqLogger.Error(err, "")
		return &ctrl.Result{}, err
	}

	if r.infinispan.IsDataGrid() {
		err = r.reconcileDataGrid(cacheExists, cacheClient)
	} else {
		err = r.reconcileCacheService(cacheExists, cacheClient)
	}
	if err != nil {
		return &ctrl.Result{Requeue: true}, err
	}
	return nil, nil
}

func (r *cacheRequest) reconcileCacheService(cacheExists bool, cache api.Cache) error {
	spec := r.cache.Spec
	if spec.TemplateName != "" || spec.Template != "" {
		err := fmt.Errorf("cannot create a cache with a template in a CacheService cluster")
		r.reqLogger.Error(err, "Error creating cache")
		return err
	}

	if cacheExists {
		r.reqLogger.Info("cache already exists")
		return nil
	}

	podList, err := PodList(r.infinispan, r.kubernetes, r.ctx)
	if err != nil {
		r.reqLogger.Error(err, "failed to list pods")
		return err
	}

	template, err := manage.DefaultCacheTemplateXML(podList.Items[0].Name, r.infinispan, r.kubernetes, r.reqLogger)
	if err != nil {
		err = fmt.Errorf("unable to obtain default cache template: %w", err)
		r.reqLogger.Error(err, "Error getting default XML")
		return err
	}
	if err = cache.Create(template, mime.ApplicationXml); err != nil {
		err = fmt.Errorf("unable to create cache using default template: %w", err)
		r.reqLogger.Error(err, "Error in creating cache")
		return err
	}
	return nil
}

func (r *cacheRequest) reconcileDataGrid(cacheExists bool, cache api.Cache) error {
	spec := r.cache.Spec

	if spec.Template == "" && spec.TemplateName == "" {
		return fmt.Errorf("unable to reconcile Cache as both 'spec.template' and 'spec.templateName' are undefined")
	}

	if cacheExists {
		if spec.Template == "" {
			return nil
		}

		serverConfig, err := cache.Config(mime.ApplicationJson)
		if err != nil {
			return fmt.Errorf("unable to retrieve existing cache configuration: %w", err)
		}

		configUpdated, err := configChanged(serverConfig, spec.Template, r.infinispan, r.ispnClient.Caches(), r.versionManager)
		if err != nil {
			return fmt.Errorf("unable to determine if configuration has changed: %w", err)
		}
		if !configUpdated {
			r.log.Info("configuration has not changed, ignoring update")
			return nil
		}

		if spec.Updates.Strategy == v2alpha1.CacheUpdateRetain {
			// Only update the cache if possible at runtime, otherwise set Ready=false.
			if err := cache.UpdateConfig(spec.Template, mime.GuessMarkup(spec.Template)); err != nil {
				return fmt.Errorf("unable to update cache template at runtime: %w", err)
			}
			return nil
		}

		// Recreate strategy
		// Update the cache configuration at runtime if possible, retaining data, otherwise delete
		if err := cache.UpdateConfig(spec.Template, mime.GuessMarkup(spec.Template)); err != nil {
			r.log.Info("unable to update cache template at runtime, recreating", "error", err)

			// Add an annotation to indicate to the ConfigListener that a remote-cache event should be expected for this CR
			// Required in order to prevent the Cache CR that's being reconciled from being
			err := r.update(func() error {
				if r.cache.ObjectMeta.Annotations == nil {
					r.cache.ObjectMeta.Annotations = make(map[string]string, 1)
				}
				r.cache.ObjectMeta.Annotations[constants.ListenerControllerDelete] = "true"
				return nil
			})
			if err != nil {
				return fmt.Errorf("unable to add annotation on update '%s': %w", constants.ListenerControllerDelete, err)
			}

			cacheName := r.cache.GetCacheName()
			if err := cache.Delete(); err != nil {
				return fmt.Errorf("unable to delete existing cache '%s': %w", cacheName, err)
			}

			if err := cache.Create(spec.Template, mime.GuessMarkup(spec.Template)); err != nil {
				return fmt.Errorf("unable to create cache '%s': %w", cacheName, err)
			}
		}
		return nil
	}

	var err error
	if spec.TemplateName != "" {
		if err = cache.CreateWithTemplate(spec.TemplateName); err != nil {
			err = fmt.Errorf("unable to create cache with template name '%s': %w", spec.TemplateName, err)
		}
	} else {
		if err = cache.Create(spec.Template, mime.GuessMarkup(spec.Template)); err != nil {
			err = fmt.Errorf("unable to create cache with template: %w", err)
		}
	}

	if err != nil {
		r.reqLogger.Error(err, "Unable to create Cache")
	}
	return err
}

func (cl *CacheListener) RemoveStaleResources(podName string) error {
	cl.Log.Info("Checking for stale cache resources")
	k8s := cl.Kubernetes
	ispn, err := NewInfinispanForPod(cl.Ctx, podName, cl.Infinispan, cl.VersionManager, k8s)
	if err != nil {
		return err
	}

	// Retrieve names of caches defined on the server
	cacheNames, err := ispn.Caches().Names()
	if err != nil {
		return err
	}
	cl.Log.Debugf("Caches defined on the server: '%v'", cacheNames)

	// Create Set of CR names for 0(1) lookup
	serverCaches := make(map[string]struct{}, len(cacheNames))
	for _, name := range cacheNames {
		cacheCrName := strcase.ToKebab(name)
		serverCaches[cacheCrName] = struct{}{}
	}

	// Retrieve list of all Cache CRs in namespace
	cacheList := &v2alpha1.CacheList{}
	if err := k8s.Client.List(cl.Ctx, cacheList, &client.ListOptions{Namespace: cl.Infinispan.Namespace}); err != nil {
		return fmt.Errorf("unable to rerieve existing Cache resources: %w", err)
	}

	// Iterate over all existing CRs, marking for deletion any that do not have a cache definition on the server
	for _, cache := range cacheList.Items {
		listenerCreated := kube.IsOwnedBy(&cache, cl.Infinispan)
		_, cacheExists := serverCaches[cache.GetCacheName()]
		cl.Log.Debugf("Checking if Cache CR '%s' is stale. ListenerCreated=%t. CacheExists=%t", cache.Name, listenerCreated, cacheExists)
		if listenerCreated && !cacheExists {
			cache.ObjectMeta.Annotations[constants.ListenerAnnotationDelete] = "true"
			cl.Log.Infof("Marking stale Cache resource '%s' for deletion", cache.Name)
			if err := k8s.Client.Update(cl.Ctx, &cache); err != nil {
				if !errors.IsNotFound(err) {
					return fmt.Errorf("unable to mark Cache '%s' for deletion: %w", cache.Name, err)
				}
			}
		}
	}
	return nil
}

var cacheNameRegexp = regexp.MustCompile("[^-a-z0-9]")

func (cl *CacheListener) CreateOrUpdate(data []byte) error {
	namespace := cl.Infinispan.Namespace
	clusterName := cl.Infinispan.Name
	cacheName, configJson, err := unmarshallEventConfig(data)
	if err != nil {
		return err
	}

	if strings.HasPrefix(cacheName, "___") {
		cl.Log.Debugf("Ignoring internal cache %s", cacheName)
		return nil
	}

	cache, err := cl.findExistingCacheCR(cacheName, clusterName)
	if err != nil {
		return err
	}

	k8sClient := cl.Kubernetes.Client
	ispnClient, err := NewInfinispan(cl.Ctx, cl.Infinispan, cl.VersionManager, cl.Kubernetes)
	if err != nil {
		return fmt.Errorf("unable to create Infinispan client: %w", err)
	}

	if cache == nil {
		// There's no Existing Cache CR, so we must create one
		sanitizedCacheName := cacheNameRegexp.ReplaceAllString(strcase.ToKebab(cacheName), "-")
		errs := validation.IsDNS1123Subdomain(sanitizedCacheName)
		if len(errs) > 0 {
			return fmt.Errorf("unable to create Cache Resource Name for cache=%s, cluster=%s: %s", cacheName, clusterName, strings.Join(errs, "."))
		}

		cache = &v2alpha1.Cache{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: sanitizedCacheName + "-",
				Namespace:    namespace,
				Annotations: map[string]string{
					constants.ListenerAnnotationGeneration: "1",
				},
			},
			Spec: v2alpha1.CacheSpec{
				ClusterName: cl.Infinispan.Name,
				Name:        cacheName,
			},
		}

		// Convert JSON to yaml
		configYaml, err := ispnClient.Caches().ConvertConfiguration(configJson, mime.ApplicationJson, mime.ApplicationYaml)
		if err != nil {
			return fmt.Errorf("unable to convert cache configuration from '%s' to '%s': %w", mime.ApplicationJson, mime.ApplicationYaml, err)
		}
		if err != nil {
			return err
		}
		cache.Spec.Template = configYaml

		controllerutil.AddFinalizer(cache, constants.InfinispanFinalizer)
		if err := controllerutil.SetOwnerReference(cl.Infinispan, cache, k8sClient.Scheme()); err != nil {
			return err
		}

		cl.Log.Infof("Creating Cache CR for '%s'\n%s", cacheName, configYaml)
		if err := k8sClient.Create(cl.Ctx, cache); err != nil {
			return fmt.Errorf("unable to create Cache CR for cache '%s': %w", cacheName, err)
		}
		cl.Log.Infof("Cache CR '%s' created", cache.Name)
	} else {
		// Update existing Cache
		maxRetries := 5
		for i := 1; i <= maxRetries; i++ {

			if cache.Spec.Template == "" {
				// The cache uses a template defined on the server so nothing to do
				break
			}

			configUpdated, err := configChanged(configJson, cache.Spec.Template, cl.Infinispan, ispnClient.Caches(), cl.VersionManager)
			if !configUpdated {
				cl.Log.Debugf("Cache '%s' configuration on update has not changed, ignoring update", cache.Name)
				break
			}

			_, err = controllerutil.CreateOrPatch(cl.Ctx, k8sClient, cache, func() error {
				if cache.CreationTimestamp.IsZero() {
					return errors.NewNotFound(schema.ParseGroupResource("caches.infinispan.org"), cache.Name)
				}
				var template, templateName string
				if cache.Spec.Template != "" {
					mediaType := mime.GuessMarkup(cache.Spec.Template)
					cl.Log.Infof("Update Cache CR for '%s' with MediaType %s\n%s", cache.Name, mediaType, configJson)
					// Determinate the original user markup format and convert stream configuration to that format if required
					if mediaType == mime.ApplicationJson {
						template = configJson
					} else {
						template, err = ispnClient.Caches().ConvertConfiguration(configJson, mime.ApplicationJson, mime.ApplicationYaml)
						if err != nil {
							return fmt.Errorf("unable to convert cache configuration from '%s' to '%s': %w", mime.ApplicationJson, mime.ApplicationYaml, err)
						}
					}
				} else {
					templateName = cache.Spec.TemplateName
				}

				if cache.ObjectMeta.Annotations == nil {
					cache.ObjectMeta.Annotations = make(map[string]string, 1)
				}
				controllerutil.AddFinalizer(cache, constants.InfinispanFinalizer)
				cache.ObjectMeta.Annotations[constants.ListenerAnnotationGeneration] = strconv.FormatInt(cache.GetGeneration()+1, 10)
				cache.Spec = v2alpha1.CacheSpec{
					Name:         cacheName,
					ClusterName:  cl.Infinispan.Name,
					Template:     template,
					TemplateName: templateName,
					Updates:      cache.Spec.Updates,
				}
				return nil
			})
			if err == nil {
				break
			}

			if !errors.IsConflict(err) {
				return fmt.Errorf("unable to Update Cache CR '%s': %w", cache.Name, err)
			}
			cl.Log.Errorf("Conflict encountered on Cache CR '%s' update. Retry %d..%d", cache.Name, i, maxRetries)
		}
		if err != nil {
			return fmt.Errorf("unable to Update Cache CR %s after %d attempts", cache.Name, maxRetries)
		}
	}
	return nil
}

func (cl *CacheListener) findExistingCacheCR(cacheName, clusterName string) (*v2alpha1.Cache, error) {
	cacheList := &v2alpha1.CacheList{}
	listOpts := &client.ListOptions{
		Namespace: cl.Infinispan.Namespace,
	}
	if err := cl.Kubernetes.Client.List(cl.Ctx, cacheList, listOpts); err != nil {
		return nil, fmt.Errorf("unable to list existing Cache CRs: %w", err)
	}

	var caches []v2alpha1.Cache
	for _, c := range cacheList.Items {
		if c.GetCacheName() == cacheName && c.Spec.ClusterName == clusterName {
			caches = append(caches, c)
		}
	}
	switch len(caches) {
	case 0:
		cl.Log.Debugf("No existing Cache CR found for Cache=%s, Cluster=%s", cacheName, clusterName)
		return nil, nil
	case 1:
		cl.Log.Debugf("An existing Cache CR '%s' was found for Cache=%s, Cluster=%s", caches[0].Name, cacheName, clusterName)
		return &caches[0], nil
	default:
		// Multiple existing Cache CRs found. Should never happen
		y, _ := json.Marshal(caches)
		return nil, fmt.Errorf("More than one Cache CR found for Cache=%s, Cluster=%s:\n%s", cacheName, clusterName, string(y))
	}
}

func (cl *CacheListener) Delete(data []byte) error {
	cacheName := string(data)
	cl.Log.Infof("Processing remove event for cache '%s'", cacheName)

	existingCacheCr, err := cl.findExistingCacheCR(cacheName, cl.Infinispan.Name)
	if existingCacheCr == nil || err != nil {
		return err
	}

	if existingCacheCr.ObjectMeta.Annotations != nil {
		_, controllerDelete := existingCacheCr.ObjectMeta.Annotations[constants.ListenerControllerDelete]
		if controllerDelete {
			cl.Log.Infof("Retaining Cache CR '%s' as cache deletion was requested by the cache controller", existingCacheCr.Name)
			return updateCache(existingCacheCr, cl.Ctx, cl.Kubernetes.Client, func() error {
				delete(existingCacheCr.ObjectMeta.Annotations, constants.ListenerControllerDelete)
				return nil
			})
		}
	}

	cache := &v2alpha1.Cache{}
	existingCacheCr.DeepCopyInto(cache)

	cl.Log.Infof("Marking Cache CR '%s' for removal", cache.Name)
	_, err = controllerutil.CreateOrPatch(cl.Ctx, cl.Kubernetes.Client, cache, func() error {
		if cache.CreationTimestamp.IsZero() || !cache.DeletionTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("caches.infinispan.org"), cache.Name)
		}
		if cache.ObjectMeta.Annotations == nil {
			cache.ObjectMeta.Annotations = make(map[string]string, 1)
		}
		cache.ObjectMeta.Annotations[constants.ListenerAnnotationDelete] = "true"
		return nil
	})
	// If the CR can't be found, do nothing
	if !errors.IsNotFound(err) {
		cl.Log.Debugf("Cache CR '%s' not found, nothing todo.", cache.Name)
		return err
	}
	return nil
}

func updateCache(cache *v2alpha1.Cache, ctx context.Context, client client.Client, mutate func() error) error {
	_, err := controllerutil.CreateOrPatch(ctx, client, cache, func() error {
		if cache.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("cache.infinispan.org"), cache.Name)
		}
		return mutate()
	})
	if err != nil {
		return fmt.Errorf("unable to update cache %s: %w", cache.Name, err)
	}
	return nil
}

func unmarshallEventConfig(data []byte) (string, string, error) {
	type Config struct {
		Infinispan struct {
			CacheContainer struct {
				Caches map[string]interface{}
			} `json:"cache-container"`
		}
	}

	config := &Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return "", "", fmt.Errorf("unable to unmarshal event data: %w", err)
	}

	if len(config.Infinispan.CacheContainer.Caches) != 1 {
		return "", "", fmt.Errorf("unexpected json format: %s", data)
	}
	var cacheName string
	var cacheConfig interface{}
	// Retrieve the first (and only) entry in the map
	for cacheName, cacheConfig = range config.Infinispan.CacheContainer.Caches {
		break
	}

	configJson, err := json.Marshal(cacheConfig)
	if err != nil {
		return "", "", fmt.Errorf("unable to marshall cache configuration: %w", err)
	}
	return cacheName, string(configJson), nil
}

func configChanged(a, b string, i *v1.Infinispan, caches api.Caches, vm *version.Manager) (bool, error) {
	// If Operand is >= 14.0.7.Final we can use the cache compare endpoint to avoid unnecessary updates to the
	// Infinispan spec.template field
	operand, _ := vm.WithRef(i.Spec.Version)
	if operand.UpstreamVersion.Major >= 14 && operand.UpstreamVersion.Patch >= 7 {
		// Check if Cache configuration is semantically the same as that already present in the Cache CR
		equalConfig, err := caches.EqualConfiguration(a, b)
		if err != nil {
			return false, err
		}
		return !equalConfig, nil
	} else {
		// Hack use the same source and destination media type in order to get the server representation of a configuration
		// so that we can just compare the strings directly
		aMarkup := mime.GuessMarkup(a)
		aServerConfig, err := caches.ConvertConfiguration(a, aMarkup, mime.ApplicationJson)
		if err != nil {
			return false, err
		}

		bMarkup := mime.GuessMarkup(b)
		bServerConfig, err := caches.ConvertConfiguration(b, bMarkup, mime.ApplicationJson)
		if err != nil {
			return false, err
		}
		// Compare the two server-side representations of the provided configurations
		return aServerConfig != bServerConfig, nil
	}
}
