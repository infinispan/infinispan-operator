package infinispan

import (
	"context"
	"testing"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestServiceAccountName(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	saName := "custom-ispn-sa"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: tutils.Namespace,
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), sa))

	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ServiceAccountName = saName
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, v1.ConditionWellFormed)

	assert := assert.New(t)
	require := require.New(t)

	// Verify StatefulSet PodSpec has the custom ServiceAccountName
	ss := testKube.GetStatefulSet(ispn.GetStatefulSetName(), ispn.Namespace)
	assert.Equal(saName, ss.Spec.Template.Spec.ServiceAccountName)

	// Verify the actual pod uses the custom ServiceAccountName
	pod := corev1.Pod{}
	require.NoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: ispn.Name + "-0", Namespace: tutils.Namespace}, &pod))
	assert.Equal(saName, pod.Spec.ServiceAccountName)
}

func TestServiceAccountNameUpdate(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	saName := "updated-ispn-sa"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: tutils.Namespace,
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), sa))

	spec := tutils.DefaultSpec(t, testKube, nil)

	var modifier = func(ispn *v1.Infinispan) {
		ispn.Spec.ServiceAccountName = saName
	}
	var verifier = func(ispn *v1.Infinispan, ss *appsv1.StatefulSet) {
		assert.Equal(t, saName, ss.Spec.Template.Spec.ServiceAccountName)
	}
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestConfigListenerServiceAccountName(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	clSaName := "custom-cl-sa"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clSaName,
			Namespace: tutils.Namespace,
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), sa))

	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled:            true,
			ServiceAccountName: clSaName,
		}
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, v1.ConditionWellFormed)

	assert := assert.New(t)

	// Verify ConfigListener Deployment uses the custom ServiceAccountName
	clName := ispn.GetConfigListenerName()
	deployment := testKube.WaitForDeployment(clName, ispn.Namespace)
	container := kube.GetContainer(provision.InfinispanListenerContainer, &deployment.Spec.Template.Spec)
	assert.NotNil(container)
	assert.Equal(clSaName, deployment.Spec.Template.Spec.ServiceAccountName)

	// Verify operator did NOT create auto-managed RBAC resources
	autoSA := &corev1.ServiceAccount{}
	err := testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: clName, Namespace: ispn.Namespace}, autoSA)
	assert.True(k8serrors.IsNotFound(err), "auto-created ServiceAccount should not exist when user provides their own")

	role := &rbacv1.Role{}
	err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: clName, Namespace: ispn.Namespace}, role)
	assert.True(k8serrors.IsNotFound(err), "auto-created Role should not exist when user provides their own SA")

	roleBinding := &rbacv1.RoleBinding{}
	err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: clName, Namespace: ispn.Namespace}, roleBinding)
	assert.True(k8serrors.IsNotFound(err), "auto-created RoleBinding should not exist when user provides their own SA")
}
