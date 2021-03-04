package operatorconfig

import (
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	grafanav1alpha1 "github.com/integr8ly/grafana-operator/v3/pkg/apis/integreatly/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// reconcileGrafana reconciles grafana object status with the operator configuration settings
func (r *ReconcileOperatorConfig) reconcileGrafana(config, currentConfig map[string]string, operatorNs string) (*reconcile.Result, error) {
	grafanaNs := config[grafanaDashboardNamespaceKey]

	// Delete current grafana dashboard if namespace is changes
	if err := r.deleteDashboardOnKeyChanged(config, currentConfig); err != nil {
		return &reconcile.Result{}, err
	}

	if grafanaNs == "" {
		return &reconcile.Result{}, nil
	}
	// detect the GrafanaDashboard resource type resourceExists on the cluster
	resourceExists, err := r.IsGroupVersionSupported(grafanav1alpha1.SchemeGroupVersion.String(), grafanav1alpha1.GrafanaDashboardKind)
	if err != nil {
		r.Logger().Error(err, "Error checking Grafana support")
		return &reconcile.Result{Requeue: true}, nil
	}
	if !resourceExists {
		r.Logger().Info("Grafana CRD not present - not installing dashboard CR")
		return &reconcile.Result{RequeueAfter: consts.DefaultLongWaitOnCreateResource}, nil
	}
	infinispanDashboard := emptyDashboard(config)
	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, infinispanDashboard, func() error {
		if infinispanDashboard.CreationTimestamp.IsZero() {
			if grafanaNs == operatorNs {
				operatorDeployment := &appsv1.Deployment{}
				if result, err := r.LookupResource(operatorDeploymentName, operatorNs, operatorDeployment, nil, r); result == nil {
					// If running in cluster set controller config
					if err = controllerutil.SetControllerReference(operatorDeployment, infinispanDashboard, r.Scheme); err != nil {
						return err
					}
				} else {
					return err
				}
			} else {
				r.Logger().Info("Not setting controller reference, cause Infinispan and Grafana are in different namespaces.")
			}
		}
		populateDashboard(infinispanDashboard, config)
		return nil
	}); err != nil {
		return &reconcile.Result{}, err
	}

	currentConfig[grafanaDashboardNamespaceKey] = grafanaNs
	currentConfig[grafanaDashboardNameKey] = config[grafanaDashboardNameKey]
	return &reconcile.Result{}, nil
}

func emptyDashboard(config map[string]string) *grafanav1alpha1.GrafanaDashboard {
	return &grafanav1alpha1.GrafanaDashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config[grafanaDashboardNameKey],
			Namespace: config[grafanaDashboardNamespaceKey],
		},
	}
}

func populateDashboard(dashboard *grafanav1alpha1.GrafanaDashboard, config map[string]string) {
	dashboard.ObjectMeta.Labels = map[string]string{
		"monitoring-key": config[grafanaDashboardMonitoringKey],
		"app":            "grafana",
	}
	dashboard.Spec = grafanav1alpha1.GrafanaDashboardSpec{
		Json: dashboardJSON,
		Name: "infinispan.json",
		Datasources: []grafanav1alpha1.GrafanaDashboardDatasource{
			{
				InputName:      "DS_PROMETHEUS",
				DatasourceName: "Prometheus",
			},
		},
	}
}

func (r *ReconcileOperatorConfig) deleteDashboardOnKeyChanged(newCfg, curCfg map[string]string) error {
	// If key is changed and old key is not nil, delete old grafana dashboard
	if (newCfg[grafanaDashboardNameKey] != curCfg[grafanaDashboardNameKey] ||
		newCfg[grafanaDashboardNamespaceKey] != curCfg[grafanaDashboardNamespaceKey]) &&
		curCfg[grafanaDashboardNameKey] != "" &&
		curCfg[grafanaDashboardNamespaceKey] != "" {
		currGrafana := &grafanav1alpha1.GrafanaDashboard{
			ObjectMeta: metav1.ObjectMeta{
				Name:      curCfg[grafanaDashboardNameKey],
				Namespace: curCfg[grafanaDashboardNamespaceKey],
			},
		}
		if err := r.Delete(ctx, currGrafana); err != nil && !errors.IsNotFound(err) {
			return err
		}
		currentConfig[grafanaDashboardNamespaceKey] = ""
		currentConfig[grafanaDashboardNameKey] = ""
	}
	return nil
}
