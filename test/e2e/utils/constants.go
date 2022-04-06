package utils

import (
	"os"
	"strconv"
	"strings"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	TestTimeout          = timeout("TEST_TIMEOUT", "5m")
	SinglePodTimeout     = timeout("TEST_SINGLE_POD_TIMEOUT", "5m")
	RouteTimeout         = timeout("TEST_ROUTE_TIMEOUT", "4m")
	DefaultPollPeriod    = timeout("TEST_DEFAULT_POLL_PERIOD", "1s") // DefaultPollPeriod is the default retry time when waiting for resources
	ConditionPollPeriod  = timeout("TEST_CONDITION_POLL_PERIOD", "1s")
	MaxWaitTimeout       = timeout("TEST_MAX_WAIT_TIMEOUT", "3m") // MaxWaitTimeout is the maximum time to wait for resources
	ConditionWaitTimeout = timeout("TEST_CONDITION_WAIT_TIMEOUT", "3m")

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
	SuiteMode, _      = strconv.ParseBool(constants.GetEnvWithDefault("SUITE_MODE", "false"))
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

func timeout(env, defVal string) time.Duration {
	duration, err := time.ParseDuration(constants.GetEnvWithDefault(env, defVal))
	ExpectNoError(err)
	return duration
}
