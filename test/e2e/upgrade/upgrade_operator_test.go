package upgrade

import (
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

// TestOperatorUpgradeWontTriggerRollout ensures that the Operator upgrade will not result in unwanted rollout due to changes in ConfigMap
func TestOperatorUpgradeWontTriggerRollout(t *testing.T) {
	// Ideally we should use latest release from the last minor, or intial release of current minor for testing
	// to prevent rollouts hidden in transitive updates but currently we introduced a bunch of rollouts in those releases.
	olm := testKube.OLMTestEnv()
	olm.SubStartingCSV = testKube.GetLatestReleasedCSV()

	if olm.SubStartingCSV == olm.SourceChannel.CurrentCSVName {
		t.Skip("Initial version release, skip the test")
	}

	// Only test Operands in the most recent CSV
	olm.PrintManifest()

	testKube.NewNamespace(tutils.Namespace)
	sub := subscription(olm)

	defer testKube.CleanupOLMTest(t, tutils.TestName(t), olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateOperatorGroup(olm)
	testKube.CreateSubscriptionAndApproveInitialVersion(sub)

	spec := tutils.DefaultSpec(t, testKube, nil)
	spec.Spec.Version = ""

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	spec = testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	ssGenerationPreUpgrade := testKube.GetStatefulSet(spec.GetStatefulSetName(), tutils.Namespace).Generation

	testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
	testKube.ApproveInstallPlan(sub)

	testKube.WaitForSubscription(sub, func() bool {
		return sub.Status.InstalledCSV == sub.Status.CurrentCSV
	})
	testKube.WaitForCSVSucceeded(sub)
	// Operator does not start properly on the first attempt after the upgrade and is restarted
	// https://github.com/infinispan/infinispan-operator/issues/1719
	time.Sleep(time.Minute)

	ssGenerationPostUpgrade := testKube.GetStatefulSet(spec.GetStatefulSetName(), tutils.Namespace).Generation
	if ssGenerationPostUpgrade != ssGenerationPreUpgrade {
		t.Fatalf("Operator upgrade unexpectedly rolled the StatefulSet: generation changed from %d to %d", ssGenerationPreUpgrade, ssGenerationPostUpgrade)
	}
}
