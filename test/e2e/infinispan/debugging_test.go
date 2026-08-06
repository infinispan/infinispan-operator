package infinispan

import (
	"context"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestReconciliationPause(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a single-replica Infinispan cluster
	spec := tutils.DefaultSpec(t, testKube, nil)
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Annotate the resource to pause reconciliation
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			if ispn.Annotations == nil {
				ispn.Annotations = make(map[string]string)
			}
			ispn.Annotations[consts.AnnotationPaused] = "true"
		}),
	)

	// Wait for the ReconciliationPaused condition to appear
	ispn = testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionReconciliationPaused)

	// Scale to 2 replicas while paused
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Replicas = 2
		}),
	)

	// Verify the StatefulSet does NOT scale up (stays at 1 replica)
	time.Sleep(10 * time.Second)
	sts := testKube.GetStatefulSet(ispn.GetStatefulSetName(), ispn.Namespace)
	assert.Equal(t, int32(1), *sts.Spec.Replicas, "StatefulSet should remain at 1 replica while reconciliation is paused")

	// Remove the pause annotation
	ispn = testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionReconciliationPaused)
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			delete(ispn.Annotations, consts.AnnotationPaused)
		}),
	)

	// Wait for the condition to be removed
	tutils.ExpectNoError(
		wait.PollUntilContextTimeout(context.Background(), tutils.ConditionPollPeriod, tutils.ConditionWaitTimeout, false, func(ctx context.Context) (bool, error) {
			current := &ispnv1.Infinispan{}
			if err := testKube.Kubernetes.Client.Get(ctx, types.NamespacedName{Name: spec.Name, Namespace: spec.Namespace}, current); err != nil {
				return false, err
			}
			return !current.HasCondition(ispnv1.ConditionReconciliationPaused), nil
		}),
	)

	// Verify the StatefulSet automatically scales up to 2 replicas
	testKube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	sts = testKube.GetStatefulSet(ispn.GetStatefulSetName(), ispn.Namespace)
	assert.Equal(t, int32(2), *sts.Spec.Replicas, "StatefulSet should scale to 2 replicas after reconciliation is unpaused")

	// Verify the condition was indeed removed
	current := &ispnv1.Infinispan{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: spec.Name, Namespace: spec.Namespace}, current))
	assert.False(t, current.HasCondition(ispnv1.ConditionReconciliationPaused))
}
