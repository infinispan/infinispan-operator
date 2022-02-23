package utils

import (
	"os"
	"strings"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TestTimeout      = 5 * time.Minute
	SinglePodTimeout = 5 * time.Minute
	RouteTimeout     = 240 * time.Second
	// DefaultPollPeriod is the default retry time when waiting for resources
	DefaultPollPeriod   = 1 * time.Second
	ConditionPollPeriod = 100 * time.Millisecond
	// MaxWaitTimeout is the maximum time to wait for resources
	MaxWaitTimeout       = 3 * time.Minute
	ConditionWaitTimeout = 3 * time.Minute
	DefaultClusterName   = "test-node-startup"
)

var (
	CPU               = os.Getenv("INFINISPAN_CPU")
	Memory            = constants.GetEnvWithDefault("INFINISPAN_MEMORY", "1Gi")
	Namespace         = strings.ToLower(constants.GetEnvWithDefault("TESTING_NAMESPACE", "namespace-for-testing"))
	MultiNamespace    = strings.ToLower(constants.GetEnvWithDefault("TESTING_MULTINAMESPACE", "namespace-for-testing-1,namespace-for-testing-2"))
	OperatorNamespace = strings.ToLower(constants.GetEnvWithDefault("TESTING_OPERATOR_NAMESPACE", ""))
	OperatorName      = "infinispan-operator"
	OperatorSAName    = strings.ToLower(constants.GetEnvWithDefault("OPERATOR_SA_NAME", "infinispan-operator-controller-manager"))
	RunLocalOperator  = strings.ToUpper(constants.GetEnvWithDefault("RUN_LOCAL_OPERATOR", "true"))
	RunSaOperator     = strings.ToUpper(constants.GetEnvWithDefault("RUN_SA_OPERATOR", "false"))
	CleanupInfinispan = strings.ToUpper(constants.GetEnvWithDefault("CLEANUP_INFINISPAN_ON_FINISH", "true"))
	ExposeServiceType = constants.GetEnvWithDefault("EXPOSE_SERVICE_TYPE", string(ispnv1.ExposeTypeNodePort))

	WebServerName       = "external-libs-web-server"
	WebServerImageName  = "quay.io/openshift-scale/nginx"
	WebServerRootFolder = "/usr/share/nginx/html"
	WebServerPortNumber = 8080

	CleanupXSiteOnFinish = strings.ToUpper(constants.GetEnvWithDefault("TESTING_CLEANUP_XSITE_ON_FINISH", "TRUE")) == "TRUE"
)

// DeleteOpts is used when deleting resources
var DeleteOpts = []client.DeleteOption{
	client.GracePeriodSeconds(int64(0)),
	client.PropagationPolicy(metav1.DeletePropagationBackground),
}

var InfinispanTypeMeta = metav1.TypeMeta{
	APIVersion: "infinispan.org/v1",
	Kind:       "Infinispan",
}
