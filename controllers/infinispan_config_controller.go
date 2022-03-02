package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/logging"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/server"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	EncryptPkcs12KeystoreName = "keystore.p12"
	EncryptPemKeystoreName    = "keystore.pem"
)

// ConfigReconciler reconciles a ConfigMap object
type ConfigReconciler struct {
	client.Client
	scheme     *runtime.Scheme
	log        logr.Logger
	kubernetes *kube.Kubernetes
	eventRec   record.EventRecorder
}

// Struct for wrapping reconcile request data
type configRequest struct {
	*ConfigReconciler
	infinispan *v1.Infinispan
	reqLogger  logr.Logger
	ctx        context.Context
}

func (r *ConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "configmap"
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName(strings.Title(name))
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor(name + "-controller")

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&v1.Infinispan{}).
		Owns(&corev1.ConfigMap{}).
		WithEventFilter(predicate.Funcs{
			DeleteFunc: func(e event.DeleteEvent) bool {
				switch e.Object.(type) {
				case *v1.Infinispan:
					return false
				}
				return true
			},
		}).
		Complete(r)
}

func (reconciler *ConfigReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := reconciler.log.WithValues("Reconciling", "ConfigMap", "Request.Namespace", request.Namespace, "Request.Name", request.Name)
	infinispan := &v1.Infinispan{}
	if err := reconciler.Get(ctx, request.NamespacedName, infinispan); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Infinispan CR not found")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("unable to fetch Infinispan CR %w", err)
	}

	// Don't reconcile Infinispan CRs marked for deletion
	if infinispan.GetDeletionTimestamp() != nil {
		reqLogger.Info(fmt.Sprintf("Ignoring Infinispan CR '%s:%s' marked for deletion", infinispan.Namespace, infinispan.Name))
		return reconcile.Result{}, nil
	}

	// Validate that Infinispan CR passed all preliminary checks
	if !infinispan.IsConditionTrue(v1.ConditionPrelimChecksPassed) {
		reqLogger.Info("Infinispan CR not ready")
		return reconcile.Result{}, nil
	}

	r := &configRequest{
		ConfigReconciler: reconciler,
		infinispan:       infinispan,
		reqLogger:        reqLogger,
		ctx:              ctx,
	}

	// Don't update the ConfigMap if an update is about to be scheduled
	if req, err := IsUpgradeRequired(infinispan, r.kubernetes, ctx); req || err != nil {
		reqLogger.Info("Postponing reconciliation. Infinispan upgrade required")
		return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCreateResource}, nil
	}

	// Don't update the configMap if an update is in progress
	if infinispan.IsConditionTrue(v1.ConditionUpgrade) {
		reqLogger.Info("Postponing reconciliation. Infinispan upgrade in progress")
		return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCreateResource}, nil
	}

	serverConfig, result, err := GenerateServerConfig(infinispan.GetStatefulSetName(), infinispan, r.kubernetes, r.Client, r.log, r.eventRec, r.ctx)
	if result != nil {
		return *result, err
	}

	loggingSpec := &logging.Spec{
		Categories: infinispan.GetLogCategoriesForConfig(),
	}

	// TODO utilise a version specific logging once server/operator versions decoupled
	log4jXml, err := logging.Generate(nil, loggingSpec)
	if err != nil {
		return reconcile.Result{}, err
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      infinispan.GetConfigName(),
			Namespace: infinispan.Namespace,
		},
	}

	createOrUpdate, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		if configMap.CreationTimestamp.IsZero() {
			if err = controllerutil.SetControllerReference(r.infinispan, configMap, r.scheme); err != nil {
				return err
			}
		}
		InitServerConfigMap(configMap, infinispan, serverConfig, log4jXml)
		return nil
	})
	if err != nil {
		reqLogger.Error(err, "Error while computing and reconciling ConfigMap")
		return reconcile.Result{}, err
	}
	if createOrUpdate != controllerutil.OperationResultNone {
		r.reqLogger.Info(fmt.Sprintf("ConfigMap '%s' %s", configMap.Name, createOrUpdate))
	}
	return reconcile.Result{}, nil
}

func InitServerConfigMap(configMapObject *corev1.ConfigMap, i *v1.Infinispan, serverConfig, log4jXml string) {
	configMapObject.Data = map[string]string{
		"infinispan.xml": serverConfig,
		"log4j.xml":      log4jXml,
	}
	configMapObject.Data["infinispan.xml"] = serverConfig
	configMapObject.Data["log4j.xml"] = log4jXml
	configMapObject.Labels = LabelsResource(i.Name, "infinispan-configmap-configuration")
}

func GenerateServerConfig(statefulSet string, i *v1.Infinispan, kubernetes *kube.Kubernetes, c client.Client, log logr.Logger,
	eventRec record.EventRecorder, ctx context.Context) (string, *reconcile.Result, error) {
	var xsite *config.XSite
	if i.HasSites() {
		// Check x-site configuration first.
		// Must be done before creating any Infinispan resources,
		// because remote site host:port combinations need to be injected into Infinispan.

		// For cross site, reconcile must come before compute, because
		// we need xsite service details to compute xsite struct
		siteService := &corev1.Service{}
		svcName := i.GetSiteServiceName()
		if result, err := kube.LookupResource(svcName, i.Namespace, siteService, i, c, log, eventRec, ctx); result != nil {
			return "", result, err
		}

		var err error
		xsite, err = ComputeXSite(i, kubernetes, siteService, log, eventRec, ctx)
		if err != nil {
			log.Error(err, "Error in computeXSite configuration")
			return "", &reconcile.Result{RequeueAfter: consts.DefaultWaitOnCreateResource}, nil
		}
	}

	var roleMapper string
	if i.IsClientCertEnabled() && i.Spec.Security.EndpointEncryption.ClientCert == v1.ClientCertAuthenticate {
		roleMapper = "commonName"
	} else {
		roleMapper = "cluster"
	}

	configSpec := &config.Spec{
		ClusterName:     i.Name,
		Namespace:       i.Namespace,
		StatefulSetName: statefulSet,
		Infinispan: config.Infinispan{
			Authorization: &config.Authorization{
				Enabled:    i.IsAuthorizationEnabled(),
				RoleMapper: roleMapper,
			},
		},
		JGroups: config.JGroups{
			Diagnostics: consts.JGroupsDiagnosticsFlag == "TRUE",
			FastMerge:   consts.JGroupsFastMerge,
		},
		Endpoints: config.Endpoints{
			Authenticate: i.IsAuthenticationEnabled(),
			ClientCert:   string(v1.ClientCertNone),
		},
		XSite: xsite,
	}

	// Apply settings for authentication and roles
	specRoles := i.GetAuthorizationRoles()
	if len(specRoles) > 0 {
		confRoles := make([]config.AuthorizationRole, len(specRoles))
		for i, role := range specRoles {
			confRoles[i] = config.AuthorizationRole{
				Name:        role.Name,
				Permissions: strings.Join(role.Permissions, ","),
			}
		}
		configSpec.Infinispan.Authorization.Roles = confRoles
	}

	if i.Spec.CloudEvents != nil {
		configSpec.CloudEvents = &config.CloudEvents{
			Acks:              i.Spec.CloudEvents.Acks,
			BootstrapServers:  i.Spec.CloudEvents.BootstrapServers,
			CacheEntriesTopic: i.Spec.CloudEvents.CacheEntriesTopic,
		}
	}

	if result, err := ConfigureServerEncryption(i, configSpec, c, log, eventRec, ctx); result != nil {
		return "", result, err
	}

	if i.IsSiteTLSEnabled() {
		if result, err := configureXSiteTransportTLS(i, configSpec, c, log, eventRec, ctx); result != nil || err != nil {
			return "", result, err
		}
	}

	// TODO utilise a version specific configurator once server/operator versions decoupled
	config, err := config.Generate(nil, configSpec)
	if err != nil {
		return "", &reconcile.Result{}, err
	}
	return config, nil, nil
}

func IsUserProvidedKeystore(secret *corev1.Secret) bool {
	_, userKeystore := secret.Data["keystore.p12"]
	return userKeystore
}

func IsUserProvidedPrivateKey(secret *corev1.Secret) bool {
	for _, k := range []string{corev1.TLSPrivateKeyKey, corev1.TLSCertKey} {
		if _, ok := secret.Data[k]; !ok {
			return false
		}
	}
	return true
}

func ConfigureServerEncryption(i *v1.Infinispan, c *config.Spec, client client.Client, log logr.Logger,
	eventRec record.EventRecorder, ctx context.Context) (*reconcile.Result, error) {

	if !i.IsEncryptionEnabled() {
		return nil, nil
	}

	// Configure Keystore
	keystoreSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(i.GetKeystoreSecretName(), i.Namespace, keystoreSecret, i, client, log, eventRec, ctx); result != nil {
		return result, err
	}

	if i.IsEncryptionCertFromService() {
		if strings.Contains(i.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			c.Keystore.CrtPath = consts.ServerEncryptKeystoreRoot
			c.Keystore.Path = consts.ServerOperatorSecurity + "/" + EncryptPemKeystoreName
		}
	} else {
		if IsUserProvidedKeystore(keystoreSecret) {
			// If user provide a keystore in secret then use it ...
			c.Keystore.Path = fmt.Sprintf("%s/%s", consts.ServerEncryptKeystoreRoot, EncryptPkcs12KeystoreName)
			c.Keystore.Alias = string(keystoreSecret.Data["alias"])
			// Actual value is not used by template, but required to show that a credential ref is required
			c.Keystore.Password = string(keystoreSecret.Data["password"])
		} else if IsUserProvidedPrivateKey(keystoreSecret) {
			c.Keystore.CrtPath = consts.ServerEncryptKeystoreRoot
			c.Keystore.Path = consts.ServerOperatorSecurity + "/" + EncryptPemKeystoreName
		}
	}

	// Configure Truststore
	if i.IsClientCertEnabled() {
		trustSecret := &corev1.Secret{}
		if result, err := kube.LookupResource(i.GetTruststoreSecretName(), i.Namespace, trustSecret, i, client, log, eventRec, ctx); result != nil {
			return result, err
		}

		c.Endpoints.ClientCert = string(i.Spec.Security.EndpointEncryption.ClientCert)
		c.Truststore.Path = fmt.Sprintf("%s/%s", consts.ServerEncryptTruststoreRoot, consts.EncryptTruststoreKey)
	}
	return nil, nil
}

func configureXSiteTransportTLS(i *v1.Infinispan, c *config.Spec, client client.Client, log logr.Logger,
	eventRec record.EventRecorder, ctx context.Context) (*reconcile.Result, error) {
	keyStoreSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(i.GetSiteTransportSecretName(), i.Namespace, keyStoreSecret, i, client, log, eventRec, ctx); result != nil || err != nil {
		return result, err
	}

	keyStoreFileName := i.GetSiteTransportKeyStoreFileName()
	password := string(keyStoreSecret.Data["password"])
	alias := i.GetSiteTransportKeyStoreAlias()

	if err := ValidaXSiteTLSKeyStore(keyStoreSecret.Name, keyStoreFileName, password, alias); err != nil {
		return nil, err
	}

	log.Info("Transport TLS Configured.", "Keystore", keyStoreFileName, "Secret Name", keyStoreSecret.Name)
	c.Transport.TLS.Enabled = true
	c.Transport.TLS.KeyStore = config.Keystore{
		Path:     fmt.Sprintf("%s/%s", consts.SiteTransportKeyStoreRoot, keyStoreFileName),
		Password: password,
		Alias:    alias,
	}

	trustStoreSecret, err := FindSiteTrustStoreSecret(i, client, ctx)
	if err != nil || trustStoreSecret == nil {
		return nil, err
	}
	trustStoreFileName := i.GetSiteTrustStoreFileName()
	password = string(trustStoreSecret.Data["password"])

	if err := ValidaXSiteTLSTrustStore(trustStoreSecret.Name, trustStoreFileName, password); err != nil {
		return nil, err
	}

	log.Info("Found Truststore.", "Truststore", trustStoreFileName, "Secret Name", trustStoreSecret.ObjectMeta.Name)
	c.Transport.TLS.TrustStore = config.Truststore{
		Path:     fmt.Sprintf("%s/%s", consts.SiteTrustStoreRoot, trustStoreFileName),
		Password: password,
	}
	return nil, nil
}
