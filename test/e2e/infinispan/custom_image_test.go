package infinispan

import (
	"context"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

func TestCustomImage(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Get the two latest Operands that share the same major/minor version, otherwise we'll hit incompatibility issue
	customOperand, versionOperand := specImageOperands()

	// Use customerOperand (previous release) as a custom image and spec.version of the latest release in the stream
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Image = pointer.String(customOperand.Image)
		i.Spec.Version = versionOperand.Ref()
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	assert.True(t, ispn.Status.Operand.CustomImage)
	assert.Equal(t, customOperand.Image, ispn.Status.Operand.Image)
	assert.Equal(t, versionOperand.Ref(), ispn.Status.Operand.Version)
	assert.Equal(t, ispnv1.OperandPhaseRunning, ispn.Status.Operand.Phase)

	ss := &appsv1.StatefulSet{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.GetStatefulSetName()}, ss))
	container := kubernetes.GetContainer(provision.InfinispanContainer, &ss.Spec.Template.Spec)

	// Ensure that no SS rolling upgrade is executed as the default test Operand is marked as a CVE release
	// https://github.com/infinispan/infinispan-operator/issues/1817
	assert.Equal(t, int64(1), ss.Generation)
	assert.Equal(t, customOperand.Image, container.Image)
}
