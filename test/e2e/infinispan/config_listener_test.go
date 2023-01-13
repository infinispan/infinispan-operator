package infinispan

import (
	"testing"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestConfigListenerDeployment(t *testing.T) {
	// t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: true,
			Logging: v1.ConfigListenerLoggingDebug,
		}
	})

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, v1.ConditionWellFormed)

	// Wait for ConfigListener Deployment to be created
	clName, namespace := ispn.GetConfigListenerName(), ispn.Namespace
	testKube.WaitForDeployment(clName, namespace)

	waitForNoConfigListener := func() {
		err := wait.Poll(tutils.ConditionPollPeriod, tutils.ConditionWaitTimeout, func() (bool, error) {
			exists := testKube.AssertK8ResourceExists(clName, namespace, &appsv1.Deployment{}) &&
				testKube.AssertK8ResourceExists(clName, namespace, &rbacv1.Role{}) &&
				testKube.AssertK8ResourceExists(clName, namespace, &rbacv1.RoleBinding{}) &&
				testKube.AssertK8ResourceExists(clName, namespace, &corev1.ServiceAccount{})
			return !exists, nil
		})
		tutils.ExpectNoError(err)
	}

	// Ensure that the deployment is deleted if the spec is updated
	err := testKube.UpdateInfinispan(ispn, func() {
		ispn.Spec.ConfigListener.Enabled = false
	})
	tutils.ExpectNoError(err)
	waitForNoConfigListener()

	// Re-add the ConfigListener to ensure that it's removed when the Infinispan CR is finally deleted
	err = testKube.UpdateInfinispan(ispn, func() {
		ispn.Spec.ConfigListener.Enabled = true
	})
	tutils.ExpectNoError(err)
	testKube.WaitForDeployment(clName, namespace)

	// Update the ConfigListener log level to ensure that the deployment is updated
	ispn = testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, v1.ConditionWellFormed)
	err = testKube.UpdateInfinispan(ispn, func() {
		ispn.Spec.ConfigListener.Logging = v1.ConfigListenerLoggingInfo
	})
	tutils.ExpectNoError(err)
	testKube.WaitForDeploymentState(clName, namespace, func(deployment *appsv1.Deployment) bool {
		container := kube.GetContainer(provision.InfinispanListenerContainer, &deployment.Spec.Template.Spec)
		logLevel := container.Args[len(container.Args)-1]
		return deployment.Status.ObservedGeneration == 2 && logLevel == string(v1.ConfigListenerLoggingInfo)
	})

	// Ensure that deployment is deleted with the Infinispan CR
	testKube.DeleteInfinispan(ispn)
	waitForNoConfigListener()
}
