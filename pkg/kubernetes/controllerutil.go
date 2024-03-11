package kubernetes

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	EventReasonResourceNotReady = "ResourceNotReady"
)

// LookupResource lookup for resource to be created by separate resource controller
func LookupResource(name, namespace string, resource, caller client.Object, client client.Client, logger logr.Logger, eventRec record.EventRecorder, ctx context.Context) (*reconcile.Result, error) {
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, resource)
	if err != nil {
		if errors.IsNotFound(err) {
			if eventRec != nil {
				objMeta, err1 := meta.Accessor(resource)
				if err1 == nil {
					if objMeta.GetName() == "" {
						objMeta.SetName(name)
					}
					if objMeta.GetNamespace() == "" {
						objMeta.SetNamespace(namespace)
					}
					eventRec.Event(caller, corev1.EventTypeWarning, EventReasonResourceNotReady, fmt.Sprintf("%s resource '%s' not ready", reflect.TypeOf(resource).Elem().Name(), name))
				}
			}
			logger.Info(fmt.Sprintf("%s resource '%s' not ready", reflect.TypeOf(resource).Elem().Name(), name))
			return &reconcile.Result{RequeueAfter: consts.DefaultWaitOnCreateResource}, nil
		}
		return &reconcile.Result{}, err
	}
	return nil, nil
}

func IsControlledByGVK(refs []metav1.OwnerReference, gvk schema.GroupVersionKind) bool {
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller && ref.APIVersion == gvk.GroupVersion().String() && ref.Kind == gvk.Kind {
			return true
		}
	}
	return false
}
