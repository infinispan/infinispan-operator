package grafana

import (
	"context"

	grafanav1alpha1 "github.com/integr8ly/grafana-operator/v3/pkg/apis/integreatly/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("infinispan-grafana")

func AddToScheme(scheme *runtime.Scheme) error {
	return grafanav1alpha1.AddToScheme(scheme)
}

func EnsureDashboardExported(context context.Context, namespace string, mgr manager.Manager) error {
	dc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return err
	}
	// detect the GrafanaDashboard resource type resourceExists on the cluster
	resourceExists, _ := k8sutil.ResourceExists(dc, grafanav1alpha1.SchemeGroupVersion.String(), grafanav1alpha1.GrafanaDashboardKind)
	if !resourceExists {
		log.Info("Grafana CRD not present - not installing dashboard CR")
		return nil
	}

	infinispanDashboard := &grafanav1alpha1.GrafanaDashboard{}

	selector := client.ObjectKey{
		Name:      ApplicationName,
		Namespace: namespace,
	}

	// Using mgr.GetClient() does not work as we don't configure the cache for this scheme
	controllerClient, err := client.New(mgr.GetConfig(), client.Options{})
	if err != nil {
		return err
	}

	log.Info("Checking if grafana dashboard already exists")
	err = controllerClient.Get(context, selector, infinispanDashboard)

	if err == nil {
		log.Info("Grafana dashboard already exists, not creating")
		return nil
	}
	// We verify that the error is not a NotFound - if it is NotFound we ignore so we can create the dashboard
	if !meta.IsNoMatchError(err) && !errors.IsNotFound(err) {
		log.Error(err, "Error encountered while retrieving grafana dashboard")
		return err
	}

	infinispanDashboard = dashboard(namespace)

	log.Info("Creating grafana dashboard in namespace %s\n", namespace)
	err = controllerClient.Create(context, infinispanDashboard)
	if err != nil {
		log.Error(err, "Error encountered while creating grafana dashboard\n")
		return err
	}

	log.Info("Successfully grafana dashboard")
	return nil
}

func dashboard(namespace string) *grafanav1alpha1.GrafanaDashboard {
	return &grafanav1alpha1.GrafanaDashboard{
		ObjectMeta: v12.ObjectMeta{
			Name:      ApplicationName,
			Namespace: namespace,
			Labels: map[string]string{
				"monitoring-key": MonitoringKey,
				"app":            "grafana",
			},
		},
		Spec: grafanav1alpha1.GrafanaDashboardSpec{
			Json: dashboardJSON,
			Name: "infinispan.json",
			Datasources: []grafanav1alpha1.GrafanaDashboardDatasource{
				{
					InputName:      "DS_PROMETHEUS",
					DatasourceName: "Prometheus",
				},
			},
		},
	}
}

func dashboardSelector(namespace string) client.ObjectKey {
	return client.ObjectKey{
		Name:      ApplicationName,
		Namespace: namespace,
	}
}
