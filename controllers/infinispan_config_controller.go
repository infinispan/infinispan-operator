package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/prometheus/common/log"
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

	var xsite *config.XSite
	if infinispan.HasSites() {
		// Check x-site configuration first.
		// Must be done before creating any Infinispan resources,
		// because remote site host:port combinations need to be injected into Infinispan.

		// For cross site, reconcile must come before compute, because
		// we need xsite service details to compute xsite struct
		siteService := &corev1.Service{}
		if result, err := kube.LookupResource(infinispan.GetSiteServiceName(), infinispan.Namespace, siteService, infinispan, r.Client, reqLogger, r.eventRec, r.ctx); result != nil {
			return *result, err
		}

		var err error
		xsite, err = ComputeXSite(infinispan, r.kubernetes, siteService, reqLogger, r.eventRec, r.ctx)
		if err != nil {
			reqLogger.Error(err, "Error in computeXSite configuration")
			return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCreateResource}, nil
		}
	}

	result, err := r.computeAndReconcileConfigMap(xsite)
	if result != nil {
		if err != nil {
			reqLogger.Error(err, "Error while computing and reconciling ConfigMap")
		}
		return *result, err
	}

	return reconcile.Result{}, nil
}

// computeAndReconcileConfigMap computes, creates or updates the ConfigMap for the Infinispan
func (r configRequest) computeAndReconcileConfigMap(xsite *config.XSite) (*reconcile.Result, error) {
	name := r.infinispan.Name
	namespace := r.infinispan.Namespace

	lsConfigMap := LabelsResource(name, "infinispan-configmap-configuration")

	var roleMapper string
	if r.infinispan.IsClientCertEnabled() && r.infinispan.Spec.Security.EndpointEncryption.ClientCert == v1.ClientCertAuthenticate {
		roleMapper = "commonName"
	} else {
		roleMapper = "cluster"
	}
	serverConf := config.InfinispanConfiguration{
		Infinispan: config.Infinispan{
			Authorization: config.Authorization{
				Enabled:    r.infinispan.IsAuthorizationEnabled(),
				RoleMapper: roleMapper,
			},
			ClusterName: name,
			Locks: config.Locks{
				Owners:      -1,
				Reliability: "consistent",
			},
		},
		JGroups: config.JGroups{
			Transport:   "tcp",
			BindPort:    7800,
			Diagnostics: consts.JGroupsDiagnosticsFlag == "TRUE",
			DNSPing: config.DNSPing{
				RecordType: "A",
				Query:      fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace),
			},
			FastMerge: consts.JGroupsFastMerge,
		},
		Keystore: config.Keystore{
			Password: "password",
			Alias:    "server",
			Type:     "pkcs12",
		},
		XSite: &config.XSite{
			RelayNodeCandidate: true,
			MaxRelayNodes:      1,
			Transport:          "tunnel",
			Relay: config.Relay{
				BindPort: 0,
			},
		},
		Logging: config.Logging{
			Console: config.Console{
				Level:   "trace",
				Pattern: "%d{HH:mm:ss,SSS} %-5p (%t) [%c] %m%throwable%n",
			},
			File: config.File{
				Level:   "trace",
				Pattern: "%d{yyyy-MM-dd HH:mm:ss,SSS} %-5p (%t) [%c] %m%throwable%n",
				Path:    "${sys:infinispan.server.log.path}/server.log",
			},
			Categories: r.infinispan.GetLogCategoriesForConfig(),
		},
		Endpoints: config.Endpoints{
			Authenticate:   r.infinispan.IsAuthenticationEnabled(),
			ClientCert:     "none",
			DedicatedAdmin: true,
			Hotrod: config.Endpoint{
				Enabled:    true,
				Qop:        "auth",
				ServerName: "infinispan",
			},
			Enabled: true,
		},
		CloudEvents: &config.CloudEvents{},
		Transport: config.Transport{
			TLS: config.TransportTLS{
				Enabled: r.infinispan.IsSiteTLSEnabled(),
			},
		},
	}

	// Apply settings for authentication and roles
	specRoles := r.infinispan.GetAuthorizationRoles()
	if len(specRoles) > 0 {
		confRoles := make([]config.AuthorizationRole, len(specRoles))
		for i, role := range specRoles {
			confRoles[i] = config.AuthorizationRole(role)
		}
		serverConf.Infinispan.Authorization.Roles = confRoles
	}

	// Apply settings for cross site
	if xsite != nil {
		serverConf.XSite = xsite
	}
	if result, err := ConfigureServerEncryption(r.infinispan, &serverConf, r.Client, r.reqLogger, r.eventRec, r.ctx); result != nil {
		return result, err
	}
	r.configureCloudEvent(&serverConf)

	configMapObject := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.infinispan.GetConfigName(),
			Namespace: namespace,
		},
	}

	if r.infinispan.IsSiteTLSEnabled() {
		if result, err := r.configureXSiteTransportTLS(&serverConf); result != nil || err != nil {
			return result, err
		}
	}

	result, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, configMapObject, func() error {
		configYaml, err := serverConf.Yaml()
		if err != nil {
			return err
		}

		ispnXml, log4jXml, err := serverConf.Xml()
		if err != nil {
			return err
		}

		if configMapObject.CreationTimestamp.IsZero() {
			configMapObject.Data = map[string]string{consts.ServerConfigFilename: configYaml}
			configMapObject.Data["infinispan.xml"] = ispnXml
			configMapObject.Data["log4j.xml"] = log4jXml
			configMapObject.Labels = lsConfigMap
			// Set Infinispan instance as the owner and controller
			if err = controllerutil.SetControllerReference(r.infinispan, configMapObject, r.scheme); err != nil {
				return err
			}
		} else {
			previousConfig, err := config.FromYaml(configMapObject.Data[consts.ServerConfigFilename])
			if err == nil {
				// Protecting Logging configuration from changes
				serverConf.Logging = previousConfig.Logging
			}
			configMapObject.Data[consts.ServerConfigFilename] = configYaml
			configMapObject.Data["infinispan.xml"] = ispnXml
			configMapObject.Data["log4j.xml"] = log4jXml
		}
		return nil
	})
	if err != nil {
		return &reconcile.Result{}, err
	}
	if result != controllerutil.OperationResultNone {
		r.reqLogger.Info(fmt.Sprintf("ConfigMap '%s' %s", name, result))
	}
	return nil, err
}

func (r configRequest) configureCloudEvent(c *config.InfinispanConfiguration) {
	spec := r.infinispan.Spec
	if spec.CloudEvents != nil {
		c.CloudEvents = &config.CloudEvents{}
		c.CloudEvents.Acks = spec.CloudEvents.Acks
		c.CloudEvents.BootstrapServers = spec.CloudEvents.BootstrapServers
		c.CloudEvents.CacheEntriesTopic = spec.CloudEvents.CacheEntriesTopic
	}
}

func ConfigureServerEncryption(i *v1.Infinispan, c *config.InfinispanConfiguration, client client.Client, log logr.Logger,
	eventRec record.EventRecorder, ctx context.Context) (*reconcile.Result, error) {
	if !i.IsEncryptionEnabled() {
		return nil, nil
	}

	secretContains := func(secret *corev1.Secret, keys ...string) bool {
		for _, k := range keys {
			if _, ok := secret.Data[k]; !ok {
				return false
			}
		}
		return true
	}

	configureNewKeystore := func(c *config.InfinispanConfiguration) {
		c.Keystore.CrtPath = consts.ServerEncryptKeystoreRoot
		c.Keystore.Path = consts.ServerOperatorSecurity + "/" + EncryptPemKeystoreName
		c.Keystore.Password = ""
		c.Keystore.Alias = ""
		c.Keystore.Type = "pem"
	}

	// Configure Keystore
	keystoreSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(i.GetKeystoreSecretName(), i.Namespace, keystoreSecret, i, client, log, eventRec, ctx); result != nil {
		return result, err
	}
	if i.IsEncryptionCertFromService() {
		if strings.Contains(i.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			configureNewKeystore(c)
		}
	} else {
		if secretContains(keystoreSecret, EncryptPkcs12KeystoreName) {
			// If user provide a keystore in secret then use it ...
			c.Keystore.Path = fmt.Sprintf("%s/%s", consts.ServerEncryptKeystoreRoot, EncryptPkcs12KeystoreName)
			c.Keystore.Password = string(keystoreSecret.Data["password"])
			c.Keystore.Alias = string(keystoreSecret.Data["alias"])
		} else if secretContains(keystoreSecret, corev1.TLSPrivateKeyKey, corev1.TLSCertKey) {
			configureNewKeystore(c)
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

		if userPass, ok := trustSecret.Data[consts.EncryptTruststorePasswordKey]; ok {
			c.Truststore.Password = string(userPass)
		} else {
			c.Truststore.Password = "password"
		}
	}
	return nil, nil
}

// configureXSiteTransportTLS configures the keystore and truststore paths and password in Infinispan server for TLS cross-site communication
func (r configRequest) configureXSiteTransportTLS(c *config.InfinispanConfiguration) (*reconcile.Result, error) {
	keyStoreSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(r.infinispan.GetSiteTransportSecretName(), r.infinispan.Namespace, keyStoreSecret, r.infinispan, r.Client, r.reqLogger, r.eventRec, r.ctx); result != nil || err != nil {
		return result, err
	}

	keyStoreFileName := r.infinispan.GetSiteTransportKeyStoreFileName()
	password := string(keyStoreSecret.Data["password"])
	alias := r.infinispan.GetSiteTransportKeyStoreAlias()

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

	trustStoreSecret, err := FindSiteTrustStoreSecret(r.infinispan, r.Client, r.ctx)
	if err != nil || trustStoreSecret == nil {
		return nil, err
	}
	trustStoreFileName := r.infinispan.GetSiteTrustStoreFileName()
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
