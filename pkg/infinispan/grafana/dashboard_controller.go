package grafana

import (
	"context"

	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	grafanav1alpha1 "github.com/integr8ly/grafana-operator/v3/pkg/apis/integreatly/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	appsv1 "k8s.io/api/apps/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("infinispan-grafana")

func AddToScheme(scheme *runtime.Scheme) error {
	return grafanav1alpha1.AddToScheme(scheme)
}

func Create(namespace string, mgr manager.Manager) error {
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

	// Using mgr.GetClient() does not work as we don't configure the cache for this scheme
	controllerClient, err := client.New(mgr.GetConfig(), client.Options{})
	if err != nil {
		return err
	}

	infinispanDashboard := dashboard(namespace)

	operatorDeployment := &appsv1.Deployment{}
	result, err := infinispan.LookupResource("infinispan-operator", namespace, operatorDeployment, controllerClient, log)

	if err != nil {
		log.Error(err, "There was a problem looking up the infinispan operator to add the dashboard as a secondary resource")
		return err
	}

	if result != nil {
		log.Info("Infinispan operator resource not initialized, not installing Grafana Dashboard")
		return nil
	}

	// result and err should be analyzed
	if err = controllerutil.SetControllerReference(operatorDeployment, infinispanDashboard, mgr.GetScheme()); err != nil {
		log.Error(err, "Unable to set Grafana Dashboard controller reference to Infinispan operator - skipping creation")
		return nil
	}

	if _, err = controllerutil.CreateOrUpdate(context.Background(), controllerClient, infinispanDashboard, func() error {
		// Don't do any merge
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func dashboard(namespace string) *grafanav1alpha1.GrafanaDashboard {
	return &grafanav1alpha1.GrafanaDashboard{
		ObjectMeta: v12.ObjectMeta{
			Name:      constants.GetEnvWithDefault(ApplicationNameVariable, DefaultApplicationName),
			Namespace: namespace,
			Labels: map[string]string{
				"monitoring-key": constants.GetEnvWithDefault(MonitoringKeyVariable, DefaultMonitoringKey),
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
		Name:      constants.GetEnvWithDefault(ApplicationNameVariable, DefaultApplicationName),
		Namespace: namespace,
	}
}
