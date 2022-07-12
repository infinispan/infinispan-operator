package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/infinispan/infinispan-operator/launcher/listener"
	"github.com/infinispan/infinispan-operator/launcher/operator"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

func main() {
	if len(os.Args) < 2 {
		exit()
	}

	zapOpts := zap.Options{
		Development: true,
	}

	// Operator Flags
	operatorFs := flag.NewFlagSet("operator", flag.ExitOnError)
	metricsAddr := operatorFs.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	probeAddr := operatorFs.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	enableLeaderElection := operatorFs.Bool("leader-elect", false, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	zapOpts.BindFlags(operatorFs)

	// Listener Flags
	listenerFs := flag.NewFlagSet("listener", flag.ExitOnError)
	listenerFs.String("kubeconfig", "", "Paths to a kubeconfig. Only required if out-of-cluster.")
	listenerNs := listenerFs.String("namespace", "", "The namespace of the Infinispan cluster.")
	listenerCluster := listenerFs.String("cluster", "", "The name of the Infinispan cluster.")
	zapOpts.BindFlags(listenerFs)

	switch os.Args[1] {
	case "operator":
		parse(operatorFs, os.Args[2:])
		operator.New(operator.Parameters{
			MetricsBindAddress:     *metricsAddr,
			HealthProbeBindAddress: *probeAddr,
			LeaderElection:         *enableLeaderElection,
			ZapOptions:             &zapOpts,
		})
	case "listener":
		parse(listenerFs, os.Args[2:])
		if *listenerNs == "" {
			listenerFs.Usage()
			os.Exit(1)
		}
		listener.New(context.Background(), listener.Parameters{
			Namespace:  *listenerNs,
			Cluster:    *listenerCluster,
			ZapOptions: &zapOpts,
		})
	default:
		exit()
	}
}

func exit() {
	fmt.Println("asfasfexpected 'operator' or 'listener' subcommands")
	os.Exit(1)
}

func parse(flags *flag.FlagSet, args []string) {
	if err := flags.Parse(args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
