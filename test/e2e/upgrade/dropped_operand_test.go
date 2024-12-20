package upgrade

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/blang/semver"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

const DroppedOperandVersionEnv = "TESTING_DROPPED_OPERAND_VERSION"

func TestUpgradeFromDroppedOperand(t *testing.T) {
	olm := testKube.OLMTestEnv()
	olm.PrintManifest()
	sourceChannel := olm.SourceChannel
	targetChannel := olm.TargetChannel

	// Skip the test when the specified starting CSV makes the test invalid
	if os.Getenv("DroppedOperandVersionEnv") == "" && strings.HasPrefix(olm.SubStartingCSV, "infinispan") {
		csv := semver.MustParse(strings.Split(olm.SubStartingCSV, "infinispan-operator.v")[1])
		if csv.Major >= 2 && csv.Minor >= 4 && csv.Patch >= 3 {
			t.Skipf("Skipping test as specified CSV '%s' does not contain dropped Operand 13.0.10", olm.SubStartingCSV)
		}
	}

	testKube.NewNamespace(tutils.Namespace)
	sub := subscription(olm)
	defer testKube.CleanupOLMTest(t, tutils.TestName(t), olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateOperatorGroup(olm)
	testKube.CreateSubscriptionAndApproveInitialVersion(sub)

	replicas := 1
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = constants.GetEnvWithDefault(DroppedOperandVersionEnv, "13.0.10")
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(targetChannel.Name, sub)
	}

	for testKube.Subscription(sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
		testKube.ApproveInstallPlan(sub)
		testKube.WaitForSubscription(sub, func() bool {
			return sub.Status.InstalledCSV == sub.Status.CurrentCSV
		})
		testKube.WaitForCSVSucceeded(sub)
	}

	versionManager := testKube.VersionManagerFromCSV(sub)
	latestOperand := versionManager.Latest()
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Version = latestOperand.Ref()
			fmt.Printf("Upgrading Operand to %s\n", ispn.Spec.Version)
		}),
	)
	testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
		return !i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.Version == latestOperand.Ref() &&
			i.Status.Operand.Image == latestOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhasePending
	})
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
		return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.Version == latestOperand.Ref() &&
			i.Status.Operand.Image == latestOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
	})
}
