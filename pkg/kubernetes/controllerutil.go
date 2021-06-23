package kubernetes

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// LookupResource lookup for resource to be created by separate resource controller
func LookupResource(name, namespace string, resource, caller client.Object, client client.Client, logger logr.Logger, eventRec record.EventRecorder, ctx context.Context) (*reconcile.Result, error) {
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, resource)
	if err != nil {
		if k8serrors.IsNotFound(err) {
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

func LookupServiceAccountTokenSecret(name, namespace string, client client.Client, ctx context.Context) (*corev1.Secret, error) {
	serviceAccount := &corev1.ServiceAccount{}
	if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, serviceAccount); err != nil {
		return nil, err
	}
	for _, secretReference := range serviceAccount.Secrets {
		secret := &corev1.Secret{}
		if err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretReference.Name}, secret); err != nil {
			continue
		}
		if isServiceAccountToken(secret, serviceAccount) {
			return secret, nil
		}
	}
	return nil, fmt.Errorf("could not find a service account token secret for service account %q", serviceAccount.Name)
}

// isServiceAccountToken returns true if the secret is a valid api token for the service account
func isServiceAccountToken(secret *corev1.Secret, sa *corev1.ServiceAccount) bool {
	if secret.Type != corev1.SecretTypeServiceAccountToken {
		return false
	}

	name := secret.Annotations[corev1.ServiceAccountNameKey]
	uid := secret.Annotations[corev1.ServiceAccountUIDKey]
	if name != sa.Name {
		// Name must match
		return false
	}
	if len(uid) > 0 && uid != string(sa.UID) {
		// If UID is specified, it must match
		return false
	}

	return true
}

func IsControlledByGVK(refs []metav1.OwnerReference, gvk schema.GroupVersionKind) bool {
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller && ref.APIVersion == gvk.GroupVersion().String() && ref.Kind == gvk.Kind {
			return true
		}
	}
	return false
}

func RemoveOwnerReference(obj, owner metav1.Object) {
	ownerReferences := obj.GetOwnerReferences()
	for index, ref := range ownerReferences {
		if ref.UID == owner.GetUID() {
			ownerReferences[index] = ownerReferences[len(ownerReferences)-1]
			obj.SetOwnerReferences(ownerReferences[:len(ownerReferences)-1])
			break
		}
	}
}
