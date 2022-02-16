package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Test if single node working correctly
func TestNodeStartup(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube)
	spec.Annotations = make(map[string]string)
	spec.Annotations[v1.TargetLabels] = "my-svc-label"
	spec.Labels["my-svc-label"] = "my-svc-value"
	tutils.ExpectNoError(os.Setenv(v1.OperatorTargetLabelsEnvVarName, "{\"operator-svc-label\":\"operator-svc-value\"}"))
	defer os.Unsetenv(v1.OperatorTargetLabelsEnvVarName)
	spec.Annotations[v1.PodTargetLabels] = "my-pod-label"
	spec.Labels["my-svc-label"] = "my-svc-value"
	spec.Labels["my-pod-label"] = "my-pod-value"
	tutils.ExpectNoError(os.Setenv(v1.OperatorPodTargetLabelsEnvVarName, "{\"operator-pod-label\":\"operator-pod-value\"}"))
	defer os.Unsetenv(v1.OperatorPodTargetLabelsEnvVarName)

	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	pod := corev1.Pod{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: spec.Name + "-0", Namespace: tutils.Namespace}, &pod))

	// Checking labels propagation to pods
	// from Infinispan CR to pods
	if pod.Labels["my-pod-label"] != ispn.Labels["my-pod-label"] {
		panic("Infinispan CR labels haven't been propagated to pods")
	}

	// from operator environment
	if tutils.RunLocalOperator == "TRUE" {
		// running locally, labels are hardcoded and set by the testsuite
		if pod.Labels["operator-pod-label"] != "operator-pod-value" ||
			ispn.Labels["operator-pod-label"] != "operator-pod-value" {
			panic("Infinispan CR labels haven't been propagated to pods")
		}
	} else {
		// Get the operator namespace from the env if it's different
		// from the testsuite one
		operatorNS := tutils.OperatorNamespace
		if operatorNS == "" {
			operatorNS = spec.Namespace
		}
		// operator deployed on cluster, labels are set by the deployment
		if !areOperatorLabelsPropagated(operatorNS, ispnv1.OperatorPodTargetLabelsEnvVarName, pod.Labels) {
			panic("Operator labels haven't been propagated to pods")
		}
	}

	svcList := &corev1.ServiceList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(ispn.Namespace, map[string]string{"infinispan_cr": "test-node-startup"}, svcList, context.TODO()))
	if len(svcList.Items) == 0 {
		panic("No services found for cluster")
	}
	for _, svc := range svcList.Items {
		// from Infinispan CR to service
		if svc.Labels["my-svc-label"] != ispn.Labels["my-svc-label"] {
			panic("Infinispan CR labels haven't been propagated to services")
		}

		// from operator environment
		if tutils.RunLocalOperator == "TRUE" {
			// running locally, labels are hardcoded and set by the testsuite
			if svc.Labels["operator-svc-label"] != "operator-svc-value" ||
				ispn.Labels["operator-svc-label"] != "operator-svc-value" {
				panic("Labels haven't been propagated to services")
			}
		} else {
			// Get the operator namespace from the env if it's different
			// from the testsuite one
			operatorNS := tutils.OperatorNamespace
			if operatorNS == "" {
				operatorNS = spec.Namespace
			}
			// operator deployed on cluster, labels are set by the deployment
			if !areOperatorLabelsPropagated(operatorNS, ispnv1.OperatorTargetLabelsEnvVarName, svc.Labels) {
				panic("Operator labels haven't been propagated to services")
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

// Test if the cluster is working correctly
func TestClusterFormation(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube)
	spec.Spec.Replicas = 2

	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
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
