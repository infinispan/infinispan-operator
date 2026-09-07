package backup_restore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/blang/semver"
	"github.com/go-logr/zapr"
	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	v2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

type clusterSpec func(t *testing.T, name string, clusterSize int) *v1.Infinispan

func TestMain(m *testing.M) {
	ctrl.SetLogger(zapr.NewLogger(tutils.Log().Desugar()))
	tutils.RunOperator(m, testKube)
}

func TestBackupRestore(t *testing.T) {
	testBackupRestore(t, datagridService, 2, 1000)
}

func TestBackupRestoreNoAuth(t *testing.T) {
	testBackupRestore(t, datagridServiceNoAuth, 1, 1)
}

// TestBackupRestoreSecurityContext verifies that the transient backup zero pod inherits both the
// pod-level and container-level securityContext.
func TestBackupRestoreSecurityContext(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	testName := tutils.TestName(t)
	name := strcase.ToKebab(testName)
	namespace := tutils.Namespace

	// Create a source cluster with a user-supplied securityContext override.
	sourceCluster := name + "-source"
	infinispan := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Name = sourceCluster
		i.Spec.Replicas = 1
		i.Spec.SecurityContext = &corev1.PodSecurityContext{SupplementalGroups: []int64{1000}}
		i.Spec.Container.SecurityContext = &corev1.SecurityContext{ReadOnlyRootFilesystem: ptr.To(false)}
	})
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(sourceCluster, namespace, v1.ConditionWellFormed)

	// Trigger a backup; the operator spins up a transient zero pod that joins the cluster.
	backupName := "backup"
	backupSpec := backupSpec(testName, backupName, namespace, sourceCluster)
	testKube.Create(backupSpec)

	// Capture the zero pod while it exists and verify the inherited securityContext.
	zeroPod := &corev1.Pod{}
	err := wait.PollUntilContextTimeout(context.Background(), tutils.DefaultPollPeriod, tutils.SinglePodTimeout, false, func(ctx context.Context) (bool, error) {
		podList := &corev1.PodList{}
		listErr := testKube.Kubernetes.Client.List(context.TODO(), podList, &client.ListOptions{
			Namespace:     namespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{"app": "infinispan-zero-pod"}),
		})
		if listErr != nil || len(podList.Items) == 0 {
			return false, nil
		}
		zeroPod = &podList.Items[0]
		return true, nil
	})
	tutils.ExpectNoError(err)

	// Pod-level securityContext: inherited override merged in, hardened defaults preserved.
	podCtx := zeroPod.Spec.SecurityContext
	if assert.NotNil(t, podCtx, "zero pod securityContext should be set") {
		assert.Equal(t, []int64{1000}, podCtx.SupplementalGroups)
		assert.Equal(t, ptr.To(true), podCtx.RunAsNonRoot)
	}

	// Container-level securityContext on the infinispan container: inherited override merged in, defaults preserved.
	var container *corev1.Container
	for i := range zeroPod.Spec.Containers {
		if zeroPod.Spec.Containers[i].Name == provision.InfinispanContainer {
			container = &zeroPod.Spec.Containers[i]
			break
		}
	}
	if assert.NotNil(t, container, "zero pod must contain the %s container", provision.InfinispanContainer) {
		sc := container.SecurityContext
		if assert.NotNil(t, sc, "zero pod container securityContext should be set") {
			assert.Equal(t, ptr.To(false), sc.ReadOnlyRootFilesystem)   // inherited override
			assert.Equal(t, ptr.To(false), sc.AllowPrivilegeEscalation) // default preserved
			if assert.NotNil(t, sc.Capabilities) {
				assert.Equal(t, []corev1.Capability{"ALL"}, sc.Capabilities.Drop)
			}
		}
	}
}

func testBackupRestore(t *testing.T, clusterSpec clusterSpec, clusterSize, numEntries int) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	testName := tutils.TestName(t)
	name := strcase.ToKebab(testName)
	namespace := tutils.Namespace

	// 1. Create initial source cluster
	sourceCluster := name + "-source"
	infinispan := clusterSpec(t, sourceCluster, clusterSize)
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(sourceCluster, namespace, v1.ConditionWellFormed)

	// 2. Populate the cluster with some data to backup
	client := tutils.HTTPClientForCluster(infinispan, testKube)
	cacheName := "someCache"

	cache := tutils.NewCacheHelper(cacheName, client)

	config := "{\"distributed-cache\":{\"mode\":\"SYNC\", \"statistics\":\"true\", \"encoding\": {\"media-type\": \"application/json\"}}}"
	cache.Create(config, mime.ApplicationJson)
	cache.Populate(numEntries)
	cache.AssertSize(numEntries)

	// 3. Backup the cluster's content
	backupName := "backup"
	backupSpec := backupSpec(testName, backupName, namespace, sourceCluster)
	testKube.Create(backupSpec)

	// Ensure the backup pod has joined the cluster
	testKube.WaitForValidBackupPhase(backupName, namespace, v2alpha1.BackupSucceeded)

	// Ensure that the backup pod has left the cluster, by checking a cluster pod's size
	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)

	// Retrieve the latest Infinispan so that the call to `ImageName()` has the operand initialized
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: sourceCluster}, infinispan))

	// Validate the number of entries stored in the someCache backup file
	// Utilise a 15.0.x image as this still contains the unzip package
	imageOp, err := tutils.VersionManager().LatestUpstreamPatch(semver.Version{Major: 15, Minor: 0})
	tutils.ExpectNoError(err)
	cmd := fmt.Sprintf("ls -l /etc/backups/backup; cd /tmp; unzip /etc/backups/backup/backup.zip; LINES=$(cat containers/default/caches/someCache/someCache.dat | wc -l); echo $LINES; [[ $LINES -eq \"%d\" ]]", numEntries)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "verify-backup-pod",
			Namespace: namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:    "verify",
				Image:   imageOp.Image,
				Command: []string{"/bin/bash"},
				Args:    []string{"-c", cmd},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "backup-volume",
						MountPath: "/etc/backups",
					},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{
							"ALL",
						},
					},
					RunAsNonRoot: ptr.To(true),
					SeccompProfile: &corev1.SeccompProfile{
						Type: "RuntimeDefault",
					},
				},
			}},
			RestartPolicy: corev1.RestartPolicyNever,
			Volumes: []corev1.Volume{
				{
					Name: "backup-volume",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: backupName,
						},
					},
				},
			},
		},
	}
	testKube.Create(pod)
	err = wait.PollUntilContextTimeout(context.Background(), tutils.DefaultPollPeriod, tutils.SinglePodTimeout, false, func(ctx context.Context) (done bool, err error) {
		err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
		tutils.ExpectMaybeNotFound(err)
		if pod.Status.Phase == corev1.PodFailed {
			return false, fmt.Errorf("expected %d entries to be backed up for 'someCache'", numEntries)
		}
		return pod.Status.Phase == corev1.PodSucceeded, err
	})
	if err != nil {
		if yaml_, e := yaml.Marshal(pod); e != nil {
			tutils.Log().Error(e, "Failed to marshal pod to yaml")
		} else {
			tutils.Log().Error(yaml_)
		}
		// Delete the pod to prevent issues with PVCs named "backup" in subsequent tests
		defer tutils.LogError(testKube.Kubernetes.Client.Delete(context.TODO(), pod))
		panic(err.Error())
	}
	tutils.LogError(testKube.Kubernetes.Client.Delete(context.TODO(), pod))

	// 4. Delete the original cluster
	testKube.DeleteInfinispan(infinispan)
	waitForNoCluster(infinispan)

	// 5. Create a new cluster to restore the backup to
	targetCluster := name + "-target"
	infinispan = clusterSpec(t, targetCluster, clusterSize)
	testKube.Create(infinispan)

	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(targetCluster, namespace, v1.ConditionWellFormed)

	// 6. Restore the backed up data from the volume to the target cluster
	restoreName := "restore"
	restoreSpec := restoreSpec(testName, restoreName, namespace, backupName, targetCluster)
	testKube.Create(restoreSpec)

	// Ensure the restore pod has joined the cluster
	err = testKube.WaitForValidRestorePhase(restoreName, namespace, v2alpha1.RestoreSucceeded)
	if err != nil {
		// Known issue with older operands that won't be fixed, skip the test to reduce noise in the test results
		if strings.Contains(err.Error(), "ISPN-15173") {
			tutils.SkipPriorTo(t, "14.0.18", "Known issue: "+err.Error())
		}
		panic(err.Error())
	}

	// Ensure that the restore pod has left the cluster, by checking a cluster pod's size
	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)

	// Recreate the cluster instance to use the credentials of the new cluster
	client = tutils.HTTPClientForCluster(infinispan, testKube)

	// 7. Ensure that all data is in the target cluster
	tutils.NewCacheHelper(cacheName, client).AssertSize(numEntries)
}

func datagridServiceNoAuth(t *testing.T, name string, replicas int) *v1.Infinispan {
	infinispan := datagridService(t, name, replicas)
	infinispan.Spec.Security.EndpointAuthentication = ptr.To(false)
	return infinispan
}

func datagridService(t *testing.T, name string, replicas int) *v1.Infinispan {
	return tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Name = name
		i.Spec.Replicas = int32(replicas)
	})
}

func backupSpec(testName, name, namespace, cluster string) *v2alpha1.Backup {
	spec := &v2alpha1.Backup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Backup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: v2alpha1.BackupSpec{
			Cluster: cluster,
		},
	}
	_ = (&v2alpha1.BackupCustomDefaulter{}).Default(context.TODO(), spec)
	return spec
}

func restoreSpec(testName, name, namespace, backup, cluster string) *v2alpha1.Restore {
	spec := &v2alpha1.Restore{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Restore",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: v2alpha1.RestoreSpec{
			Backup:  backup,
			Cluster: cluster,
		},
	}
	_ = (&v2alpha1.RestoreCustomDefaulter{}).Default(context.TODO(), spec)
	return spec
}

func waitForNoCluster(infinispan *v1.Infinispan) {
	statefulSet := &appsv1.StatefulSet{}
	namespacedName := types.NamespacedName{Namespace: tutils.Namespace, Name: infinispan.GetStatefulSetName()}
	err := wait.PollUntilContextTimeout(context.Background(), tutils.DefaultPollPeriod, tutils.SinglePodTimeout, false, func(ctx context.Context) (done bool, err error) {
		e := testKube.Kubernetes.Client.Get(context.Background(), namespacedName, statefulSet)
		return e != nil && k8errors.IsNotFound(e), nil
	})
	tutils.ExpectNoError(err)
}
