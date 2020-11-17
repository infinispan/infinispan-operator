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
	if !s.infinispan.IsGeneratedSecret() {
		return reconcile.Result{}, nil
	}
	if err := s.client.Get(context.TODO(), types.NamespacedName{Namespace: s.infinispan.Namespace, Name: s.infinispan.GetSecretName()}, &corev1.Secret{}); err == nil || !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	}

	s.log.Info("Creating identities for Secret")
	// Generate the Secret identities
	if identities, err := users.GetCredentials(); err == nil {
		// Generate the Secret
		s.log.Info(fmt.Sprintf("Creating Secret %s", s.infinispan.GetSecretName()))
		return reconcile.Result{}, s.client.Create(ctx, s.computeSecret(identities))
	} else {
		return reconcile.Result{}, err
	}
}

func (s secretResource) computeSecret(identities []byte) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.infinispan.GetSecretName(),
			Namespace: s.infinispan.Namespace,
			Labels:    infinispan.LabelsResource(s.infinispan.Name, "infinispan-secret-identities"),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{consts.ServerIdentitiesFilename: string(identities)},
	}
	controllerutil.SetControllerReference(s.infinispan, secret, s.scheme)
	return secret
}
