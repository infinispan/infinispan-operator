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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// OperationResultUpdatedStatus means that an existing resource and its status is updated
	OperationResultUpdatedStatus controllerutil.OperationResult = "updatedStatus"
	// OperationResultUpdatedStatusOnly means that only an existing status is updated
	OperationResultUpdatedStatusOnly controllerutil.OperationResult = "updatedStatusOnly"

	EventReasonResourceNotReady = "ResourceNotReady"
)

// CreateOrPatch DO NOT REMOVE UNTIL controller-runtime v0.9.0
// https://github.com/kubernetes-sigs/controller-runtime/pull/1403
//
// CreateOrPatch creates or patches the given object in the Kubernetes
// cluster. The object's desired state must be reconciled with the before
// state inside the passed in callback MutateFn.
//
// The MutateFn is called regardless of creating or updating an object.
//
// It returns the executed operation and an error.
//
// Ported from https://github.com/kubernetes-sigs/controller-runtime/blob/master/pkg/controller/controllerutil/controllerutil.go
func CreateOrPatch(ctx context.Context, c client.Client, obj client.Object, f controllerutil.MutateFn) (controllerutil.OperationResult, error) {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Get(ctx, key, obj); err != nil {
		if !errors.IsNotFound(err) {
			return controllerutil.OperationResultNone, err
		}
		if f != nil {
			if err := mutate(f, key, obj); err != nil {
				return controllerutil.OperationResultNone, err
			}
		}
		if err := c.Create(ctx, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
		return controllerutil.OperationResultCreated, nil
	}

	// Create patches for the object and its possible status.
	objPatch := client.MergeFrom(obj.DeepCopyObject())
	statusPatch := client.MergeFrom(obj.DeepCopyObject())

	// Create a copy of the original object as well as converting that copy to
	// unstructured data.
	before, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj.DeepCopyObject())
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	// Attempt to extract the status from the resource for easier comparison later
	beforeStatus, hasBeforeStatus, err := unstructured.NestedFieldCopy(before, "status")
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	// If the resource contains a status then remove it from the unstructured
	// copy to avoid unnecessary patching later.
	if hasBeforeStatus {
		unstructured.RemoveNestedField(before, "status")
	}

	// Mutate the original object.
	if f != nil {
		if err := mutate(f, key, obj); err != nil {
			return controllerutil.OperationResultNone, err
		}
	}

	// Convert the resource to unstructured to compare against our before copy.
	after, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	// Attempt to extract the status from the resource for easier comparison later
	afterStatus, hasAfterStatus, err := unstructured.NestedFieldCopy(after, "status")
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	// If the resource contains a status then remove it from the unstructured
	// copy to avoid unnecessary patching later.
	if hasAfterStatus {
		unstructured.RemoveNestedField(after, "status")
	}

	result := controllerutil.OperationResultNone

	if !reflect.DeepEqual(before, after) {
		// Only issue a Patch if the before and after resources (minus status) differ
		if err := c.Patch(ctx, obj, objPatch); err != nil {
			return result, err
		}
		result = controllerutil.OperationResultUpdated
	}

	if (hasBeforeStatus || hasAfterStatus) && !reflect.DeepEqual(beforeStatus, afterStatus) {
		// Only issue a Status Patch if the resource has a status and the beforeStatus
		// and afterStatus copies differ
		if result == controllerutil.OperationResultUpdated {
			// If Status was replaced by Patch before, set it to afterStatus
			objectAfterPatch, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			if err != nil {
				return result, err
			}
			if err = unstructured.SetNestedField(objectAfterPatch, afterStatus, "status"); err != nil {
				return result, err
			}
			// If Status was replaced by Patch before, restore patched structure to the obj
			if err = runtime.DefaultUnstructuredConverter.FromUnstructured(objectAfterPatch, obj); err != nil {
				return result, err
			}
		}
		if err := c.Status().Patch(ctx, obj, statusPatch); err != nil {
			return result, err
		}
		if result == controllerutil.OperationResultUpdated {
			result = OperationResultUpdatedStatus
		} else {
			result = OperationResultUpdatedStatusOnly
		}
	}

	return result, nil
}

// mutate wraps a MutateFn and applies validation to its result
func mutate(f controllerutil.MutateFn, key client.ObjectKey, obj client.Object) error {
	if err := f(); err != nil {
		return err
	}
	if newKey := client.ObjectKeyFromObject(obj); key != newKey {
		return fmt.Errorf("MutateFn cannot mutate object name and/or object namespace")
	}
	return nil
}

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
