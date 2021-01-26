package operatorconfig

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ControllerName = "operator-config-controller"
	configMapName  = "infinispan-operator-config"
)

const (
	grafanaDashboardNamespaceKey  = "grafana.dashboard.namespace"
	grafanaDashboardNameKey       = "grafana.dashboard.name"
	grafanaDashboardMonitoringKey = "grafana.dashboard.monitoring.key"
)

var ctx = context.Background()

// DefaultConfig default configuration for operator config controller
var defaultOperatorConfig = map[string]string{
	grafanaDashboardMonitoringKey: "middleware",
	grafanaDashboardNameKey:       "infinispan",
}

// ReconcileInfinispan reconciles a Infinispan object
type ReconcileOperatorConfig struct {
	client client.Client
	scheme *runtime.Scheme
	Log    logr.Logger
	Kube   *kube.Kubernetes
}

func Add(mgr manager.Manager) error {
	return addOperatorConfig(mgr)
}

// addOperatorConfig installs a reconciler for a specific object.
// Only events coming from operatorNs/infinispan-operator-config will be processed
func addOperatorConfig(mgr manager.Manager) error {
	r := &ReconcileOperatorConfig{client: mgr.GetClient(), scheme: mgr.GetScheme(), Log: logf.Log.WithName("operator-config-controller"), Kube: kube.NewKubernetesFromController(mgr)}
	// Create a new controller
	c, err := controller.New(ControllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}
	operatorNS, err := getOperatorNamespace()
	if err != nil {
		return err
	}
	operatorConfigPredicate := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Meta.GetName() == configMapName && e.Meta.GetNamespace() == operatorNS
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Meta.GetName() == configMapName && e.Meta.GetNamespace() == operatorNS
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.MetaNew.GetName() == configMapName && e.MetaNew.GetNamespace() == operatorNS
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Meta.GetName() == configMapName && e.Meta.GetNamespace() == operatorNS
		},
	}
	// Watch for changes to global operator config
	err = c.Watch(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestForObject{}, operatorConfigPredicate)
	if err != nil {
		return err
	}
	return nil
}

var _ reconcile.Reconciler = &ReconcileOperatorConfig{}
var currentConfig map[string]string = make(map[string]string)

// Reconcile reads the operator configuration in the `infinispan-operator-config` ConfigMap and
// applies the configuration to the cluster.
func (r *ReconcileOperatorConfig) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	operatorNs, err := getOperatorNamespace()
	if err != nil {
		r.Log.Error(err, "Error getting operator runtime namespace")
		return reconcile.Result{Requeue: true}, nil
	}

	configMap := &corev1.ConfigMap{}
	err = r.client.Get(ctx, types.NamespacedName{Namespace: operatorNs, Name: configMapName}, configMap)
	if err != nil && !errors.IsNotFound(err) {
		r.Log.Error(err, "Error getting operator configuration resource")
		return reconcile.Result{Requeue: true}, nil
	}

	config := make(map[string]string)
	// Copy defaults
	for k, v := range defaultOperatorConfig {
		config[k] = v
	}
	// Merge config value with defaults
	for k, v := range configMap.Data {
		config[k] = v
	}
	res, err := r.reconcileGrafana(config, currentConfig, operatorNs)
	return *res, err
}

// getOperatorNamespace returns the operator namespace or the WATCH_NAMESPACE
// value if the operator is running outside the cluster.
// "OSDK_FORCE_RUN_MODE"="local" must be set if running locally (not in the cluster)
func getOperatorNamespace() (string, error) {
	operatorNs, err := k8sutil.GetOperatorNamespace()
	// This makes everything works even running outside the cluster
	if err == k8sutil.ErrRunLocal {
		var operatorWatchNs string
		operatorWatchNs, err = k8sutil.GetWatchNamespace()
		if operatorWatchNs != "" {
			operatorNs = strings.Split(operatorWatchNs, ",")[0]
		}
	}
	return operatorNs, err
}
