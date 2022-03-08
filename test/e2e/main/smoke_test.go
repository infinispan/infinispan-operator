package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers"
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
	spec := tutils.DefaultSpec(t, testKube)
	// Ensure that cluster creation using the limit:request format works on initial creation
	spec.Spec.Container.Memory = fmt.Sprintf("%s:%s", tutils.Memory, tutils.Memory)
	spec.Annotations = make(map[string]string)
	spec.Spec.Replicas = int32(replicas)

	spec.Annotations[v1.TargetLabels] = "my-svc-label"
	spec.Labels["my-svc-label"] = "my-svc-value"
	tutils.ExpectNoError(os.Setenv(v1.OperatorTargetLabelsEnvVarName, "{\"operator-svc-label\":\"operator-svc-value\"}"))
	defer os.Unsetenv(v1.OperatorTargetLabelsEnvVarName)

	spec.Annotations[v1.PodTargetLabels] = "my-pod-label"
	spec.Labels["my-pod-label"] = "my-pod-value"
	tutils.ExpectNoError(os.Setenv(v1.OperatorPodTargetLabelsEnvVarName, "{\"operator-pod-label\":\"operator-pod-value\"}"))
	defer os.Unsetenv(v1.OperatorPodTargetLabelsEnvVarName)

	// Create the cluster
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	assert := assert.New(t)
	require := require.New(t)

	verifyNoPVCs(assert, require, ispn)
	verifyLabels(assert, require, ispn)
	verifyDefaultAuthention(require, ispn)
}

// Make sure no PVCs were created
func verifyNoPVCs(assert *assert.Assertions, require *require.Assertions, ispn *v1.Infinispan) {
	pvcs := &corev1.PersistentVolumeClaimList{}
	err := testKube.Kubernetes.ResourcesList(ispn.Namespace, controllers.PodLabels(ispn.Name), pvcs, context.TODO())

	require.NoError(err)
	assert.Equal(0, len(pvcs.Items), "persistent volume claims were found (count = %d) but not expected for ephemeral storage configuration")
}

// Check labels propagation
func verifyLabels(assert *assert.Assertions, require *require.Assertions, ispn *v1.Infinispan) {
	pod := corev1.Pod{}
	require.NoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: ispn.Name + "-0", Namespace: tutils.Namespace}, &pod))

	// From Infinispan CR to pods
	assert.Equal(ispn.Labels["my-pod-label"], pod.Labels["my-pod-label"], "Infinispan CR labels haven't been propagated to pods")

	// From operator environment
	if tutils.RunLocalOperator == "TRUE" {
		// running locally, labels are hardcoded and set by the testsuite
		assert.Equal("operator-pod-value", pod.Labels["operator-pod-label"], "Operator labels haven't been propagated to pods")
		assert.Equal("operator-pod-value", ispn.Labels["operator-pod-label"], "Operaotr labels haven't been propagated to Infinsipan CR")
	} else {
		// Get the operator namespace from the env if it's different from the testsuite one
		operatorNS := constants.GetWithDefault(tutils.OperatorNamespace, ispn.Namespace)

		// operator deployed on cluster, labels are set by the deployment
		if !areOperatorLabelsPropagated(operatorNS, ispnv1.OperatorPodTargetLabelsEnvVarName, pod.Labels) {
			assert.Fail("Operator labels haven't been propagated to pods")
		}
	}

	svcList := &corev1.ServiceList{}
	require.NoError(testKube.Kubernetes.ResourcesList(ispn.Namespace, map[string]string{"infinispan_cr": "test-base-functionality"}, svcList, context.TODO()))
	require.NotEqual(0, len(svcList.Items), "No services found for cluster")

	for _, svc := range svcList.Items {
		// from Infinispan CR to service
		assert.Equal(ispn.Labels["my-svc-label"], svc.Labels["my-svc-label"], "Infinispan CR labels haven't been propagated to services")

		// from operator environment
		if tutils.RunLocalOperator == "TRUE" {
			// running locally, labels are hardcoded and set by the testsuite
			assert.Equal("operator-svc-value", svc.Labels["operator-svc-label"], "Labels haven't been propagated to services")
			assert.Equal("operator-svc-value", ispn.Labels["operator-svc-label"], "Labels haven't been propagated to Infinispan CR")
		} else {
			// Get the operator namespace from the env if it's different from the testsuite one
			operatorNS := constants.GetWithDefault(tutils.OperatorNamespace, ispn.Namespace)

			// operator deployed on cluster, labels are set by the deployment
			if !areOperatorLabelsPropagated(operatorNS, ispnv1.OperatorTargetLabelsEnvVarName, svc.Labels) {
				assert.Fail("Operator labels haven't been propagated to services")
			}
		}
	}
}

// areOperatorLabelsPropagated helper function that read the labels from the infinispan operator pod
// and match them with the labels map provided by the caller
func areOperatorLabelsPropagated(namespace, varName string, labels map[string]string) bool {
	podList := &corev1.PodList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(namespace, map[string]string{"name": tutils.OperatorName}, podList, context.TODO()))
	if len(podList.Items) == 0 {
		panic("Cannot get the Infinispan operator pod")
	}
	labelsAsString := ""
	for _, item := range podList.Items[0].Spec.Containers[0].Env {
		if item.Name == varName {
			labelsAsString = item.Value
		}
	}
	if labelsAsString == "" {
		return true
	}
	opLabels := make(map[string]string)
	if json.Unmarshal([]byte(labelsAsString), &opLabels) != nil {
		return true
	}
	for name, value := range opLabels {
		if labels[name] != value {
			return false
		}
	}
	return true
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

	spec := tutils.DefaultSpec(t, testKube)
	spec.Spec.Service.Type = ispnv1.ServiceTypeCache
	spec.Spec.Expose = tutils.ExposeServiceSpec(testKube)

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper("default", client_)
	cacheHelper.WaitForCacheToExist()
	cacheHelper.TestBasicUsage("test", "test-operator")
}
