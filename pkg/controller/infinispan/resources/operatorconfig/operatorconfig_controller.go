package operatorconfig

import (
	"context"

	"github.com/go-logr/logr"
	k8sutil "github.com/infinispan/infinispan-operator/pkg/k8sutil"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
	Client client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	Kube   *kube.Kubernetes
}

// addOperatorConfig installs a reconciler for a specific object.
// Only events coming from operatorNs/infinispan-operator-config will be processed
func (r *ReconcileOperatorConfig) addOperatorConfig(mgr manager.Manager) error {
	// Create a new controller
	operatorNS, err := k8sutil.GetOperatorNamespace()
	if err != nil {
		return err
	}
	operatorConfigPredicate := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == configMapName && e.Object.GetNamespace() == operatorNS
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetName() == configMapName && e.Object.GetNamespace() == operatorNS
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == configMapName && e.ObjectNew.GetNamespace() == operatorNS
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return e.Object.GetName() == configMapName && e.Object.GetNamespace() == operatorNS
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).WithEventFilter(operatorConfigPredicate).Complete(r)
}

var _ reconcile.Reconciler = &ReconcileOperatorConfig{}
var currentConfig map[string]string = make(map[string]string)

// Reconcile reads the operator configuration in the `infinispan-operator-config` ConfigMap and
// applies the configuration to the cluster.
func (r *ReconcileOperatorConfig) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	operatorNs, err := k8sutil.GetOperatorNamespace()
	if err != nil {
		r.Log.Error(err, "Error getting operator runtime namespace")
		return reconcile.Result{Requeue: true}, nil
	}

	configMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: operatorNs, Name: configMapName}, configMap)
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

// SetupWithManager sets up the controller with the Manager.
func (r *ReconcileOperatorConfig) SetupWithManager(mgr manager.Manager) error {
	return r.addOperatorConfig(mgr)
}
