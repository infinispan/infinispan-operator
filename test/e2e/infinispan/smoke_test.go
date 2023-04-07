package infinispan

import (
	"context"
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Test if single node working correctly
func TestBaseFunctionality(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Infinispan cluster defintion with extra labels to be propagated to the service and pods
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		// Ensure that cluster creation using the limit:request format works on initial creation
		i.Spec.Container.Memory = fmt.Sprintf("%s:%s", tutils.Memory, tutils.Memory)
		i.Annotations = make(map[string]string)
		i.Spec.Replicas = int32(replicas)

		i.Annotations[v1.TargetLabels] = "my-svc-label"
		i.Annotations[v1.PodTargetLabels] = "my-pod-label"
		i.Annotations[v1.ServiceMonitorTargetLabels] = "my-servicemonitor-label"
		i.ObjectMeta.Labels["my-svc-label"] = "my-svc-value"
		i.ObjectMeta.Labels["my-pod-label"] = "my-pod-value"
		i.Annotations[v1.TargetAnnotations] = "my-svc-annotation"
		i.Annotations["my-svc-annotation"] = "my-svc-value"
		i.Annotations[v1.PodTargetAnnotations] = "my-pod-annotation"
		i.Annotations["my-pod-annotation"] = "my-pod-value"
		i.Annotations[v1.RouterAnnotations] = "my-router-annotation"
		i.Annotations["my-router-annotation"] = "my-router-value"
	})

	// Create the cluster
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	assert := assert.New(t)
	require := require.New(t)

	verifyNoPVCs(assert, ispn)
	verifyLabelsAndAnnotations(assert, require, ispn)
	verifyDefaultAuthention(require, ispn)
}

// Make sure no PVCs were created
func verifyNoPVCs(assert *assert.Assertions, ispn *v1.Infinispan) {
	pvcs := testKube.GetPVCList(ispn.Namespace, ispn.PodSelectorLabels())
	assert.Equal(0, len(pvcs.Items), "persistent volume claims were found (count = %d) but not expected for ephemeral storage configuration")
}

// Check label and annotation propagation
func verifyLabelsAndAnnotations(assert *assert.Assertions, require *require.Assertions, ispn *v1.Infinispan) {
	pod := corev1.Pod{}
	require.NoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: ispn.Name + "-0", Namespace: tutils.Namespace}, &pod))

	// From Infinispan CR to pods
	assert.Equal(ispn.ObjectMeta.Labels["my-pod-label"], pod.Labels["my-pod-label"], "Infinispan CR labels haven't been propagated to pods")
	assert.Equal(ispn.Annotations["my-pod-annotation"], pod.Annotations["my-pod-annotation"], "Infinispan CR annotations haven't been propagated to pods")

	svcList := &corev1.ServiceList{}
	require.NoError(testKube.Kubernetes.ResourcesList(ispn.Namespace, map[string]string{"infinispan_cr": "test-base-functionality"}, svcList, context.TODO()))
	require.NotEqual(0, len(svcList.Items), "No services found for cluster")

	for _, svc := range svcList.Items {
		// from Infinispan CR to service
		assert.Equal(ispn.ObjectMeta.Labels["my-svc-label"], svc.Labels["my-svc-label"], "Infinispan CR labels haven't been propagated to services")
	}
}

func verifyDefaultAuthention(require *require.Assertions, ispn *v1.Infinispan) {
	schema := testKube.GetSchemaForRest(ispn)

	user := constants.DefaultDeveloperUser
	pass, err := users.UserPassword(user, ispn.GetSecretName(), ispn.Namespace, testKube.Kubernetes, context.TODO())
	require.NoError(err)

	testAuthentication(ispn, schema, user, pass)
}

func TestCacheService(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Service.ReplicationFactor = 2
		i.Spec.Service.Type = ispnv1.ServiceTypeCache
		i.Spec.Expose = tutils.ExposeServiceSpec(testKube)
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper("default", client_)
	cacheHelper.WaitForCacheToExist()
	cacheHelper.TestBasicUsage("test", "test-operator")
}
