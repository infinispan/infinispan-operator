package upgrade

import (
	"testing"

	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
)

// TestOperatorUpgradeWontTriggerRollout ensures that the Operator upgrade will not result in unwanted rollout due to changes in ConfigMap
func TestOperatorUpgradeGA(t *testing.T) {
	olm := testKube.OLMTestEnv()

	// Only test Operands in the most recent CSV
	olm.PrintManifest()

	testKube.NewNamespace(tutils.Namespace)
	sub := subscription(olm)

	defer testKube.CleanupOLMTest(t, tutils.TestName(t), olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateOperatorGroup(olm)
	testKube.CreateSubscriptionAndApproveInitialVersion(sub)

}
