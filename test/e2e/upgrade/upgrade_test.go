package upgrade

import (
	"context"
	"fmt"
	"os"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	batchtest "github.com/infinispan/infinispan-operator/test/e2e/batch"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	ctx              = context.TODO()
	testKube         = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
	conditionTimeout = 2 * tutils.ConditionWaitTimeout
)

func TestUpgrade(t *testing.T) {
	olm := testKube.OLMTestEnv()
	olm.PrintManifest()
	sourceChannel := olm.SourceChannel
	targetChannel := olm.TargetChannel

	testKube.NewNamespace(tutils.Namespace)
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
			Channel:                olm.SourceChannel.Name,
			CatalogSource:          olm.CatalogSource,
			CatalogSourceNamespace: olm.CatalogSourceNamespace,
			InstallPlanApproval:    coreos.ApprovalManual,
			Package:                olm.SubPackage,
			StartingCSV:            olm.SubStartingCSV,
		},
	}

	defer testKube.CleanupOLMTest(t, tutils.TestName(t), olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateSubscriptionAndApproveInitialVersion(olm, sub)

	// Create the Infinispan CR
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Service.Container.EphemeralStorage = false
		// Ensure that FIPS is disabled when testing 13.0.x Operand
		i.Spec.Container.CliExtraJvmOpts = "-Dcom.redhat.fips=false"
		i.Spec.Container.ExtraJvmOpts = "-Dcom.redhat.fips=false"
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Add a persistent cache with data to ensure contents can be read after upgrade(s)
	numEntries := 100
	client := tutils.HTTPClientForCluster(spec, testKube)
	cacheName := "someCache"

	cache := tutils.NewCacheHelper(cacheName, client)
	config := `{"distributed-cache":{"mode":"SYNC", "persistence":{"file-store":{}}}}`
	cache.Create(config, mime.ApplicationJson)
	cache.Populate(numEntries)
	cache.AssertSize(numEntries)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(targetChannel.Name, sub)
	}

	// Approve InstallPlans and verify cluster state on each upgrade until the most recent CSV has been reached
	for testKube.Subscription(sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		fmt.Printf("Installed csv: %s, Current CSV: %s\n", sub.Status.InstalledCSV, targetChannel.CurrentCSVName)
		ispnPreUpgrade := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)
		testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
		testKube.ApproveInstallPlan(sub)

		testKube.WaitForSubscription(sub, func() bool {
			return sub.Status.InstalledCSV == sub.Status.CurrentCSV
		})

		assertOperandImage := func(expectedImage string) {
			pods := &corev1.PodList{}
			err := testKube.Kubernetes.ResourcesList(tutils.Namespace, spec.PodSelectorLabels(), pods, ctx)
			tutils.ExpectNoError(err)
			for _, pod := range pods.Items {
				if pod.Spec.Containers[0].Image != expectedImage {
					panic(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, expectedImage))
				}
			}
		}

		latestOperand := func() version.Operand {
			operandVersions := testKube.InstalledCSVEnv(ispnv1.OperatorOperandVersionEnvVarName, sub)
			if operandVersions == "" {
				panic(fmt.Sprintf("%s env empty, cannot continue", ispnv1.OperatorOperandVersionEnvVarName))
			}
			versionManager, err := version.ManagerFromJson(operandVersions)
			tutils.ExpectNoError(err)
			return versionManager.Latest()
		}

		if ispnPreUpgrade.Spec.Version == "" {
			// We're upgrading from an Infinispan version that does not have multi-operand support so expect cluster
			// GracefulShutdown upgrade to happen automatically
			testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionStopping, conditionTimeout)
			testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
			testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

			relatedImageJdk := testKube.InstalledCSVEnv("RELATED_IMAGE_OPENJDK", sub)
			if relatedImageJdk != "" {
				// The latest Operator version still doesn't support multi-operand so check that the RELATED_IMAGE_OPENJDK
				// image has been installed on all pods
				assertOperandImage(relatedImageJdk)
			} else {
				latestOperand := latestOperand()
				testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
					return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
						i.Status.Operand.Version == latestOperand.Ref() &&
						i.Status.Operand.Image == latestOperand.Image &&
						i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
				})
				assertOperandImage(latestOperand.Image)
			}
		} else {
			latestOperand := latestOperand()
			if ispnPreUpgrade.Spec.Version != latestOperand.Ref() {
				ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)
				tutils.ExpectNoError(
					testKube.UpdateInfinispan(ispn, func() {
						ispn.Spec.Version = latestOperand.Ref()
					}),
				)
				testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
					return !i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
						i.Status.Operand.Version == latestOperand.Ref() &&
						i.Status.Operand.Image == latestOperand.Image &&
						i.Status.Operand.Phase == ispnv1.OperandPhasePending
				})
				testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
				testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
					return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
						i.Status.Operand.Version == latestOperand.Ref() &&
						i.Status.Operand.Image == latestOperand.Image &&
						i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
				})
				assertOperandImage(latestOperand.Image)
			}
		}

		// Ensure that persistent cache entries have survived the upgrade(s)
		// Refresh the hostAddr and client as the url will change if NodePort is used.
		client = tutils.HTTPClientForCluster(spec, testKube)
		tutils.NewCacheHelper(cacheName, client).AssertSize(numEntries)
	}

	checkServicePorts(t, spec.Name)
	checkBatch(t, spec.Name)

	// Kill the first pod to ensure that the cluster can recover from failover after upgrade
	err := testKube.Kubernetes.Client.Delete(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name + "-0",
			Namespace: tutils.Namespace,
		},
	})
	tutils.ExpectNoError(err)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Ensure that persistent cache entries still contain the expected numEntries
	client = tutils.HTTPClientForCluster(spec, testKube)
	tutils.NewCacheHelper(cacheName, client).AssertSize(numEntries)
}

func checkServicePorts(t *testing.T, name string) {
	// Check two services are exposed and the admin service listens on its own port
	userPort := testKube.GetServicePorts(tutils.Namespace, name)
	assert.Equal(t, 1, len(userPort))
	assert.Equal(t, constants.InfinispanUserPort, int(userPort[0].Port))

	adminPort := testKube.GetAdminServicePorts(tutils.Namespace, name)
	assert.Equal(t, 1, len(adminPort))
	assert.Equal(t, constants.InfinispanAdminPort, int(adminPort[0].Port))
}

func checkBatch(t *testing.T, name string) {
	// Run a batch in the migrated cluster
	batchHelper := batchtest.NewBatchHelper(testKube)
	config := "create cache --template=org.infinispan.DIST_SYNC batch-cache"
	batchHelper.CreateBatch(t, name, name, &config, nil)
	batchHelper.WaitForValidBatchPhase(name, v2alpha1.BatchSucceeded)
}
