package secret

import (
	"context"
	"fmt"
	"net/url"

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

func (r reconcileSecret) Types() map[string]*resources.ReconcileType {
	return map[string]*resources.ReconcileType{"Secret": {ObjectType: &corev1.Secret{}, GroupVersion: corev1.SchemeGroupVersion, GroupVersionSupported: true}}
}

func (r reconcileSecret) EventsPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
	}
}

func Add(mgr manager.Manager) error {
	return resources.CreateController(ControllerName, &reconcileSecret{mgr.GetClient()}, mgr)
}

func (s *secretResource) Process() (reconcile.Result, error) {
	if err := s.reconcileAdminSecret(); err != nil {
		return reconcile.Result{}, nil
	}

	// If the user has provided their own secret or authentication is disabled, do nothing
	if !s.infinispan.IsGeneratedSecret() {
		return reconcile.Result{}, nil
	}

	// Create the user identities secret if it doesn't already exist
	secret, err := s.getSecret(s.infinispan.GetSecretName())
	if secret != nil || err != nil {
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
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{consts.ServerIdentitiesFilename: identities},
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

func (s secretResource) reconcileAdminSecret() error {
	adminSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.infinispan.GetAdminSecretName(),
			Namespace: s.infinispan.Namespace,
		},
	}

	_, err := kube.CreateOrPatch(ctx, s.client, adminSecret, func() error {
		if adminSecret.CreationTimestamp.IsZero() {
			identities, err := users.GetAdminCredentials()
			if err != nil {
				return err
			}
			if adminSecret.Data == nil {
				adminSecret.Data = map[string][]byte{}
			}
			adminSecret.Labels = infinispan.LabelsResource(s.infinispan.Name, "infinispan-secret-admin-identities")
			adminSecret.Data[consts.ServerIdentitiesFilename] = identities
			if err = controllerutil.SetControllerReference(s.infinispan, adminSecret, s.scheme); err != nil {
				return err
			}
		}
		pass, ok := adminSecret.Data[consts.AdminPasswordKey]
		password := string(pass)
		if !ok || password == "" {
			var usrErr error
			if password, usrErr = users.FindPassword(consts.DefaultOperatorUser, adminSecret.Data[consts.ServerIdentitiesFilename]); usrErr != nil {
				return usrErr
			}
		}
		identities, err := users.CreateIdentitiesFor(consts.DefaultOperatorUser, password)
		if err != nil {
			return err
		}
		s.addCliProperties(adminSecret, password)
		s.addServiceMonitorProperties(adminSecret, password)
		adminSecret.Data[consts.ServerIdentitiesFilename] = identities
		return nil
	})
	return err
}

func (s secretResource) addCliProperties(secret *corev1.Secret, password string) {
	service := s.infinispan.GetServiceName()
	url := fmt.Sprintf("http://%s:%s@%s:%d", consts.DefaultOperatorUser, url.QueryEscape(password), service, consts.InfinispanAdminPort)
	properties := fmt.Sprintf("autoconnect-url=%s", url)
	secret.Data[consts.CliPropertiesFilename] = []byte(properties)
}

func (s secretResource) addServiceMonitorProperties(secret *corev1.Secret, password string) {
	secret.Data[consts.AdminPasswordKey] = []byte(password)
	secret.Data[consts.AdminUsernameKey] = []byte(consts.DefaultOperatorUser)
}

func (s secretResource) getSecret(name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := s.client.Get(ctx, types.NamespacedName{Namespace: s.infinispan.Namespace, Name: name}, secret)
	if err == nil {
		return secret, nil
	}
	if errors.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}
