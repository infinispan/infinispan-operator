package utils

import (
	"strings"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TestTimeout      = 5 * time.Minute
	SinglePodTimeout = 5 * time.Minute
	RouteTimeout     = 240 * time.Second
	// Default retry time when waiting for resources
	DefaultPollPeriod   = 1 * time.Second
	ConditionPollPeriod = 1 * time.Second
	// Maximum time to wait for resources
	MaxWaitTimeout       = 120 * time.Second
	ConditionWaitTimeout = 120 * time.Second
	DefaultClusterName   = "test-node-startup"

	OperatorUpgradeStageNone = "NONE"
	OperatorUpgradeStageFrom = "FROM"
	OperatorUpgradeStageTo   = "TO"
)

var (
	CPU                  = constants.GetEnvWithDefault("INFINISPAN_CPU", "500m")
	Memory               = constants.GetEnvWithDefault("INFINISPAN_MEMORY", "512Mi")
	Namespace            = strings.ToLower(constants.GetEnvWithDefault("TESTING_NAMESPACE", "namespace-for-testing"))
	MultiNamespace       = strings.ToLower(constants.GetEnvWithDefault("TESTING_MULTINAMESPACE", "namespace-for-testing-1,namespace-for-testing-2"))
	OperatorNamespace    = strings.ToLower(constants.GetEnvWithDefault("TESTING_OPERATOR_NAMESPACE", ""))
	OperatorName         = "infinispan-operator"
	RunLocalOperator     = strings.ToUpper(constants.GetEnvWithDefault("RUN_LOCAL_OPERATOR", "true"))
	RunSaOperator        = strings.ToUpper(constants.GetEnvWithDefault("RUN_SA_OPERATOR", "false"))
	OperatorUpgradeStage = strings.ToUpper(constants.GetEnvWithDefault("OPERATOR_UPGRADE_STAGE", OperatorUpgradeStageNone))
	CleanupInfinispan    = strings.ToUpper(constants.GetEnvWithDefault("CLEANUP_INFINISPAN_ON_FINISH", "true"))
	ExpectedImage        = constants.GetEnvWithDefault("EXPECTED_IMAGE", "quay.io/infinispan/server:12.1")
	ExposeServiceType    = constants.GetEnvWithDefault("EXPOSE_SERVICE_TYPE", string(ispnv1.ExposeTypeNodePort))

	OperatorUpgradeStateFlow = []ispnv1.ConditionType{ispnv1.ConditionUpgrade, ispnv1.ConditionStopping, ispnv1.ConditionWellFormed}

	WebServerName       = "external-libs-web-server"
	WebServerImageName  = "quay.io/openshift-scale/nginx"
	WebServerRootFolder = "/usr/share/nginx/html"
	WebServerPortNumber = 8080
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
