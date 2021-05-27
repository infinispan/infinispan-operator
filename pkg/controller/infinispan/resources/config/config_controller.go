package config

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ControllerName = "config-controller"
)

var ctx = context.Background()

// reconcileConfig reconciles a ConfigMap object
type reconcileConfig struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
}

type configResource struct {
	infinispan *ispnv1.Infinispan
	client     client.Client
	scheme     *runtime.Scheme
	kube       *kube.Kubernetes
	log        logr.Logger
	eventRec   record.EventRecorder
}

func (r reconcileConfig) ResourceInstance(infinispan *ispnv1.Infinispan, ctrl *resources.Controller, kube *kube.Kubernetes, log logr.Logger) resources.Resource {
	return &configResource{
		infinispan: infinispan,
		client:     r.Client,
		scheme:     ctrl.Scheme,
		kube:       kube,
		log:        log,
		eventRec:   ctrl.EventRec,
	}
}

func (r reconcileConfig) Types() map[string]*resources.ReconcileType {
	return map[string]*resources.ReconcileType{"ConfigMap": {ObjectType: &corev1.ConfigMap{}, GroupVersion: corev1.SchemeGroupVersion, GroupVersionSupported: true}}
}

func (r reconcileConfig) EventsPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
	}
}

func Add(mgr manager.Manager) error {
	return resources.CreateController(ControllerName, &reconcileConfig{mgr.GetClient()}, mgr)
}

func (c *configResource) Process() (reconcile.Result, error) {
	var xsite *configuration.XSite
	if c.infinispan.HasSites() {
		// Check x-site configuration first.
		// Must be done before creating any Infinispan resources,
		// because remote site host:port combinations need to be injected into Infinispan.

		// For cross site, reconcile must come before compute, because
		// we need xsite service details to compute xsite struct
		siteService := &corev1.Service{}
		if result, err := kube.LookupResource(c.infinispan.GetSiteServiceName(), c.infinispan.Namespace, siteService, c.client, c.log, c.eventRec); result != nil {
			return *result, err
		}

		var err error
		xsite, err = ComputeXSite(c.infinispan, c.kube, siteService, c.log, c.eventRec)
		if err != nil {
			c.log.Error(err, "Error in computeXSite configuration")
			return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCreateResource}, nil
		}
	}

	err := c.computeAndReconcileConfigMap(xsite)
	if err != nil {
		c.log.Error(err, "Error while computing and reconciling ConfigMap")
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, nil
}

// computeAndReconcileConfigMap computes, creates or updates the ConfigMap for the Infinispan
func (c configResource) computeAndReconcileConfigMap(xsite *configuration.XSite) error {
	name := c.infinispan.Name
	namespace := c.infinispan.Namespace

	lsConfigMap := infinispan.LabelsResource(name, "infinispan-configmap-configuration")

	jgroupsDiagnostics := consts.JGroupsDiagnosticsFlag == "TRUE"
	serverConf := config.InfinispanConfiguration{
		Infinispan: config.Infinispan{
			Authorization: config.Authorization{
				Enabled:    c.infinispan.IsAuthorizationEnabled(),
				RoleMapper: "cluster",
			},
			ClusterName: name,
		},
		JGroups: config.JGroups{
			Transport: "tcp",
			DNSPing: config.DNSPing{
				Query: fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace),
			},
			Diagnostics: jgroupsDiagnostics,
		},
		Endpoints: config.Endpoints{
			Authenticate:   c.infinispan.IsAuthenticationEnabled(),
			DedicatedAdmin: true,
		},
		Logging: config.Logging{
			Categories: c.infinispan.GetLogCategoriesForConfig(),
		},
	}

	specRoles := c.infinispan.GetAuthorizationRoles()
	if len(specRoles) > 0 {
		confRoles := make([]config.AuthorizationRole, len(specRoles))
		for i, role := range specRoles {
			confRoles[i] = config.AuthorizationRole(role)
		}
		serverConf.Infinispan.Authorization.Roles = confRoles
	}

	if xsite != nil {
		serverConf.XSite = xsite
	}

	err := infinispan.ConfigureServerEncryption(c.infinispan, &serverConf, c.client)
	if err != nil {
		return err
	}

	configureCloudEvent(c.infinispan, &serverConf)

	configMapObject := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.infinispan.GetConfigName(),
			Namespace: namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, c.client, configMapObject, func() error {
		if configMapObject.CreationTimestamp.IsZero() {
			configYaml, err := serverConf.Yaml()
			if err != nil {
				return err
			}
			configMapObject.Data = map[string]string{consts.ServerConfigFilename: configYaml}
			configMapObject.Labels = lsConfigMap
			// Set Infinispan instance as the owner and controller
			if err = controllerutil.SetControllerReference(c.infinispan, configMapObject, c.scheme); err != nil {
				return err
			}
		} else {
			previousConfig, err := configuration.FromYaml(configMapObject.Data[consts.ServerConfigFilename])
			if err == nil {
				// Protecting Logging configuration from changes
				serverConf.Logging = previousConfig.Logging
			}
			configYaml, err := serverConf.Yaml()
			if err != nil {
				return err
			}
			configMapObject.Data[consts.ServerConfigFilename] = configYaml
		}
		return nil
	})
	if err == nil && result != controllerutil.OperationResultNone {
		c.log.Info(fmt.Sprintf("ConfigMap '%s' %s", name, result))
	}
	return err
}

func configureCloudEvent(m *ispnv1.Infinispan, c *configuration.InfinispanConfiguration) {
	if m.Spec.CloudEvents != nil {
		c.CloudEvents = &configuration.CloudEvents{}
		c.CloudEvents.Acks = m.Spec.CloudEvents.Acks
		c.CloudEvents.BootstrapServers = m.Spec.CloudEvents.BootstrapServers
		c.CloudEvents.CacheEntriesTopic = m.Spec.CloudEvents.CacheEntriesTopic
	}
}
