package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	v2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
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
	log        logr.Logger
	scheme     *runtime.Scheme
	kubernetes *kube.Kubernetes
	eventRec   record.EventRecorder
}

type CacheListener struct {
	// The Infinispan cluster to listen to in the configured namespace
	Infinispan *v1.Infinispan
	Ctx        context.Context
	Kubernetes *kube.Kubernetes
	Log        *zap.SugaredLogger
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
func (r *CacheReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName("Cache")
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor("cache-controller")

	if err := mgr.GetFieldIndexer().IndexField(ctx, &v2alpha1.Cache{}, "spec.clusterName", func(obj client.Object) []string {
		return []string{obj.(*v2alpha1.Cache).Spec.ClusterName}
	}); err != nil {
		return err
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
		// The ConfigListener has marked this resource for deletion
		// Remove finalizer and delete CR. No need to update the server as the cache has already been removed
		if err := cache.removeFinalizer(); err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		if err := cache.kubernetes.Client.Delete(ctx, instance); err != nil && errors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	crDeleted := instance.GetDeletionTimestamp() != nil
	if !crDeleted && instance.Spec.AdminAuth != nil {
		reqLogger.Info("Ignoring and removing 'spec.AdminAuth' field. The operator's admin credentials are now used to perform cache operations")
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, instance, func() error {
			instance.Spec.AdminAuth = nil
			return nil
		})
		return ctrl.Result{}, err
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

	ispnClient, err := NewInfinispan(ctx, infinispan, r.kubernetes)
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
	cache := r.cache
	_, err := kube.CreateOrPatch(r.ctx, r.Client, cache, func() error {
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

// Determine if reconciliation was triggered by the ConfigListener
func (r *cacheRequest) reconcileOnServer() bool {
	if val, exists := r.cache.ObjectMeta.Annotations[constants.ListenerAnnotationGeneration]; exists {
		generation, _ := strconv.ParseInt(val, 10, 64)
		return generation != r.cache.GetGeneration()
	}
	return true
}

func (r *cacheRequest) markedForDeletion() bool {
	_, exists := r.cache.ObjectMeta.Annotations[constants.ListenerAnnotationDelete]
	return exists
}

func (r *cacheRequest) removeFinalizer() error {
	return r.update(func() error {
		controllerutil.RemoveFinalizer(r.cache, constants.InfinispanFinalizer)
		return nil
	})
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
	if cacheExists {
		err := fmt.Errorf("cannot update an existing cache in a CacheService cluster")
		r.reqLogger.Error(err, "Error updating cache")
		return err
	}

	if spec.TemplateName != "" || spec.Template != "" {
		err := fmt.Errorf("cannot create a cache with a template in a CacheService cluster")
		r.reqLogger.Error(err, "Error creating cache")
		return err
	}

	podList, err := PodList(r.infinispan, r.kubernetes, r.ctx)
	if err != nil {
		r.reqLogger.Error(err, "failed to list pods")
		return err
	}

	template, err := DefaultCacheTemplateXML(podList.Items[0].Name, r.infinispan, r.kubernetes, r.reqLogger)
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
	if cacheExists {
		if spec.TemplateName != "" {
			// TODO enforce with webhook validation when supported
			r.log.Error(fmt.Errorf("updating an existing Cache's 'spec.TemplateName' field is not supported"), "")
		} else {
			err := cache.UpdateConfig(spec.Template, mime.GuessMarkup(spec.Template))
			if err != nil {
				return fmt.Errorf("unable to update cache template: %w", err)
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

func (cl *CacheListener) CreateOrUpdate(data []byte) error {
	cacheName, configYaml, err := unmarshallEventConfig(data)
	if err != nil {
		return err
	}

	if strings.HasPrefix(cacheName, "___") {
		cl.Log.Debugf("Ignoring internal cache %s", cacheName)
		return nil
	}

	cacheCrName := strcase.ToKebab(cacheName)
	cache := &v2alpha1.Cache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cacheCrName,
			Namespace: cl.Infinispan.Namespace,
		},
	}

	client := cl.Kubernetes.Client
	maxRetries := 5
	for i := 1; i <= maxRetries; i++ {
		_, err = controllerutil.CreateOrPatch(cl.Ctx, client, cache, func() error {
			var template string
			if cache.CreationTimestamp.IsZero() {
				cl.Log.Infof("Create Cache CR for '%s'\n%s", cacheCrName, configYaml)
				if err := controllerutil.SetOwnerReference(cl.Infinispan, cache, client.Scheme()); err != nil {
					return err
				}
				// Define template using YAML provided by the stream when Cache is being created for the first time
				template = configYaml
			} else {
				cl.Log.Infof("Update Cache CR for '%s'\n%s", cacheCrName, configYaml)
				// Determinate the original user markup format and convert stream configuration to that format if required
				mediaType := mime.GuessMarkup(cache.Spec.Template)
				if mediaType == mime.ApplicationYaml {
					template = configYaml
				} else {
					ispnClient, err := NewInfinispan(cl.Ctx, cl.Infinispan, cl.Kubernetes)
					if err != nil {
						return fmt.Errorf("unable to create Infinispan client: %w", err)
					}
					if template, err = ispnClient.Caches().ConvertConfiguration(configYaml, mime.ApplicationYaml, mediaType); err != nil {
						return fmt.Errorf("unable to convert cache configuration from '%s' to '%s': %w", mime.ApplicationYaml, mediaType, err)
					}
				}
			}
			if cache.ObjectMeta.Annotations == nil {
				cache.ObjectMeta.Annotations = make(map[string]string, 1)
			}
			controllerutil.AddFinalizer(cache, constants.InfinispanFinalizer)
			cache.ObjectMeta.Annotations[constants.ListenerAnnotationGeneration] = strconv.FormatInt(cache.GetGeneration()+1, 10)
			cache.Spec = v2alpha1.CacheSpec{
				Name:        cacheName,
				ClusterName: cl.Infinispan.Name,
				Template:    template,
			}
			return nil
		})

		if err == nil {
			break
		}

		if !errors.IsConflict(err) {
			return fmt.Errorf("unable to CreateOrUpdate Cache CR: %w", err)
		}
		cl.Log.Errorf("Conflict encountered on Cache update. Retry %d..%d", i, maxRetries)
	}

	if err != nil {
		return fmt.Errorf("unable to CreateOrPatch Cache CR %s after %d attempts", cacheCrName, maxRetries)
	}
	return nil
}

func (cl *CacheListener) Delete(data []byte) error {
	cacheName := string(data)
	crName := strcase.ToKebab(cacheName)
	cl.Log.Infof("Remove cache %s, cr %s", cacheName, crName)

	cache := &v2alpha1.Cache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: cl.Infinispan.Namespace,
		},
	}

	_, err := kube.CreateOrPatch(cl.Ctx, cl.Kubernetes.Client, cache, func() error {
		if cache.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("caches.infinispan.org"), crName)
		}
		if cache.ObjectMeta.Annotations == nil {
			cache.ObjectMeta.Annotations = make(map[string]string, 1)
		}
		cache.ObjectMeta.Annotations[constants.ListenerAnnotationDelete] = "true"
		return nil
	})
	// If the CR can't be found, do nothing
	if !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func unmarshallEventConfig(data []byte) (string, string, error) {
	type Config struct {
		Infinispan struct {
			CacheContainer struct {
				Caches map[string]interface{}
			} `yaml:"cacheContainer"`
		}
	}

	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return "", "", fmt.Errorf("unable to unmarshal event data: %w", err)
	}

	if len(config.Infinispan.CacheContainer.Caches) != 1 {
		return "", "", fmt.Errorf("unexpected yaml format: %s", data)
	}
	var cacheName string
	var cacheConfig interface{}
	// Retrieve the first (and only) entry in the map
	for cacheName, cacheConfig = range config.Infinispan.CacheContainer.Caches {
		break
	}

	configYaml, err := yaml.Marshal(cacheConfig)
	if err != nil {
		return "", "", fmt.Errorf("unable to marshall cache configuration: %w", err)
	}
	return cacheName, string(configYaml), nil
}

func DefaultCacheTemplateXML(podName string, infinispan *v1.Infinispan, k8 *kube.Kubernetes, logger logr.Logger) (string, error) {
	namespace := infinispan.Namespace
	memoryLimitBytes, err := GetPodMemoryLimitBytes(podName, namespace, k8)
	if err != nil {
		logger.Error(err, "unable to extract memory limit (bytes) from pod")
		return "", err
	}

	maxUnboundedMemory, err := GetPodMaxMemoryUnboundedBytes(podName, namespace, k8)
	if err != nil {
		logger.Error(err, "unable to extract max memory unbounded from pod")
		return "", err
	}

	containerMaxMemory := maxUnboundedMemory
	if memoryLimitBytes < maxUnboundedMemory {
		containerMaxMemory = memoryLimitBytes
	}

	nativeMemoryOverhead := containerMaxMemory * (constants.CacheServiceJvmNativePercentageOverhead / 100)
	evictTotalMemoryBytes := containerMaxMemory - (constants.CacheServiceJvmNativeMb * 1024 * 1024) - (constants.CacheServiceFixedMemoryXmxMb * 1024 * 1024) - nativeMemoryOverhead
	replicationFactor := infinispan.Spec.Service.ReplicationFactor

	logger.Info("calculated maximum off-heap size", "size", evictTotalMemoryBytes, "container max memory", containerMaxMemory, "memory limit (bytes)", memoryLimitBytes, "max memory bound", maxUnboundedMemory)

	return fmt.Sprintf(constants.DefaultCacheTemplate, constants.DefaultCacheName, replicationFactor, evictTotalMemoryBytes), nil
}
