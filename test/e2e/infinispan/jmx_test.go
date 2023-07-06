package infinispan

import (
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestJmxEnabled(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	replicas := 1
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Jmx = &ispnv1.JmxSpec{
			Enabled: true,
		}
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
	pods := testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	container := kube.GetContainer(provision.InfinispanContainer, &pods.Items[0].Spec)
	var jmxEnabled bool
	for _, arg := range container.Args {
		if arg == "--jmx" {
			jmxEnabled = true
			break
		}
	}
	assert.True(t, jmxEnabled)
	ports := testKube.GetAdminServicePorts(tutils.Namespace, spec.Name)
	var jmxPort *corev1.ServicePort
	for _, port := range ports {
		if port.Name == consts.InfinispanJmxPortName {
			jmxPort = &port
			break
		}
	}
	assert.NotNil(t, jmxPort)
	assert.Equal(t, int32(consts.InfinispanJmxPort), jmxPort.Port)
}
