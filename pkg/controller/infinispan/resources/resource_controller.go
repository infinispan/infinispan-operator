package resources

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/base"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type Resource interface {
	Process() (reconcile.Result, error)
}

type ReconcileType struct {
	ObjectType            runtime.Object
	GroupVersion          schema.GroupVersion
	GroupVersionSupported bool
}

func (rt ReconcileType) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   rt.GroupVersion.Group,
		Version: rt.GroupVersion.Version,
		Kind:    rt.Kind(),
	}
}

func (rt ReconcileType) Kind() string {
	return reflect.TypeOf(rt.ObjectType).Elem().Name()
}

type Reconciler interface {
	// The k8 structs being handled by this controller
	Types() []*ReconcileType
	// Events for the struct handled by this controller
	EventsPredicate() predicate.Predicate
	// Create a new instance of Infinispan wrapping the actual k8s type
	ResourceInstance(infinispan *ispnv1.Infinispan, ctrl *Controller) Resource
}

type Controller struct {
	*base.ReconcilerBase
	Reconciler Reconciler
}

func CreateController(name string, reconciler Reconciler, mgr manager.Manager) error {
	r := &Controller{
		ReconcilerBase: base.NewReconcilerBaseFromManager(name, mgr),
		Reconciler:     reconciler,
	}

	// Create a new controller
	c, err := controller.New(name, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	infinispanPredicate := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}

	// Watch for changes to primary Infinispan resource
	err = c.Watch(&source.Kind{Type: &ispnv1.Infinispan{}}, &handler.EnqueueRequestForObject{}, infinispanPredicate)
	if err != nil {
		return err
	}

	// Watch for changes to secondary configured resources
	for index, obj := range reconciler.Types() {
		if !obj.GroupVersionSupported {
			// Validate that GroupVersion is supported on runtime platform
			ok, err := r.IsGroupVersionSupported(obj.GroupVersion.String(), obj.Kind())
			if err != nil {
				r.Logger().Error(err, fmt.Sprintf("Failed to check if GVK '%s' is supported", obj.GroupVersionKind()))
				continue
			}
			reconciler.Types()[index].GroupVersionSupported = ok
			if !ok {
				continue
			}
		}
		err := c.Watch(&source.Kind{Type: obj.ObjectType}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &ispnv1.Infinispan{},
		}, reconciler.EventsPredicate())
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Controller) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reconciler := r.Reconciler
	var objects []string
	for _, obj := range reconciler.Types() {
		if obj.GroupVersionSupported {
			objects = append(objects, obj.Kind())
		}
	}
	resources := strings.Join(objects, ",")
	namespace := request.Namespace
	r.InitLogger("Reconciling", resources, "Request.Namespace", namespace, "Request.Name", request.Name)

	infinispan := &ispnv1.Infinispan{}
	if err := r.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: request.Name}, infinispan); err != nil {
		if errors.IsNotFound(err) {
			r.Logger().Info("Infinispan CR not found")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("unable to fetch Infinispan CR %w", err)
	}

	// Validate that Infinispan CR passed all preliminary checks
	if !infinispan.IsConditionTrue(ispnv1.ConditionPrelimChecksPassed) {
		r.Logger().Info("Infinispan CR is not preliminary check passed")
		return reconcile.Result{}, nil
	}

	instance := reconciler.ResourceInstance(infinispan, r)
	// Process resource
	return instance.Process()
}
