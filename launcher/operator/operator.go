package operator

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/infinispan/infinispan-operator/controllers"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/infinispan/infinispan-operator/launcher"

	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	infinispanv2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	grafanav1alpha1 "github.com/infinispan/infinispan-operator/pkg/apis/integreatly/v1alpha1"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ingressv1 "k8s.io/api/networking/v1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(infinispanv1.AddToScheme(scheme))
	utilruntime.Must(infinispanv2alpha1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(ingressv1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(grafanav1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type Parameters struct {
	MetricsBindAddress     string
	HealthProbeBindAddress string
	LeaderElection         bool
	ZapOptions             *zap.Options
}

func New(p Parameters) {
	NewWithContext(ctrl.SetupSignalHandler(), p)
}

// Cancelling the context won't work correctly until we upgrade to at least controller-runtime v0.9.0
// Known issues:
// - https://github.com/kubernetes-sigs/controller-runtime/pull/1428
func NewWithContext(ctx context.Context, p Parameters) {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(p.ZapOptions)))

	setupLog := ctrl.Log.WithName("setup")
	namespace, err := kubernetes.GetWatchNamespace()
	if err != nil {
		setupLog.Error(err, "failed to get watch namespace")
		os.Exit(1)
	}

	fipsEnabled, err := func() (bool, error) {
		bytes, err := os.ReadFile("/proc/sys/crypto/fips_enabled")
		if err != nil {
			return false, err
		}
		return strconv.ParseBool(string(bytes)[0:1])
	}()
	if err != nil {
		setupLog.Error(err, "unable to determine if FIPS enabled")
	}
	setupLog.Info("FIPS", "enabled", fipsEnabled)

	options := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     p.MetricsBindAddress,
		Port:                   9443,
		HealthProbeBindAddress: p.HealthProbeBindAddress,
		LeaderElection:         p.LeaderElection,
		LeaderElectionID:       "632512e4.infinispan.org",
	}

	if strings.Contains(namespace, ",") {
		options.NewCache = cache.MultiNamespacedCacheBuilder(strings.Split(namespace, ","))
	} else {
		options.Namespace = namespace
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.InfinispanReconciler{FipsEnabled: fipsEnabled}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Infinispan")
		os.Exit(1)
	}

	if err = (&controllers.BackupReconciler{}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Backup")
		os.Exit(1)
	}
	if err = (&controllers.RestoreReconciler{}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Restore")
		os.Exit(1)
	}
	if err = (&controllers.BatchReconciler{}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Batch")
		os.Exit(1)
	}
	if err = (&controllers.CacheReconciler{}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cache")
		os.Exit(1)
	}

	if err = (&controllers.ReconcileOperatorConfig{}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "OperatorConfig")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	// Setup webhooks if enabled
	if os.Getenv("ENABLE_WEBHOOKS") != "false" {

		webhookServer := mgr.GetWebhookServer()
		if _, err := os.Stat("/tmp/k8s-webhook-server/serving-certs"); os.IsNotExist(err) {
			// Use the old webhook cert directory if running on Openshift 4.6 or older
			webhookServer.CertDir = "/apiserver.local.config/certificates"
			webhookServer.CertName = "apiserver.crt"
			webhookServer.KeyName = "apiserver.key"
			setupLog.Info("Using legacy webhook certificate mounts", "CertDir", webhookServer.CertDir, "CertName", webhookServer.CertName, "KeyName", webhookServer.KeyName)
		}

		if err = (&infinispanv1.Infinispan{}).SetupWebhookWithManager(mgr, fipsEnabled); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Infinispan")
			os.Exit(1)
		}

		if err = (&infinispanv2alpha1.Batch{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Batch")
			os.Exit(1)
		}

		if err = (&infinispanv2alpha1.Cache{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create defaulting webhook", "webhook", "Cache")
			os.Exit(1)
		}

		infinispanv2alpha1.RegisterCacheValidatingWebhook(mgr)

		if err = (&infinispanv2alpha1.Backup{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Backup")
			os.Exit(1)
		}

		if err = (&infinispanv2alpha1.Restore{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "Restore")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info(fmt.Sprintf("Starting Infinispan Operator Version: %s", launcher.Version))
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
