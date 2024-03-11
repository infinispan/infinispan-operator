package webhook

import (
	"context"
	"errors"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	ispnv2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	testifyAssert "github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	ctx      = context.Background()
	testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)

// This test is to ensure that the Webhooks are deployed correctly with OLM deployments. Webhook semantic tests should
// be written using Envtest in the api/ packages
func TestMain(m *testing.M) {
	olm := testKube.OLMTestEnv()
	olm.PrintManifest()

	testKube.NewNamespace(tutils.Namespace)
	// Install Operator via Subscription
	sub := &coreos.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: coreos.SubscriptionCRDAPIVersion,
			Kind:       coreos.SubscriptionKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      olm.SubName,
			Namespace: olm.SubNamespace,
		},
		Spec: &coreos.SubscriptionSpec{
			Channel:                olm.PackageManifest.DefaultChannelName,
			CatalogSource:          olm.CatalogSource,
			CatalogSourceNamespace: olm.CatalogSourceNamespace,
			Package:                olm.SubPackage,
			Config: coreos.SubscriptionConfig{
				Env: []corev1.EnvVar{{
					Name:  ispnv1.OperatorTargetLabelsEnvVarName,
					Value: "{\"svc-label\":\"svc-value\"}",
				}, {
					Name:  ispnv1.OperatorPodTargetLabelsEnvVarName,
					Value: "{\"pod-label\":\"pod-value\"}",
				}, {
					Name:  ispnv1.OperatorTargetAnnotationsEnvVarName,
					Value: "{\"svc-annotation\":\"svc-value\"}",
				}, {
					Name:  ispnv1.OperatorPodTargetAnnotationsEnvVarName,
					Value: "{\"pod-annotation\":\"pod-value\"}",
				}, {
					Name:  "THREAD_DUMP_PRE_STOP",
					Value: "TRUE",
				}},
			},
		},
	}
	// Defer in case a panic is encountered by one of the tests
	defer testKube.CleanupOLMTest(nil, "webhook_test_main_defer", olm.SubName, olm.SubNamespace, olm.SubPackage)

	testKube.CreateSubscriptionAndApproveInitialVersion(olm, sub)
	testKube.WaitForDeployment("infinispan-operator-controller-manager", olm.SubNamespace)

	// We must ensure that the Operator pods have the defined environment variables available
	// https://github.com/operator-framework/operator-lifecycle-manager/issues/2725
	testKube.WaitForPods(1, tutils.SinglePodTimeout, &client.ListOptions{
		Namespace:     olm.SubNamespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{"app.kubernetes.io/name": "infinispan-operator"}),
	}, func(pods []corev1.Pod) bool {
		for _, pod := range pods {
			for _, env := range pod.Spec.Containers[0].Env {
				if env.Name == ispnv1.OperatorTargetLabelsEnvVarName {
					return true
				}
			}
		}
		return false
	})

	code := m.Run()
	testKube.CleanupOLMTest(nil, "webhook_test_main", olm.SubName, olm.SubNamespace, olm.SubPackage)
	os.Exit(code)
}

func TestInfinispanDefaultingWebhook(t *testing.T) {
	t.Parallel()
	ispn := &ispnv1.Infinispan{
		ObjectMeta: metav1.ObjectMeta{
			Name:   strcase.ToKebab(t.Name()),
			Labels: map[string]string{"test-name": t.Name()},
		},
	}
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	createdIspn := testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionPrelimChecksPassed)

	assert := testifyAssert.New(t)
	assert.Equal(ispnv1.ServiceTypeCache, createdIspn.Spec.Service.Type)
	assert.Equal("pod-value", createdIspn.ObjectMeta.Labels["pod-label"], "Operator labels haven't been propagated to CR")
	assert.Equal("svc-value", createdIspn.ObjectMeta.Labels["svc-label"], "Operator labels haven't been propagated to CR")
	assert.Equal("pod-value", createdIspn.Annotations["pod-annotation"], "Operator annotations haven't been propagated to CR")
	assert.Equal("svc-value", createdIspn.Annotations["svc-annotation"], "Operator annotations haven't been propagated to CR")
}

func TestInfinispanValidatingWebhook(t *testing.T) {
	t.Parallel()
	ispn := &ispnv1.Infinispan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Labels:    map[string]string{"test-name": t.Name()},
			Namespace: tutils.Namespace,
		},
		Spec: ispnv1.InfinispanSpec{
			Container: ispnv1.InfinispanContainerSpec{
				Memory: "invalid-value",
			},
		},
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	err := testKube.Kubernetes.Client.Create(ctx, ispn)
	assertInvalidErr(testifyAssert.New(t), err)
}

func TestBatchValidatingWebhook(t *testing.T) {
	t.Parallel()
	batch := &ispnv2.Batch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	err := testKube.Kubernetes.Client.Create(ctx, batch)
	assertInvalidErr(testifyAssert.New(t), err)
}

func TestCacheDefaultingWebhook(t *testing.T) {
	t.Parallel()
	cache := &ispnv2.Cache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
		Spec: ispnv2.CacheSpec{
			AdminAuth: &ispnv2.AdminAuth{
				SecretName: "some-secret",
			},
			ClusterName: "some-cluster",
		},
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	testKube.Create(cache)

	createdCache := &ispnv2.Cache{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(ctx, types.NamespacedName{Name: cache.Name, Namespace: cache.Namespace}, createdCache))
	testifyAssert.New(t).Nil(createdCache.Spec.AdminAuth)
}

func TestCacheValidatingWebhook(t *testing.T) {
	t.Parallel()
	cache := &ispnv2.Cache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	err := testKube.Kubernetes.Client.Create(ctx, cache)
	assertInvalidErr(testifyAssert.New(t), err)
}

func TestBackupDefaultingWebhook(t *testing.T) {
	t.Parallel()
	backup := &ispnv2.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
		Spec: ispnv2.BackupSpec{
			Cluster: "some-cluster",
		},
	}
	testKube.Create(backup)
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	createdBackup := &ispnv2.Backup{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: backup.Namespace}, createdBackup))
	testifyAssert.New(t).Equal(constants.DefaultMemorySize.String(), createdBackup.Spec.Container.Memory)
}

func TestBackupValidatingWebhook(t *testing.T) {
	t.Parallel()
	backup := &ispnv2.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	err := testKube.Kubernetes.Client.Create(ctx, backup)
	assertInvalidErr(testifyAssert.New(t), err)
}

func TestRestoreDefaultingWebhook(t *testing.T) {
	t.Parallel()
	restore := &ispnv2.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
		Spec: ispnv2.RestoreSpec{
			Cluster: "some-cluster",
			Backup:  "some-backup",
		},
	}
	testKube.Create(restore)
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	createdRestore := &ispnv2.Restore{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(ctx, types.NamespacedName{Name: restore.Name, Namespace: restore.Namespace}, createdRestore))
	testifyAssert.New(t).Equal(constants.DefaultMemorySize.String(), createdRestore.Spec.Container.Memory)
}

func TestRestoreValidatingWebhook(t *testing.T) {
	t.Parallel()
	restore := &ispnv2.Restore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	err := testKube.Kubernetes.Client.Create(ctx, restore)
	assertInvalidErr(testifyAssert.New(t), err)
}

func TestXSiteDefaultingWebhook(t *testing.T) {
	t.Parallel()
	ispn := &ispnv1.Infinispan{
		ObjectMeta: metav1.ObjectMeta{
			Name:   strcase.ToKebab(t.Name()),
			Labels: map[string]string{"test-name": t.Name()},
		},
		Spec: ispnv1.InfinispanSpec{
			Service: ispnv1.InfinispanServiceSpec{
				Type: ispnv1.ServiceTypeDataGrid,
				Sites: &ispnv1.InfinispanSitesSpec{
					Local: ispnv1.InfinispanSitesLocalSpec{
						Name: "local",
						Expose: ispnv1.CrossSiteExposeSpec{
							Type: ispnv1.CrossSiteExposeTypeClusterIP,
						},
					},
				},
			},
		},
	}
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	createdIspn := testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionPrelimChecksPassed)

	assert := testifyAssert.New(t)
	assert.Equal(ispnv1.GossipRouterType, createdIspn.Spec.Service.Sites.Local.Discovery.Type)
	assert.Equal(true, *createdIspn.Spec.Service.Sites.Local.Discovery.LaunchGossipRouter)
	assert.Equal(false, createdIspn.Spec.Service.Sites.Local.Discovery.SuspectEvents)
	assert.Equal(true, *createdIspn.Spec.Service.Sites.Local.Discovery.Heartbeats.Enabled)
	assert.Equal(int64(10000), *createdIspn.Spec.Service.Sites.Local.Discovery.Heartbeats.Interval)
	assert.Equal(int64(30000), *createdIspn.Spec.Service.Sites.Local.Discovery.Heartbeats.Timeout)
}

func TestXSiteNegativeHeartbeatIntervalWebhook(t *testing.T) {
	t.Parallel()
	heartbeatValidtionTest(t, pointer.Int64(-1), nil)
}

func TestXSiteZeroHeartbeatIntervalWebhook(t *testing.T) {
	t.Parallel()
	heartbeatValidtionTest(t, pointer.Int64(0), nil)
}

func TestXSiteNegativeHeartbeatTimeoutWebhook(t *testing.T) {
	t.Parallel()
	heartbeatValidtionTest(t, nil, pointer.Int64(-1))
}

func TestXSiteZeroHeartbeatTimeoutWebhook(t *testing.T) {
	t.Parallel()
	heartbeatValidtionTest(t, nil, pointer.Int64(0))
}

func TestXSiteSameHeartbeatIntervalAndTimeoutWebhook(t *testing.T) {
	t.Parallel()
	heartbeatValidtionTest(t, pointer.Int64(10), pointer.Int64(10))
}

func TestXSiteHigherHeartbeatIntervalThanTimeoutWebhook(t *testing.T) {
	t.Parallel()
	heartbeatValidtionTest(t, pointer.Int64(11), pointer.Int64(10))
}

func heartbeatValidtionTest(t *testing.T, interval, timeout *int64) {
	ispn := &ispnv1.Infinispan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(t.Name()),
			Labels:    map[string]string{"test-name": t.Name()},
			Namespace: tutils.Namespace,
		},
		Spec: ispnv1.InfinispanSpec{
			Service: ispnv1.InfinispanServiceSpec{
				Type: ispnv1.ServiceTypeDataGrid,
				Sites: &ispnv1.InfinispanSitesSpec{
					Local: ispnv1.InfinispanSitesLocalSpec{
						Name: "local",
						Expose: ispnv1.CrossSiteExposeSpec{
							Type: ispnv1.CrossSiteExposeTypeClusterIP,
						},
						Discovery: &ispnv1.DiscoverySiteSpec{
							Heartbeats: &ispnv1.GossipRouterHeartbeatSpec{
								Interval: interval,
								Timeout:  timeout,
							},
						},
					},
				},
			},
		},
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	err := testKube.Kubernetes.Client.Create(ctx, ispn)
	assertInvalidErr(testifyAssert.New(t), err)
}

func assertInvalidErr(assert *testifyAssert.Assertions, err error) {
	var statusError *k8serrors.StatusError
	assert.True(errors.As(err, &statusError))
	errStatus := statusError.ErrStatus
	assert.Equal(metav1.StatusReasonInvalid, errStatus.Reason)
	assert.Equal("Failure", errStatus.Status)
}
