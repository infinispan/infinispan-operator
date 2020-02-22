package launcher

import (
	"flag"
	"fmt"
	"github.com/spf13/pflag"
	"os"
	"runtime"

	"github.com/infinispan/infinispan-operator/pkg/apis"
	"github.com/infinispan/infinispan-operator/pkg/controller"
	"github.com/infinispan/infinispan-operator/version"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	sdkv "github.com/operator-framework/operator-sdk/version"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // blank import (?)
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

func printVersion() {
	logf.Log.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	logf.Log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	logf.Log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	logf.Log.Info(fmt.Sprintf("Operator SDK Version: %v", sdkv.Version))
}

// Parameters represent operator launcher parameters
type Parameters struct {
	StopChannel chan struct{}
}

// Launch launches operator
func Launch(params Parameters) {
	//TODO Uncomment after Operator SDK update
	//pflag.CommandLine.AddFlagSet(zap.FlagSet())
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	flag.Parse()

	// The logger instantiated here can be changed to any logger
	// implementing the logr.Logger interface. This logger will
	// be propagated through the whole operator, generating
	// uniform and structured logs.
	log := logf.Log.WithName("cmd")
	logf.SetLogger(logf.ZapLogger(false))

	printVersion()

	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		log.Error(err, "failed to get watch namespace")
		os.Exit(1)
	}

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{Namespace: namespace})
	if err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "")
		os.Exit(1)
	}

	log.Info("Starting the Cmd.")

	var stopCh <-chan struct{}
	if params.StopChannel != nil {
		stopCh = params.StopChannel
	} else {
		stopCh = signals.SetupSignalHandler()
	}
	if err := mgr.Start(stopCh); err != nil {
		log.Error(err, "manager exited non-zero")
		os.Exit(1)
	}
}
