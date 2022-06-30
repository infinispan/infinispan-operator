package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	pipelineContext "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/context"
	pipelineBuilder "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/pipeline"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// InfinispanReconciler reconciles a Infinispan object
type InfinispanReconciler struct {
	client.Client
	log                logr.Logger
	contextProvider    infinispan.ContextProvider
	defaultLabels      map[string]string
	defaultAnnotations map[string]string
	supportedTypes     map[schema.GroupVersionKind]struct{}
	versionManager     *version.Manager
}

func (r *InfinispanReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	kubernetes := kube.NewKubernetesFromController(mgr)

	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName("Infinispan")
	r.contextProvider = pipelineContext.Provider(
		r.Client,
		mgr.GetScheme(),
		kubernetes,
		mgr.GetEventRecorderFor("controller-infinispan"),
	)

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

	r.supportedTypes = make(map[schema.GroupVersionKind]struct{}, 3)
	for _, gvk := range []schema.GroupVersionKind{infinispan.IngressGVK, infinispan.RouteGVK, infinispan.ServiceMonitorGVK} {
		// Validate that GroupVersionKind is supported on runtime platform
		ok, err := kubernetes.IsGroupVersionKindSupported(gvk)
		if err != nil {
			r.log.Error(err, fmt.Sprintf("failed to check if GVK '%s' is supported", gvk))
			continue
		}
		if ok {
			r.supportedTypes[gvk] = struct{}{}
		}
	}

	// Initialize supported Operand versions
	r.versionManager, err = version.ManagerFromEnv(infinispanv1.OperatorOperandVersionEnvVarName)
	if err != nil {
		return err
	}
	r.versionManager.Log(r.log)

	// Initialize default operator labels and annotations
	if defaultLabels, defaultAnnotations, err := infinispanv1.LoadDefaultLabelsAndAnnotations(); err != nil {
		return err
	} else {
		r.defaultLabels = defaultLabels
		r.defaultAnnotations = defaultAnnotations
		r.log.Info("Defaults:", "Annotations", defaultAnnotations, "Labels", defaultLabels)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&infinispanv1.Infinispan{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				switch e.Object.(type) {
				case *corev1.ConfigMap:
					return false
				case *corev1.Secret:
					return false
				case *appsv1.StatefulSet:
					return false
				}
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				switch e.Object.(type) {
				case *corev1.ConfigMap:
					return false
				case *infinispanv1.Infinispan:
					return false
				}
				return true
			},
		}).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(
				func(a client.Object) []reconcile.Request {
					var requests []reconcile.Request
					// Lookup only Secrets not controlled by Infinispan CR GVK. This means it's a custom defined Secret
					if !kube.IsControlledByGVK(a.GetOwnerReferences(), infinispanv1.SchemeBuilder.GroupVersion.WithKind(reflect.TypeOf(infinispanv1.Infinispan{}).Name())) {
						for _, field := range []string{"spec.security.endpointSecretName", "spec.security.endpointEncryption.certSecretName", "spec.security.endpointEncryption.clientCertSecretName"} {
							ispnList := &infinispanv1.InfinispanList{}
							if err := kubernetes.ResourcesListByField(a.GetNamespace(), field, a.GetName(), ispnList, ctx); err != nil {
								r.log.Error(err, "failed to list Infinispan CR")
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
		).
		Watches(
			&source.Kind{Type: &corev1.ConfigMap{}},
			handler.EnqueueRequestsFromMapFunc(
				func(a client.Object) []reconcile.Request {
					var requests []reconcile.Request
					// Lookup only ConfigMap not controlled by Infinispan CR GVK. This means it's a custom defined ConfigMap
					if !kube.IsControlledByGVK(a.GetOwnerReferences(), infinispanv1.SchemeBuilder.GroupVersion.WithKind(reflect.TypeOf(infinispanv1.Infinispan{}).Name())) {
						ispnList := &infinispanv1.InfinispanList{}
						if err := kubernetes.ResourcesListByField(a.GetNamespace(), "spec.configMapName", a.GetName(), ispnList, ctx); err != nil {
							r.log.Error(err, "failed to list Infinispan CR")
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
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=infinispan.org,namespace=infinispan-operator-system,resources=infinispans;infinispans/status;infinispans/finalizers,verbs=get;list;watch;create;update;patch

// +kubebuilder:rbac:groups=core,namespace=infinispan-operator-system,resources=persistentvolumeclaims;services;services/finalizers;endpoints;configmaps;pods;secrets,verbs=get;list;watch;create;update;delete;patch;deletecollection
// +kubebuilder:rbac:groups=core,namespace=infinispan-operator-system,resources=pods/logs,verbs=get
// +kubebuilder:rbac:groups=core,namespace=infinispan-operator-system,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core;events.k8s.io,namespace=infinispan-operator-system,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,namespace=infinispan-operator-system,resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=infinispan-operator-system,resources=roles;rolebindings,verbs=create;delete;update

// +kubebuilder:rbac:groups=apps,namespace=infinispan-operator-system,resources=replicasets,verbs=get
// +kubebuilder:rbac:groups=apps,namespace=infinispan-operator-system,resources=deployments;deployments/finalizers;statefulsets,verbs=get;list;watch;create;update;delete;patch

// +kubebuilder:rbac:groups=networking.k8s.io,namespace=infinispan-operator-system,resources=ingresses,verbs=get;list;watch;create;delete;deletecollection;update
// +kubebuilder:rbac:groups=networking.k8s.io,namespace=infinispan-operator-system,resources=customresourcedefinitions;customresourcedefinitions/status,verbs=get;list

// +kubebuilder:rbac:groups=route.openshift.io,namespace=infinispan-operator-system,resources=routes;routes/custom-host,verbs=get;list;watch;create;delete;deletecollection;update

// +kubebuilder:rbac:groups=monitoring.coreos.com,namespace=infinispan-operator-system,resources=servicemonitors,verbs=get;list;watch;create;delete;update

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions;customresourcedefinitions/status,verbs=get;list;watch

// Reconcile the Infinispan CR resource
func (r *InfinispanReconciler) Reconcile(ctx context.Context, ctrlRequest ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("infinispan", ctrlRequest.NamespacedName)
	// Fetch the Infinispan instance
	instance := &infinispanv1.Infinispan{}
	if err := r.Get(ctx, ctrlRequest.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("Infinispan CR not found")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("unable to fetch Infinispan CR %w", err)
	}

	// Don't reconcile Infinispan CRs marked for deletion
	if instance.GetDeletionTimestamp() != nil {
		reqLogger.Info(fmt.Sprintf("Ignoring Infinispan CR '%s:%s' marked for deletion", instance.Namespace, instance.Name))
		return reconcile.Result{}, nil
	}

	pipeline := pipelineBuilder.Builder().
		For(instance).
		WithAnnotations(r.defaultAnnotations).
		WithContextProvider(r.contextProvider).
		WithLabels(r.defaultLabels).
		WithLogger(reqLogger).
		WithSupportedTypes(r.supportedTypes).
		WithVersionManager(r.versionManager).
		Build()

	retry, delay, err := pipeline.Process(ctx)
	reqLogger.Info("Done", "requeue", retry, "requeueAfter", delay, "error", err)
	return ctrl.Result{Requeue: retry, RequeueAfter: delay}, err
}
