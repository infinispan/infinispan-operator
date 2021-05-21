package config

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/eventlog"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
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
	infinispan    *ispnv1.Infinispan
	client        client.Client
	scheme        *runtime.Scheme
	kube          *kube.Kubernetes
	log           logr.Logger
	eventRecorder record.EventRecorder
}

func (config *configResource) Logger() *logr.Logger {
	return &config.log
}

func (config *configResource) EventRecorder() *record.EventRecorder {
	return &config.eventRecorder
}

func (config *configResource) Client() *client.Client {
	return &config.client
}

func (config *configResource) Name() string {
	return ControllerName
}

func (r reconcileConfig) ResourceInstance(infinispan *ispnv1.Infinispan, ctrl *resources.Controller, kube *kube.Kubernetes, log logr.Logger) resources.Resource {
	return &configResource{
		infinispan: infinispan,
		client:     r.Client,
		scheme:     ctrl.Scheme,
		kube:       kube,
		log:        log,
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
	xsite := &configuration.XSite{}
	if c.infinispan.HasSites() {
		// Check x-site configuration first.
		// Must be done before creating any Infinispan resources,
		// because remote site host:port combinations need to be injected into Infinispan.

		// For cross site, reconcile must come before compute, because
		// we need xsite service details to compute xsite struct
		siteService := &corev1.Service{}
		if result, err := kube.LookupResource(c.infinispan.GetSiteServiceName(), c.infinispan.Namespace, siteService, c); result != nil {
			return *result, err
		}

		var err error
		xsite, err = ComputeXSite(c.infinispan, c.kube, siteService, c.log)
		if err != nil {
			if errEv, ok := err.(*eventlog.ErrorEvent); ok {
				eventlog.LogAndSendEvent(c, siteService, errEv.Error(), errEv.Reason)
			} else {
				c.log.Error(err, "Error in computeXSite configuration")
			}
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
	loggingCategories := c.infinispan.GetLogCategoriesForConfig()

	authenticate := c.infinispan.IsAuthenticationEnabled()
	config := configuration.CreateInfinispanConfiguration(name, namespace, authenticate, loggingCategories, xsite)

	err := infinispan.ConfigureServerEncryption(c.infinispan, &config, c.client)
	if err != nil {
		return err
	}

	configureCloudEvent(c.infinispan, &config)

	configMapObject := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      c.infinispan.GetConfigName(),
			Namespace: namespace,
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, c.client, configMapObject, func() error {
		if configMapObject.CreationTimestamp.IsZero() {
			configYaml, err := config.Yaml()
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
				config.Logging = previousConfig.Logging
			}
			configYaml, err := config.Yaml()
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
