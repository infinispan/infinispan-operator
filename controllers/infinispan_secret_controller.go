package controllers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	EncryptClientCertPrefix   = "trust.cert."
	EncryptClientCAName       = "trust.ca"
	EncryptTruststorePassword = "password"
)

// ReconcileSecret reconciles a Secret object
type SecretReconciler struct {
	client.Client
	log        logr.Logger
	scheme     *runtime.Scheme
	kubernetes *kube.Kubernetes
	eventRec   record.EventRecorder
}

// Struct for wrapping reconcile request data
type secretRequest struct {
	*SecretReconciler
	infinispan *ispnv1.Infinispan
	reqLogger  logr.Logger
	ctx        context.Context
}

func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "secret"
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName(strings.Title(name))
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor(name + "-controller")

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&ispnv1.Infinispan{}).
		Owns(&corev1.Secret{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				switch e.Object.(type) {
				case *corev1.Secret:
					return false
				}
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				switch e.Object.(type) {
				case *ispnv1.Infinispan:
					return false
				}
				return true
			},
		}).
		Complete(r)
}

func (reconciler *SecretReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := reconciler.log.WithValues("Reconciling", "Secret", "Request.Namespace", request.Namespace, "Request.Name", request.Name)
	infinispan := &ispnv1.Infinispan{}
	if err := reconciler.Get(ctx, request.NamespacedName, infinispan); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Infinispan CR not found")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("unable to fetch Infinispan CR %w", err)
	}

	// Validate that Infinispan CR passed all preliminary checks
	if !infinispan.IsConditionTrue(ispnv1.ConditionPrelimChecksPassed) {
		reqLogger.Info("Infinispan CR not ready")
		return reconcile.Result{}, nil
	}

	r := &secretRequest{
		SecretReconciler: reconciler,
		infinispan:       infinispan,
		reqLogger:        reqLogger,
		ctx:              ctx,
	}

	// Reconcile Encryption Secrets
	if result, err := r.reconcileTruststoreSecret(); result != nil {
		return *result, err
	}

	var userCredSecret *corev1.Secret

	// If auth is enable
	if r.infinispan.IsAuthenticationEnabled() {
		var err error
		// get identities secret for users
		if userCredSecret, err = r.getSecret(r.infinispan.GetSecretName()); err != nil {
			return reconcile.Result{}, err
		}
		// or create the users identities secret if it doesn't already exist
		if userCredSecret == nil && r.infinispan.IsGeneratedSecret() {
			if userCredSecret, err = r.createUserIdentitiesSecret(); err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	// Reconcile Credential Secrets
	adminCredSecret, err := r.reconcileAdminSecret()
	if err != nil {
		return reconcile.Result{}, err
	}

	if result, err := r.computeAndReconcileAuthProps(userCredSecret, adminCredSecret, reqLogger); result != nil {
		return *result, err
	}

	return reconcile.Result{}, nil
}

func (r secretRequest) computeAndReconcileAuthProps(userPropSecret, adminPropSecret *corev1.Secret, reqLogger logr.Logger) (*reconcile.Result, error) {

	// Create admin and user identity properties from secrets
	adminCliBatch, err := security.IdentitiesCliFileFromSecret(adminPropSecret.Data[consts.ServerIdentitiesFilename], "admin", "cli-admin-users.properties", "cli-admin-groups.properties")
	if err != nil {
		return &reconcile.Result{}, err
	}

	var usersCliBatch string
	if userPropSecret != nil {
		if usersCliBatch, err = security.IdentitiesCliFileFromSecret(userPropSecret.Data[consts.ServerIdentitiesFilename], "default", "cli-users.properties", "cli-groups.properties"); err != nil {
			return &reconcile.Result{}, err
		}
	}
	cliBatch := adminCliBatch + usersCliBatch

	var pem []byte
	if r.infinispan.IsEncryptionEnabled() {
		keystoreSecret := &corev1.Secret{}
		if result, err := kube.LookupResource(r.infinispan.GetKeystoreSecretName(), r.infinispan.Namespace, keystoreSecret, r.infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
			return &reconcile.Result{}, err
		}

		// Add the keystore credential if the user has provided their own keystore
		if IsUserProvidedKeystore(keystoreSecret) {
			if password, ok := keystoreSecret.Data["password"]; ok {
				cliBatch += fmt.Sprintf("credentials add keystore -c \"%s\" -p secret\n", string(password))
			}
		} else {
			pem = append(keystoreSecret.Data["tls.key"], keystoreSecret.Data["tls.crt"]...)
		}

		if r.infinispan.IsClientCertEnabled() {
			trustSecret := &corev1.Secret{}
			if result, err := kube.LookupResource(r.infinispan.GetTruststoreSecretName(), r.infinispan.Namespace, trustSecret, r.infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
				return &reconcile.Result{}, err
			}
			var password string
			if userPass, ok := trustSecret.Data[consts.EncryptTruststorePasswordKey]; ok {
				password = string(userPass)
			} else {
				password = "password"
			}
			cliBatch += fmt.Sprintf("credentials add truststore -c \"%s\" -p secret\n", password)
		}
	}

	infinispanServerSecurityConf := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.infinispan.GetInfinispanSecuritySecretName(),
			Namespace: r.infinispan.Namespace,
		},
	}

	// Create secret with all the objects to be mounted as "/etc/security/conf/operator-security"
	result, err := k8sctrlutil.CreateOrUpdate(r.ctx, r.Client, infinispanServerSecurityConf, func() error {
		infinispanServerSecurityConf.Labels = LabelsResource(r.infinispan.Name, "infinispan-secret-server-security")
		infinispanServerSecurityConf.Data = map[string][]byte{consts.ServerIdentitiesCliFilename: []byte(cliBatch)}
		infinispanServerSecurityConf.Data[EncryptPemKeystoreName] = []byte(pem)
		err = k8sctrlutil.SetControllerReference(r.infinispan, infinispanServerSecurityConf, r.scheme)
		return err
	})
	if err != nil {
		return &reconcile.Result{}, err
	}
	if result != k8sctrlutil.OperationResultNone {
		r.reqLogger.Info(fmt.Sprintf("ConfigMap '%s' %s", r.infinispan.Name, result))
	}
	return nil, nil
}

func (s *secretRequest) createUserIdentitiesSecret() (*corev1.Secret, error) {
	identities, err := security.GetUserCredentials()
	if err != nil {
		return nil, err
	}
	return s.createSecret(s.infinispan.GetSecretName(), "infinispan-secret-identities", identities)
}

func (s *secretRequest) createSecret(name, label string, identities []byte) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.infinispan.Namespace,
			Labels:    LabelsResource(s.infinispan.Name, label),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{consts.ServerIdentitiesFilename: identities},
	}

	s.reqLogger.Info(fmt.Sprintf("Creating Identities Secret %s", secret.Name))
	_, err := k8sctrlutil.CreateOrUpdate(s.ctx, s.Client, secret, func() error {
		return k8sctrlutil.SetControllerReference(s.infinispan, secret, s.scheme)
	})

	if err != nil {
		return nil, fmt.Errorf("unable to create identities secret: %w", err)
	}
	return secret, nil
}

func (s *secretRequest) reconcileTruststoreSecret() (*reconcile.Result, error) {
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
		return &reconcile.Result{}, fmt.Errorf("field 'clientCertSecretName' must be provided for '%s' or '%s' to be configured",
			ispnv1.ClientCertAuthenticate, ispnv1.ClientCertValidate)
	}

	if result, err := kube.LookupResource(trustSecret.Name, i.Namespace, trustSecret, i, s.Client, s.reqLogger, s.eventRec, s.ctx); result != nil {
		return result, err
	}

	_, err := kube.CreateOrPatch(s.ctx, s.Client, trustSecret, func() error {
		if trustSecret.CreationTimestamp.IsZero() {
			return errors.NewNotFound(corev1.Resource("secret"), i.GetTruststoreSecretName())
		}

		_, truststoreExists := trustSecret.Data[consts.EncryptTruststoreKey]
		if truststoreExists {
			if _, ok := trustSecret.Data[consts.EncryptTruststorePasswordKey]; !ok {
				return fmt.Errorf("the '%s' key must be provided when configuring an existing Truststore", consts.EncryptTruststorePasswordKey)
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
		return nil
	})
	if err != nil {
		return &reconcile.Result{}, err
	}
	return nil, err
}

func (s *secretRequest) reconcileAdminSecret() (*corev1.Secret, error) {
	adminSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.infinispan.GetAdminSecretName(),
			Namespace: s.infinispan.Namespace,
		},
	}

	_, err := kube.CreateOrPatch(s.ctx, s.Client, adminSecret, func() error {
		if adminSecret.CreationTimestamp.IsZero() {
			identities, err := security.GetAdminCredentials()
			if err != nil {
				return err
			}
			if adminSecret.Data == nil {
				adminSecret.Data = map[string][]byte{}
			}
			adminSecret.Labels = LabelsResource(s.infinispan.Name, "infinispan-secret-admin-identities")
			adminSecret.Data[consts.ServerIdentitiesFilename] = identities
			if err = k8sctrlutil.SetControllerReference(s.infinispan, adminSecret, s.scheme); err != nil {
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
	if err != nil {
		return nil, err
	}
	return adminSecret, nil
}

func (s *secretRequest) addCliProperties(secret *corev1.Secret, password string) {
	service := s.infinispan.GetAdminServiceName()
	url := fmt.Sprintf("http://%s:%s@%s:%d", consts.DefaultOperatorUser, url.QueryEscape(password), service, consts.InfinispanAdminPort)
	properties := fmt.Sprintf("autoconnect-url=%s", url)
	secret.Data[consts.CliPropertiesFilename] = []byte(properties)
}

func (s *secretRequest) addServiceMonitorProperties(secret *corev1.Secret, password string) {
	secret.Data[consts.AdminPasswordKey] = []byte(password)
	secret.Data[consts.AdminUsernameKey] = []byte(consts.DefaultOperatorUser)
}

func (s *secretRequest) getSecret(name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := s.Client.Get(s.ctx, types.NamespacedName{Namespace: s.infinispan.Namespace, Name: name}, secret)
	if err == nil {
		return secret, nil
	}
	if errors.IsNotFound(err) {
		return nil, nil
	}
	return nil, err
}
