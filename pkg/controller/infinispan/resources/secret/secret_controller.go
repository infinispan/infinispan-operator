package secret

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ControllerName            = "secret-controller"
	EncryptClientCertPrefix   = "trust.cert."
	EncryptClientCAName       = "trust.ca"
	EncryptTruststorePassword = "password"
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
	eventRec   record.EventRecorder
}

func (r reconcileSecret) ResourceInstance(infinispan *ispnv1.Infinispan, ctrl *resources.Controller, kube *kube.Kubernetes, log logr.Logger) resources.Resource {
	return &secretResource{
		infinispan: infinispan,
		client:     r.Client,
		scheme:     ctrl.Scheme,
		kube:       kube,
		log:        log,
		eventRec:   ctrl.EventRec,
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
	// Reconcile Encryption Secrets
	if result, err := s.reconcileKeystoreSecret(); result != nil {
		return *result, err
	}

	if result, err := s.reconcileTruststoreSecret(); result != nil {
		return *result, err
	}

	// Reconcile Credential Secrets
	if err := s.reconcileAdminSecret(); err != nil {
		return reconcile.Result{}, nil
	}

	// If the user has provided their own secret or authentication is disabled, do nothing
	if !s.infinispan.IsAuthenticationEnabled() || !s.infinispan.IsGeneratedSecret() {
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
	identities, err := security.GetUserCredentials()
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
		return s.setControllerRef(secret)
	})

	if err != nil {
		return fmt.Errorf("Unable to create identities secret: %w", err)
	}
	return nil
}

func (s secretResource) reconcileKeystoreSecret() (*reconcile.Result, error) {
	i := s.infinispan
	if !i.IsEncryptionEnabled() {
		return nil, nil
	}

	keystoreSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(i.GetKeystoreSecretName(), i.Namespace, keystoreSecret, i, s.client, s.log, s.eventRec); result != nil {
		return result, err
	}

	_, err := controllerutil.CreateOrUpdate(context.TODO(), s.client, keystoreSecret, func() error {
		return s.setControllerRef(keystoreSecret)
	})
	if err != nil {
		return &reconcile.Result{}, err
	}
	return nil, nil
}

func (s secretResource) reconcileTruststoreSecret() (*reconcile.Result, error) {
	i := s.infinispan
	if !i.IsClientCertEnabled() {
		return nil, nil
	}

	trustSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetTruststoreSecretName(),
			Namespace: i.Namespace,
		},
	}

	if trustSecret.Name == "" {
		// It's not possible for client cert to be configured if no truststore secret is provided by the user
		// as no certificates are available to be added to the server's truststore, resulting in no client's
		// being able to authenticate with the server.
		return &reconcile.Result{}, fmt.Errorf("Field 'clientCertSecretName' must be provided for '%s' or '%s' to be configured",
			ispnv1.ClientCertAuthenticate, ispnv1.ClientCertValidate)
	}

	if result, err := kube.LookupResource(trustSecret.Name, i.Namespace, trustSecret, i, s.client, s.log, s.eventRec); result != nil {
		return result, err
	}

	_, err := kube.CreateOrPatch(ctx, s.client, trustSecret, func() error {
		if trustSecret.CreationTimestamp.IsZero() {
			return errors.NewNotFound(corev1.Resource("secret"), i.GetTruststoreSecretName())
		}

		_, truststoreExists := trustSecret.Data[consts.EncryptTruststoreKey]
		if truststoreExists {
			if _, ok := trustSecret.Data[consts.EncryptTruststorePasswordKey]; !ok {
				return fmt.Errorf("The '%s' key must be provided when configuring an existing Truststore", consts.EncryptTruststorePasswordKey)
			}
		} else {
			caPem := trustSecret.Data[EncryptClientCAName]
			if trustSecret.Data == nil {
				trustSecret.Data = map[string][]byte{
					consts.EncryptTruststorePasswordKey: []byte(EncryptTruststorePassword),
				}
			} else {
				if _, passwordProvided := trustSecret.Data[consts.EncryptTruststorePasswordKey]; !passwordProvided {
					trustSecret.Data[consts.EncryptTruststorePasswordKey] = []byte(EncryptTruststorePassword)
				}
			}

			certs := [][]byte{caPem}
			for certKey := range trustSecret.Data {
				if strings.HasPrefix(certKey, EncryptClientCertPrefix) {
					certs = append(certs, trustSecret.Data[certKey])
				}
			}
			password := string(trustSecret.Data[consts.EncryptTruststorePasswordKey])
			truststore, err := security.GenerateTruststore(certs, password)
			if err != nil {
				return err
			}
			trustSecret.Data[consts.EncryptTruststoreKey] = truststore
			return nil
		}
		return s.setControllerRef(trustSecret)
	})
	if err != nil {
		return &reconcile.Result{}, err
	}
	return nil, err
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
			identities, err := security.GetAdminCredentials()
			if err != nil {
				return err
			}
			if adminSecret.Data == nil {
				adminSecret.Data = map[string][]byte{}
			}
			adminSecret.Labels = infinispan.LabelsResource(s.infinispan.Name, "infinispan-secret-admin-identities")
			adminSecret.Data[consts.ServerIdentitiesFilename] = identities
			if err = s.setControllerRef(adminSecret); err != nil {
				return err
			}
		}
		pass, ok := adminSecret.Data[consts.AdminPasswordKey]
		password := string(pass)
		if !ok || password == "" {
			var usrErr error
			if password, usrErr = security.FindPassword(consts.DefaultOperatorUser, adminSecret.Data[consts.ServerIdentitiesFilename]); usrErr != nil {
				return usrErr
			}
		}
		identities, err := security.CreateIdentitiesFor(consts.DefaultOperatorUser, password)
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

func (s secretResource) setControllerRef(secret *corev1.Secret) error {
	if ownerRef := metav1.GetControllerOf(secret); ownerRef != nil && ownerRef.UID != s.infinispan.UID {
		return fmt.Errorf("Unable to update Secret %s's ownerReference with controller=true as it's already owned by %s:%s:%s",
			secret.Name, ownerRef.Kind, ownerRef.Name, ownerRef.UID)
	}
	return controllerutil.SetControllerReference(s.infinispan, secret, s.scheme)
}
