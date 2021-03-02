package secret

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ControllerName = "secret-controller"
)

var ctx = context.Background()

// reconcileSecret reconciles a Secret object
type reconcileSecret struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
}

type secretResource struct {
	infinispan *ispnv1.Infinispan
	client     client.Client
	scheme     *runtime.Scheme
	kube       *kube.Kubernetes
	log        logr.Logger
}

func (r reconcileSecret) ResourceInstance(infinispan *ispnv1.Infinispan, ctrl *resources.Controller, kube *kube.Kubernetes, log logr.Logger) resources.Resource {
	return &secretResource{
		infinispan: infinispan,
		client:     r.Client,
		scheme:     ctrl.Scheme,
		kube:       kube,
		log:        log,
	}
}

func (r reconcileSecret) Types() []*resources.ReconcileType {
	return []*resources.ReconcileType{{&corev1.Secret{}, corev1.SchemeGroupVersion, true}}
}

func (r reconcileSecret) EventsPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
	}
}

func Add(mgr manager.Manager) error {
	return resources.CreateController(ControllerName, &reconcileSecret{mgr.GetClient()}, mgr)
}

func (s *secretResource) Process() (reconcile.Result, error) {
	secretExists, err := s.secretExists(s.infinispan.GetAdminSecretName())
	if err != nil {
		return reconcile.Result{}, err
	}

	// If the operator secret does not already exist create it with generated identities
	if !secretExists {
		if err = s.createAdminIdentitiesSecret(); err != nil {
			return reconcile.Result{}, err
		}
	}

	// If the user has provided their own secret or authentication is disabled, do nothing
	if !s.infinispan.IsGeneratedSecret() {
		return reconcile.Result{}, nil
	}

	// Create the user identities secret if it doesn't already exist
	secretExists, err = s.secretExists(s.infinispan.GetSecretName())
	if secretExists || err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, s.createUserIdentitiesSecret()
}

func (s secretResource) createUserIdentitiesSecret() error {
	identities, err := users.GetUserCredentials()
	if err != nil {
		return err
	}
	return s.createSecret(s.infinispan.GetSecretName(), "infinispan-secret-identities", identities)
}

func (s secretResource) createAdminIdentitiesSecret() error {
	identities, err := users.GetAdminCredentials()
	if err != nil {
		return err
	}
	return s.createSecret(s.infinispan.GetAdminSecretName(), "infinispan-secret-admin-identities", identities)
}

func (s secretResource) createSecret(name, label string, identities []byte) error {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.infinispan.Namespace,
			Labels:    infinispan.LabelsResource(s.infinispan.Name, label),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{consts.ServerIdentitiesFilename: string(identities)},
	}

	s.log.Info(fmt.Sprintf("Creating Identities Secret %s", secret.Name))
	_, err := controllerutil.CreateOrUpdate(ctx, s.client, secret, func() error {
		return controllerutil.SetControllerReference(s.infinispan, secret, s.scheme)
	})

	if err != nil {
		return fmt.Errorf("Unable to create identities secret: %w", err)
	}
	return nil
}

func (s secretResource) secretExists(name string) (bool, error) {
	err := s.client.Get(ctx, types.NamespacedName{Namespace: s.infinispan.Namespace, Name: name}, &corev1.Secret{})
	if err == nil {
		return true, nil
	}
	if errors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}
