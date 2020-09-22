package constants

import (
	"strings"
	"time"

	comutil "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TestTimeout      = 5 * time.Minute
	SinglePodTimeout = 5 * time.Minute
	RouteTimeout     = 240 * time.Second
	// Default retry time when waiting for resources
	DefaultPollPeriod = 1 * time.Second
	// Maximum time to wait for resources
	MaxWaitTimeout      = 120 * time.Second
	DefaultClusterPort  = 11222
	DefaultClusterName  = "test-node-startup"
	ClusterUpKubeConfig = "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
)

var (
	CPU               = comutil.GetEnvWithDefault("INFINISPAN_CPU", "500m")
	Memory            = comutil.GetEnvWithDefault("INFINISPAN_MEMORY", "512Mi")
	Namespace         = strings.ToLower(comutil.GetEnvWithDefault("TESTING_NAMESPACE", "namespace-for-testing"))
	RunLocalOperator  = strings.ToUpper(comutil.GetEnvWithDefault("RUN_LOCAL_OPERATOR", "true"))
	RunSaOperator     = strings.ToUpper(comutil.GetEnvWithDefault("RUN_SA_OPERATOR", "true"))
	ImageName         = comutil.GetEnvWithDefault("IMAGE", "registry.hub.docker.com/infinispan/server")
	ExposeServiceType = comutil.GetEnvWithDefault("EXPOSE_SERVICE_TYPE", "NodePort")
)

// Options used when deleting resources
var DeleteOpts = []client.DeleteOption{
	client.GracePeriodSeconds(int64(0)),
	client.PropagationPolicy(metav1.DeletePropagationBackground),
}

var InfinispanTypeMeta = metav1.TypeMeta{
	APIVersion: "infinispan.org/v1",
	Kind:       "Infinispan",
}
