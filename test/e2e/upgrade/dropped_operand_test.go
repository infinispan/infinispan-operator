package upgrade

import (
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
)

const DroppedOperandVersionEnv = "TESTING_DROPPED_OPERAND_VERSION"
const DroppedOperandStartingCSVEnv = "SUBSCRIPTION_STARTING_CSV_DROPPED_OPERAND"

func TestUpgradeFromDroppedOperand(t *testing.T) {
	// Manually define last CSV version containing removed operand
	olm := testKube.OLMTestEnv()
	olm.SubStartingCSV = constants.GetEnvWithDefault(DroppedOperandStartingCSVEnv, "infinispan-operator.v2.3.7")
	olm.PrintManifest()

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

	// Delete the old subscription but keep the cluster
	testKube.DeleteSubscription(sub.Name, sub.Namespace)

	// Install the latest version of the Operator
	olm.SubStartingCSV = olm.TargetChannel.CurrentCSVName
	olm.SourceChannel = olm.TargetChannel
	sub = subscription(olm)
	testKube.CreateSubscriptionAndApproveInitialVersion(sub)

	versionManager := testKube.VersionManagerFromCSV(sub)
	latestOperand := versionManager.Latest()
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Version = latestOperand.Ref()
			tutils.Log().Infof("Upgrading Operand to %s", ispn.Spec.Version)
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
