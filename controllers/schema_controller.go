package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// SchemaReconciler reconciles a Schema object
type SchemaReconciler struct {
	client.Client
	log            logr.Logger
	scheme         *runtime.Scheme
	kubernetes     *kube.Kubernetes
	eventRec       record.EventRecorder
	versionManager *version.Manager
}

type SchemaListener struct {
	// The Infinispan cluster to listen to in the configured namespace
	Infinispan     *v1.Infinispan
	Ctx            context.Context
	Kubernetes     *kube.Kubernetes
	Log            *zap.SugaredLogger
	VersionManager *version.Manager
}

type schemaRequest struct {
	*SchemaReconciler
	ctx        context.Context
	schema     *v2alpha1.Schema
	infinispan *v1.Infinispan
	ispnClient api.Infinispan
	reqLogger  logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *SchemaReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) (err error) {
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName("Schema")
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor("schema-controller")

	r.versionManager, err = version.ManagerFromEnv(v1.OperatorOperandVersionEnvVarName)
	if err != nil {
		return
	}

	if err = mgr.GetFieldIndexer().IndexField(ctx, &v2alpha1.Schema{}, "spec.clusterName", func(obj client.Object) []string {
		return []string{obj.(*v2alpha1.Schema).Spec.ClusterName}
	}); err != nil {
		return
	}

	builder := ctrl.NewControllerManagedBy(mgr).For(&v2alpha1.Schema{})
	builder.Watches(
		&source.Kind{Type: &v1.Infinispan{}},
		handler.EnqueueRequestsFromMapFunc(
			func(a client.Object) []reconcile.Request {
				i := a.(*v1.Infinispan)
				// Only enqueue requests once an Infinispan CR has the WellFormed condition or it has been deleted
				if !i.HasCondition(v1.ConditionWellFormed) || !a.GetDeletionTimestamp().IsZero() {
					return nil
				}

				var requests []reconcile.Request
				schemaList := &v2alpha1.SchemaList{}
				if err := r.kubernetes.ResourcesListByField(a.GetNamespace(), "spec.clusterName", a.GetName(), schemaList, ctx); err != nil {
					r.log.Error(err, "watches failed to list Schema CRs")
				}

				for _, item := range schemaList.Items {
					requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: item.GetNamespace(), Name: item.GetName()}})
				}
				return requests
			}),
	)
	return builder.Complete(r)
}

// +kubebuilder:rbac:groups=infinispan.org,namespace=infinispan-operator-system,resources=schemas;schemas/status;schemas/finalizers,verbs=get;list;watch;create;update;patch;delete

func (r *SchemaReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("+++++ Reconciling Schema.")
	defer reqLogger.Info("----- End Reconciling Schema.")

	// Fetch the Schema instance
	instance := &v2alpha1.Schema{}
	if err := r.Client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Schema resource not found. Ignoring.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	infinispan := &v1.Infinispan{}
	s := &schemaRequest{
		SchemaReconciler: r,
		ctx:              ctx,
		schema:           instance,
		infinispan:       infinispan,
		reqLogger:        reqLogger,
	}

	if s.markedForDeletion() {
		reqLogger.Info("Schema CR marked for deletion. Attempting to remove.")
		if err := s.removeFinalizer(); err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Unable to remove Finalizer as Schema CR not found.")
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		if err := s.kubernetes.Client.Delete(ctx, instance); err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Schema CR does not exist, nothing todo.")
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		reqLogger.Info("Schema CR Removed.")
		return ctrl.Result{}, nil
	}

	crDeleted := instance.GetDeletionTimestamp() != nil

	// Fetch the Infinispan cluster
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.ClusterName}, infinispan); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Infinispan cluster %s not found", instance.Spec.ClusterName))
			if crDeleted {
				return ctrl.Result{}, s.removeFinalizer()
			}
			return ctrl.Result{}, s.update(func() error {
				instance.SetCondition(v2alpha1.SchemaConditionReady, metav1.ConditionFalse, "")
				return nil
			})
		}
		return ctrl.Result{}, err
	}

	// Cluster must be well formed
	if !infinispan.IsWellFormed() {
		reqLogger.Info(fmt.Sprintf("Infinispan cluster %s not well formed", infinispan.Name))
		return ctrl.Result{}, nil
	}

	ispnClient, err := NewInfinispan(ctx, infinispan, r.versionManager, r.kubernetes)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to create Infinispan client: %w", err)
	}
	s.ispnClient = ispnClient

	if crDeleted {
		if controllerutil.ContainsFinalizer(instance, constants.InfinispanFinalizer) {
			schemaName := instance.GetSchemaName()
			if err := ispnClient.Schema(schemaName).Delete(); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, s.removeFinalizer()
		}
		return ctrl.Result{}, nil
	}

	// Don't contact the Infinispan server for resources created by the ConfigListener
	if s.reconcileOnServer() {
		if result, err := s.createOrUpdate(); result != nil {
			if err != nil {
				return *result, s.update(func() error {
					instance.SetCondition(v2alpha1.SchemaConditionReady, metav1.ConditionFalse, err.Error())
					return nil
				})
			}
			return *result, err
		}
	}

	err = s.update(func() error {
		instance.SetCondition(v2alpha1.SchemaConditionReady, metav1.ConditionTrue, "")
		if !controllerutil.ContainsFinalizer(instance, constants.InfinispanFinalizer) {
			controllerutil.AddFinalizer(instance, constants.InfinispanFinalizer)
		}
		return nil
	})
	return ctrl.Result{}, err
}

func (r *schemaRequest) update(mutate func() error) error {
	return updateSchema(r.schema, r.ctx, r.Client, mutate)
}

// Determine if reconciliation was triggered by the ConfigListener
func (r *schemaRequest) reconcileOnServer() bool {
	if val, exists := r.schema.ObjectMeta.Annotations[constants.ListenerAnnotationGeneration]; exists {
		generation, _ := strconv.ParseInt(val, 10, 64)
		return generation < r.schema.GetGeneration()
	}
	return true
}

func (r *schemaRequest) markedForDeletion() bool {
	_, exists := r.schema.ObjectMeta.Annotations[constants.ListenerAnnotationDelete]
	return exists
}

func (r *schemaRequest) removeFinalizer() error {
	if controllerutil.ContainsFinalizer(r.schema, constants.InfinispanFinalizer) {
		return r.update(func() error {
			controllerutil.RemoveFinalizer(r.schema, constants.InfinispanFinalizer)
			return nil
		})
	}
	return nil
}

func (r *schemaRequest) createOrUpdate() (*ctrl.Result, error) {
	schemaName := r.schema.GetSchemaName()
	schemaClient := r.ispnClient.Schema(schemaName)

	// Use PUT to create or update the schema
	response, err := schemaClient.CreateOrUpdate(r.schema.Spec.Schema)
	if err != nil {
		err = fmt.Errorf("unable to create or update schema '%s': %w", schemaName, err)
		r.reqLogger.Error(err, "")
		return &ctrl.Result{Requeue: true}, err
	}

	if response.Error != nil {
		err = fmt.Errorf("schema '%s' has errors: %s", schemaName, response.Error.Message)
		r.reqLogger.Error(err, "Schema validation error", "cause", response.Error.Cause)
		return &ctrl.Result{}, err
	}

	return nil, nil
}

// SchemaListener methods for SSE event handling

var schemaNameRegexp = regexp.MustCompile("[^-a-z0-9]")

func (sl *SchemaListener) RemoveStaleResources(podName string) error {
	sl.Log.Info("Checking for stale schema resources")
	k8s := sl.Kubernetes
	ispn, err := NewInfinispanForPod(sl.Ctx, podName, sl.Infinispan, sl.VersionManager, k8s)
	if err != nil {
		return err
	}

	// Retrieve schemas defined on the server
	serverSchemas, err := ispn.Schemas().Names()
	if err != nil {
		return err
	}
	sl.Log.Debugf("Schemas defined on the server: '%v'", serverSchemas)

	// Create Set of server schema names for O(1) lookup
	serverSchemaSet := make(map[string]struct{}, len(serverSchemas))
	for _, s := range serverSchemas {
		serverSchemaSet[s.Name] = struct{}{}
	}

	// Retrieve list of all Schema CRs in namespace
	schemaList := &v2alpha1.SchemaList{}
	if err := k8s.Client.List(sl.Ctx, schemaList, &client.ListOptions{Namespace: sl.Infinispan.Namespace}); err != nil {
		return fmt.Errorf("unable to retrieve existing Schema resources: %w", err)
	}

	// Iterate over all existing CRs, marking for deletion any that do not have a schema definition on the server
	for _, s := range schemaList.Items {
		listenerCreated := kube.IsOwnedBy(&s, sl.Infinispan)
		_, schemaExists := serverSchemaSet[s.GetSchemaName()]
		sl.Log.Debugf("Checking if Schema CR '%s' is stale. ListenerCreated=%t. SchemaExists=%t", s.Name, listenerCreated, schemaExists)
		if listenerCreated && !schemaExists {
			s.ObjectMeta.Annotations[constants.ListenerAnnotationDelete] = "true"
			sl.Log.Infof("Marking stale Schema resource '%s' for deletion", s.Name)
			if err := k8s.Client.Update(sl.Ctx, &s); err != nil {
				if !errors.IsNotFound(err) {
					return fmt.Errorf("unable to mark Schema '%s' for deletion: %w", s.Name, err)
				}
			}
		}
	}
	return nil
}

func (sl *SchemaListener) CreateOrUpdate(data []byte) error {
	namespace := sl.Infinispan.Namespace
	clusterName := sl.Infinispan.Name
	schemaName, proto, err := unmarshallSchemaEventConfig(data)
	if err != nil {
		return err
	}

	existingSchema, err := sl.findExistingSchemaCR(schemaName, clusterName)
	if err != nil {
		return err
	}

	k8sClient := sl.Kubernetes.Client

	if existingSchema == nil {
		// Create new Schema CR
		sanitizedName := schemaNameRegexp.ReplaceAllString(strcase.ToKebab(schemaName), "-")
		errs := validation.IsDNS1123Subdomain(sanitizedName)
		if len(errs) > 0 {
			return fmt.Errorf("unable to create Schema Resource Name for schema=%s, cluster=%s: %s", schemaName, clusterName, fmt.Sprintf("%v", errs))
		}

		s := &v2alpha1.Schema{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: sanitizedName + "-",
				Namespace:    namespace,
				Annotations: map[string]string{
					constants.ListenerAnnotationGeneration: "1",
				},
			},
			Spec: v2alpha1.SchemaSpec{
				ClusterName: clusterName,
				Name:        schemaName,
				Schema:      proto,
			},
		}

		controllerutil.AddFinalizer(s, constants.InfinispanFinalizer)
		if err := controllerutil.SetOwnerReference(sl.Infinispan, s, k8sClient.Scheme()); err != nil {
			return err
		}

		sl.Log.Infof("Creating Schema CR for '%s'", schemaName)
		if err := k8sClient.Create(sl.Ctx, s); err != nil {
			return fmt.Errorf("unable to create Schema CR for schema '%s': %w", schemaName, err)
		}
		sl.Log.Infof("Schema CR '%s' created", s.Name)
	} else {
		// Update existing Schema CR
		maxRetries := 5
		for i := 1; i <= maxRetries; i++ {
			if existingSchema.Spec.Schema == proto {
				sl.Log.Debugf("Schema '%s' content on update has not changed, ignoring update", existingSchema.Name)
				break
			}

			_, err = controllerutil.CreateOrPatch(sl.Ctx, k8sClient, existingSchema, func() error {
				if existingSchema.CreationTimestamp.IsZero() {
					return errors.NewNotFound(schema.ParseGroupResource("schemas.infinispan.org"), existingSchema.Name)
				}
				if existingSchema.ObjectMeta.Annotations == nil {
					existingSchema.ObjectMeta.Annotations = make(map[string]string, 1)
				}
				controllerutil.AddFinalizer(existingSchema, constants.InfinispanFinalizer)
				existingSchema.ObjectMeta.Annotations[constants.ListenerAnnotationGeneration] = strconv.FormatInt(existingSchema.GetGeneration()+1, 10)
				existingSchema.Spec = v2alpha1.SchemaSpec{
					Name:        schemaName,
					ClusterName: clusterName,
					Schema:      proto,
				}
				return nil
			})
			if err == nil {
				break
			}

			if !errors.IsConflict(err) {
				return fmt.Errorf("unable to Update Schema CR '%s': %w", existingSchema.Name, err)
			}
			sl.Log.Errorf("Conflict encountered on Schema CR '%s' update. Retry %d..%d", existingSchema.Name, i, maxRetries)
		}
		if err != nil {
			return fmt.Errorf("unable to Update Schema CR %s after %d attempts", existingSchema.Name, maxRetries)
		}
	}
	return nil
}

func (sl *SchemaListener) Delete(data []byte) error {
	schemaName := string(data)
	sl.Log.Infof("Processing remove event for schema '%s'", schemaName)

	existingSchemaCr, err := sl.findExistingSchemaCR(schemaName, sl.Infinispan.Name)
	if existingSchemaCr == nil || err != nil {
		return err
	}

	if existingSchemaCr.ObjectMeta.Annotations != nil {
		_, controllerDelete := existingSchemaCr.ObjectMeta.Annotations[constants.ListenerControllerDelete]
		if controllerDelete {
			sl.Log.Infof("Retaining Schema CR '%s' as schema deletion was requested by the schema controller", existingSchemaCr.Name)
			return updateSchema(existingSchemaCr, sl.Ctx, sl.Kubernetes.Client, func() error {
				delete(existingSchemaCr.ObjectMeta.Annotations, constants.ListenerControllerDelete)
				return nil
			})
		}
	}

	s := &v2alpha1.Schema{}
	existingSchemaCr.DeepCopyInto(s)

	sl.Log.Infof("Marking Schema CR '%s' for removal", s.Name)
	_, err = controllerutil.CreateOrPatch(sl.Ctx, sl.Kubernetes.Client, s, func() error {
		if s.CreationTimestamp.IsZero() || !s.DeletionTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("schemas.infinispan.org"), s.Name)
		}
		if s.ObjectMeta.Annotations == nil {
			s.ObjectMeta.Annotations = make(map[string]string, 1)
		}
		s.ObjectMeta.Annotations[constants.ListenerAnnotationDelete] = "true"
		return nil
	})
	if !errors.IsNotFound(err) {
		sl.Log.Debugf("Schema CR '%s' not found, nothing todo.", s.Name)
		return err
	}
	return nil
}

func (sl *SchemaListener) findExistingSchemaCR(schemaName, clusterName string) (*v2alpha1.Schema, error) {
	schemaList := &v2alpha1.SchemaList{}
	listOpts := &client.ListOptions{
		Namespace: sl.Infinispan.Namespace,
	}
	if err := sl.Kubernetes.Client.List(sl.Ctx, schemaList, listOpts); err != nil {
		return nil, fmt.Errorf("unable to list existing Schema CRs: %w", err)
	}

	var schemas []v2alpha1.Schema
	for _, s := range schemaList.Items {
		if s.GetSchemaName() == schemaName && s.Spec.ClusterName == clusterName {
			schemas = append(schemas, s)
		}
	}
	switch len(schemas) {
	case 0:
		sl.Log.Debugf("No existing Schema CR found for Schema=%s, Cluster=%s", schemaName, clusterName)
		return nil, nil
	case 1:
		sl.Log.Debugf("An existing Schema CR '%s' was found for Schema=%s, Cluster=%s", schemas[0].Name, schemaName, clusterName)
		return &schemas[0], nil
	default:
		y, _ := json.Marshal(schemas)
		return nil, fmt.Errorf("More than one Schema CR found for Schema=%s, Cluster=%s:\n%s", schemaName, clusterName, string(y))
	}
}

func updateSchema(s *v2alpha1.Schema, ctx context.Context, client client.Client, mutate func() error) error {
	_, err := controllerutil.CreateOrPatch(ctx, client, s, func() error {
		if s.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("schemas.infinispan.org"), s.Name)
		}
		return mutate()
	})
	if err != nil {
		return fmt.Errorf("unable to update schema %s: %w", s.Name, err)
	}
	return nil
}

// unmarshallSchemaEventConfig extracts the schema name and proto content from SSE event data
// Event format: {"schema":{"name":"person.proto","errors":"","proto":"message Person { ... }"}}
func unmarshallSchemaEventConfig(data []byte) (string, string, error) {
	type SchemaEvent struct {
		Schema struct {
			Name   string `json:"name"`
			Errors string `json:"errors"`
			Proto  string `json:"proto"`
		} `json:"schema"`
	}

	event := &SchemaEvent{}
	if err := json.Unmarshal(data, event); err != nil {
		return "", "", fmt.Errorf("unable to unmarshal schema event data: %w", err)
	}

	if event.Schema.Name == "" {
		return "", "", fmt.Errorf("unexpected schema event json format: %s", data)
	}

	return event.Schema.Name, event.Schema.Proto, nil
}
