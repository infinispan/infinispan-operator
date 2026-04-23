package infinispan

import (
	"fmt"
	"os"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestAdditionalVarsUpdated(t *testing.T) {
	// The Operator must be running locall in order for changes to the ADDITIONAL_VAR env
	// variable to take effect
	if tutils.RunLocalOperator != "TRUE" {
		t.SkipNow()
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	val1 := "value1"
	tutils.ExpectNoError(os.Setenv("ADDITIONAL_VARS", "[\"VAR1\"]"))
	tutils.ExpectNoError(os.Setenv("VAR1", val1))

	spec := tutils.DefaultSpec(t, testKube, nil)
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
	podList := testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	assert.Equal(t, val1, getEnvVar("VAR1", &podList.Items[0].Spec))

	// Update the ADDITIONAL_VARS
	val2 := "value2"
	tutils.ExpectNoError(os.Setenv("ADDITIONAL_VARS", "[\"VAR1\", \"VAR2\"]"))
	tutils.ExpectNoError(os.Setenv("VAR2", val2))
	// Add an annotation to the Infinispan CR to trigger a new reconciliation that would not ordernarily cause a
	// StatefulSet rolling upgrade. This is necessary to simulate the restarting of the Operator Pod by OLM when the
	// ADDITIONAL_VARS env is updated in a Subscription
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Annotations = map[string]string{
			"ignore": "me",
		}
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		assert.Equal(t, val1, getEnvVar("VAR1", &ss.Spec.Template.Spec))
		assert.Equal(t, val2, getEnvVar("VAR2", &ss.Spec.Template.Spec))
	}
	verifyStatefulSetUpdate(*spec, modifier, verifier)
}

// TestEnvVarPropagation verifies that:
// 1. Literal EnvVars from the Infinispan CR are correctly synced to Pods.
// 2. Changes to ValueFrom references update the pod template hash, triggering a StatefulSet rollout.
func TestEnvVarsPropagation(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	testEnvName := "TEST_ENV"
	testEnvValue := "test_value"
	testEnvFromName := "FROM_VALUE_ENV"

	getEnvVars := func(fieldPath string) []corev1.EnvVar {
		return []corev1.EnvVar{
			{
				Name:  testEnvName,
				Value: testEnvValue,
			},
			{
				Name: testEnvFromName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: fieldPath,
					},
				},
			},
		}
	}

	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Container.Env = getEnvVars("metadata.namespace")
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	podList := testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	assert.Equal(t, testEnvValue, getEnvVar(testEnvName, &podList.Items[0].Spec))
	assert.Equal(t, "metadata.namespace", getValueFromFieldPath(testEnvFromName, &podList.Items[0].Spec))

	// Update the EnvFromValue, if the value is propagated the hash has been updated
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.Env = getEnvVars("spec.serviceAccountName")
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		assert.Equal(t, testEnvValue, getEnvVar(testEnvName, &ss.Spec.Template.Spec))
		assert.Equal(t, "spec.serviceAccountName", getValueFromFieldPath(testEnvFromName, &ss.Spec.Template.Spec))
	}
	verifyStatefulSetUpdate(*spec, modifier, verifier)
}

func getEnvVar(env string, podSpec *corev1.PodSpec) string {
	container := kube.GetContainer(provision.InfinispanContainer, podSpec)
	for _, e := range container.Env {
		if env == e.Name {
			return e.Value
		}
	}
	panic(fmt.Sprintf("ENV '%s' not found in container", env))
}

func getValueFromFieldPath(env string, podSpec *corev1.PodSpec) string {
	container := kube.GetContainer(provision.InfinispanContainer, podSpec)
	for _, e := range container.Env {
		if env == e.Name {
			return e.ValueFrom.FieldRef.FieldPath
		}
	}
	panic(fmt.Sprintf("ENV '%s' not found in container", env))
}
