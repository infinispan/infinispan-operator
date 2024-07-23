package infinispan

import (
	"context"
	"strconv"
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

	img := "quay.io/infinispan/server:15.0"

	// Attempting to use latest Infinispan Server image while having version set to different major will result in misconfiguration by Operator
	if tutils.OperandVersion != "" {
		operand, _ := tutils.VersionManager().WithRef(tutils.OperandVersion)
		version := operand.UpstreamVersion
		img = "quay.io/infinispan/server:" + strconv.FormatUint(version.Major, 10) + "." + strconv.FormatUint(version.Minor, 10)
	}

	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Image = pointer.String(img)
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
	assert.Equal(t, img, ispn.Status.Operand.Image)

	ss := &appsv1.StatefulSet{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.GetStatefulSetName()}, ss))
	container := kubernetes.GetContainer(provision.InfinispanContainer, &ss.Spec.Template.Spec)
	// Ensure that no SS rolling upgrade is executed as the default test Operand is marked as a CVE release
	// https://github.com/infinispan/infinispan-operator/issues/1817
	assert.Equal(t, int64(1), ss.Generation)
	assert.Equal(t, img, container.Image)
}
