package controllers

import (
	"context"
	"errors"
	"fmt"

	rice "github.com/GeertJohan/go.rice"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	grafanav1alpha1 "github.com/infinispan/infinispan-operator/pkg/apis/integreatly/v1alpha1"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	grafanaDashboardNamespaceKey  = "grafana.dashboard.namespace"
	grafanaDashboardNameKey       = "grafana.dashboard.name"
	grafanaDashboardMonitoringKey = "grafana.dashboard.monitoring.key"
)

// +kubebuilder:rbac:groups=integreatly.org,resources=grafanadashboards,verbs=get;list;watch;create;delete;update

// reconcileGrafana reconciles grafana object status with the operator configuration settings
func (r *ReconcileOperatorConfig) reconcileGrafana(ctx context.Context, config, currentConfig map[string]string, operatorNs string) (*reconcile.Result, error) {
	grafanaNs := config[grafanaDashboardNamespaceKey]

	// Delete current grafana dashboard if namespace is changes
	if err := r.deleteDashboardOnKeyChanged(ctx, config, currentConfig); err != nil {
		return &reconcile.Result{}, err
	}

	if grafanaNs == "" {
		return &reconcile.Result{}, nil
	}
	// detect the GrafanaDashboard resource type resourceExists on the cluster
	resourceExists, err := r.kubernetes.IsGroupVersionSupported(grafanav1alpha1.SchemeGroupVersion.String(), grafanav1alpha1.GrafanaDashboardKind)
	if err != nil {
		r.log.Error(err, "Error checking Grafana support")
		return &reconcile.Result{Requeue: true}, nil
	}
	if !resourceExists {
		r.log.Info("Grafana CRD not present - not installing dashboard CR")
		return &reconcile.Result{RequeueAfter: consts.DefaultLongWaitOnCreateResource}, nil
	}
	infinispanDashboard := emptyDashboard(config)
	if _, err = controllerutil.CreateOrUpdate(ctx, r.Client, infinispanDashboard, func() error {
		if infinispanDashboard.CreationTimestamp.IsZero() {
			if grafanaNs == operatorNs {
				if ownRef, err := kubernetes.GetOperatorPodOwnerRef(operatorNs, r.Client, ctx); err != nil {
					if errors.Is(err, kubernetes.ErrRunLocal) {
						r.log.Info(fmt.Sprintf("Not setting controller reference for Grafana Dashboard, cause %s.", err.Error()))
					} else {
						return err
					}
				} else {
					r.log.Info("Operator Pod owner found", "Kind", ownRef.Kind, "Name", ownRef.Name)
					infinispanDashboard.SetOwnerReferences([]metav1.OwnerReference{*ownRef})
				}
			} else {
				r.log.Info("Not setting controller reference, cause Infinispan and Grafana are in different namespaces.")
			}
		}
		return populateDashboard(infinispanDashboard, config)
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

func populateDashboard(dashboard *grafanav1alpha1.GrafanaDashboard, config map[string]string) error {
	box, err := rice.FindBox("resources")
	if err != nil {
		return err
	}
	dashboardJSONData, err := box.String("grafana_dashboard.json")
	if err != nil {
		return err
	}
	dashboard.Labels = map[string]string{
		"monitoring-key": config[grafanaDashboardMonitoringKey],
		"app":            "grafana",
	}
	dashboard.Spec = grafanav1alpha1.GrafanaDashboardSpec{
		Json: dashboardJSONData,
		// TODO migration to 1.22. No more needed Name field?
		// Name: "infinispan.json",
		Datasources: []grafanav1alpha1.GrafanaDashboardDatasource{
			{
				InputName:      "DS_PROMETHEUS",
				DatasourceName: "Prometheus",
			},
		},
	}
	return nil
}

func (r *ReconcileOperatorConfig) deleteDashboardOnKeyChanged(ctx context.Context, newCfg, curCfg map[string]string) error {
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
		if err := r.kubernetes.Client.Delete(ctx, currGrafana); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
		currentConfig[grafanaDashboardNamespaceKey] = ""
		currentConfig[grafanaDashboardNameKey] = ""
	}
	return nil
}
