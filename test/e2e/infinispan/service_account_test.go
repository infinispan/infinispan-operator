package infinispan

import (
	"context"
	"testing"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestServiceAccountName(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	saName := "custom-ispn-sa"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: tutils.Namespace,
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), sa))
	defer testKube.DeleteServiceAccount(sa)

	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ServiceAccountName = saName
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, v1.ConditionWellFormed)

	assert := assert.New(t)
	require := require.New(t)

	ss := testKube.GetStatefulSet(ispn.GetStatefulSetName(), ispn.Namespace)
	assert.Equal(saName, ss.Spec.Template.Spec.ServiceAccountName)

	pod := corev1.Pod{}
	require.NoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: ispn.Name + "-0", Namespace: tutils.Namespace}, &pod))
	assert.Equal(saName, pod.Spec.ServiceAccountName)

	// Verify custom ServiceAccount is not deleted when Infinispan CR is deleted
	testKube.DeleteInfinispan(ispn)

	saAfterDelete := &corev1.ServiceAccount{}
	err := testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: saName, Namespace: tutils.Namespace}, saAfterDelete)
	assert.NoError(err, "custom ServiceAccount should not be deleted when Infinispan CR is deleted")
}

func TestServiceAccountNameUpdate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	saName := "updated-ispn-sa"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: tutils.Namespace,
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), sa))
	defer testKube.DeleteServiceAccount(sa)

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
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	clSaName := "custom-cl-sa"
	clRoleName := "custom-cl-role"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clSaName,
			Namespace: tutils.Namespace,
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), sa))
	defer testKube.DeleteServiceAccount(sa)

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clRoleName,
			Namespace: tutils.Namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"infinispan.org"},
				Resources: []string{"caches"},
				Verbs:     []string{"create", "delete", "get", "list", "patch", "update", "watch"},
			},
			{
				APIGroups: []string{"infinispan.org"},
				Resources: []string{"infinispans"},
				Verbs:     []string{"get"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/exec"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get"},
			},
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), role))
	defer testKube.DeleteRole(role)

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clRoleName,
			Namespace: tutils.Namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     clRoleName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      clSaName,
			Namespace: tutils.Namespace,
		}},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), roleBinding))
	defer testKube.DeleteRoleBinding(roleBinding)

	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled:            true,
			ServiceAccountName: clSaName,
		}
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, v1.ConditionWellFormed)

	assert := assert.New(t)

	clName := ispn.GetConfigListenerName()
	deployment := testKube.WaitForDeployment(clName, ispn.Namespace)
	container := kube.GetContainer(provision.InfinispanListenerContainer, &deployment.Spec.Template.Spec)
	assert.NotNil(container)
	assert.Equal(clSaName, deployment.Spec.Template.Spec.ServiceAccountName)

	// Verify operator did NOT create auto-managed RBAC resources
	autoSA := &corev1.ServiceAccount{}
	err := testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: clName, Namespace: ispn.Namespace}, autoSA)
	assert.True(k8serrors.IsNotFound(err), "auto-created ServiceAccount should not exist when user provides their own")

	autoRole := &rbacv1.Role{}
	err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: clName, Namespace: ispn.Namespace}, autoRole)
	assert.True(k8serrors.IsNotFound(err), "auto-created Role should not exist when user provides their own SA")

	autoRoleBinding := &rbacv1.RoleBinding{}
	err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: clName, Namespace: ispn.Namespace}, autoRoleBinding)
	assert.True(k8serrors.IsNotFound(err), "auto-created RoleBinding should not exist when user provides their own SA")
}

func TestServiceAccountBatch(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	saName := "batch-ispn-sa"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: tutils.Namespace,
		},
	}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Create(context.TODO(), sa))
	defer testKube.DeleteServiceAccount(sa)

	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ServiceAccountName = saName
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, v1.ConditionWellFormed)

	batchScript := "create cache --template org.infinispan.DIST_SYNC batch-sa-cache"
	batch := &v2.Batch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Batch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: tutils.Namespace,
		},
		Spec: v2.BatchSpec{
			Cluster: spec.Name,
			Config:  &batchScript,
		},
	}
	testKube.Create(batch)
	defer testKube.DeleteBatch(batch)

	// Wait for the Batch Job to be created and verify it uses the custom SA
	var job batchv1.Job
	err := wait.PollUntilContextTimeout(context.Background(), tutils.DefaultPollPeriod, tutils.SinglePodTimeout, false, func(ctx context.Context) (bool, error) {
		return testKube.AssertK8ResourceExists(spec.Name, tutils.Namespace, &job), nil
	})
	require.NoError(t, err, "Batch Job should be created")
	assert.Equal(t, saName, job.Spec.Template.Spec.ServiceAccountName)
}
