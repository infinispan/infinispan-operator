package backup_restore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/infinispan/infinispan-operator/test/e2e/utils"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

type clusterSpec func(t *testing.T, name string, clusterSize int) *v1.Infinispan

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func TestBackupRestore(t *testing.T) {
	testBackupRestore(t, datagridService, 2, 1000)
}

func TestBackupRestoreNoAuth(t *testing.T) {
	testBackupRestore(t, datagridServiceNoAuth, 1, 1)
}

func TestBackupRestoreTransformations(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	testName := tutils.TestName(t)
	clusterName := strcase.ToKebab(testName)
	namespace := tutils.Namespace

	infinispan := datagridService(t, clusterName, 1)
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(infinispan.Name, namespace, v1.ConditionWellFormed)

	backupName := "backup"
	backupSpec := backupSpec(testName, backupName, namespace, clusterName)
	backupSpec.Spec.Resources = &v2.BackupResources{
		CacheConfigs: []string{"some-config"},
		Scripts:      []string{"some-script"},
	}

	testKube.Create(backupSpec)

	tutils.ExpectNoError(wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		backup := testKube.GetBackup(backupName, namespace)

		// ISPN-12675 It's not possible to create templates via rest, so the backup will fail as the template and scripts don't exist.
		backupFailed := backup.Status.Phase == v2.BackupFailed
		specUpdated := backup.Spec.Resources.CacheConfigs == nil && len(backup.Spec.Resources.Templates) == 1
		specUpdated = specUpdated && backup.Spec.Resources.Scripts == nil && len(backup.Spec.Resources.Tasks) == 1
		return backupFailed && specUpdated, nil
	}))

	restoreName := "restore"
	restoreSpec := restoreSpec(testName, restoreName, namespace, backupName, clusterName)
	restoreSpec.Spec.Resources = &v2.RestoreResources{
		CacheConfigs: []string{"some-config"},
		Scripts:      []string{"some-script"},
	}
	testKube.Create(restoreSpec)

	tutils.ExpectNoError(wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		restore := testKube.GetRestore(restoreName, namespace)

		// ISPN-12675 The restore will fail as the backup could not complete successfully
		restoreFailed := restore.Status.Phase == v2.RestoreFailed
		specUpdated := restore.Spec.Resources.CacheConfigs == nil && len(restore.Spec.Resources.Templates) == 1
		specUpdated = specUpdated && restore.Spec.Resources.Scripts == nil && len(restore.Spec.Resources.Tasks) == 1
		return restoreFailed && specUpdated, nil
	}))
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
	client := utils.HTTPClientForCluster(infinispan, testKube)
	cacheName := "someCache"

	cache := tutils.NewCacheHelper(cacheName, client)
	if infinispan.Spec.Service.Type == v1.ServiceTypeCache {
		cache.CreateWithDefault()
	} else {
		config := "{\"distributed-cache\":{\"mode\":\"SYNC\", \"statistics\":\"true\"}}"
		cache.Create(config, mime.ApplicationJson)
	}
	cache.Populate(numEntries)
	cache.AssertSize(numEntries)

	// 3. Backup the cluster's content
	backupName := "backup"
	backupSpec := backupSpec(testName, backupName, namespace, sourceCluster)
	testKube.Create(backupSpec)

	// Ensure the backup pod has joined the cluster
	waitForValidBackupPhase(backupName, namespace, v2.BackupSucceeded)

	// Ensure that the backup pod has left the cluster, by checking a cluster pod's size
	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)

	// Validate the number of entries stored in the someCache backup file
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
				Image:   infinispan.ImageName(),
				Command: []string{"/bin/bash"},
				Args:    []string{"-c", cmd},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "backup-volume",
						MountPath: "/etc/backups",
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
	err := wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
		utils.ExpectMaybeNotFound(err)
		if pod.Status.Phase == corev1.PodFailed {
			return false, fmt.Errorf("Expected %d entries to be backed up for 'someCache'", numEntries)
		}
		return pod.Status.Phase == corev1.PodSucceeded, err
	})
	if err != nil {
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

	// Ensure the restore pod hased joined the cluster
	waitForValidRestorePhase(restoreName, namespace, v2.RestoreSucceeded)

	// Ensure that the restore pod has left the cluster, by checking a cluster pod's size
	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)

	// Recreate the cluster instance to use the credentials of the new cluster
	client = utils.HTTPClientForCluster(infinispan, testKube)

	// 7. Ensure that all data is in the target cluster
	tutils.NewCacheHelper(cacheName, client).AssertSize(numEntries)
}

func waitForValidBackupPhase(name, namespace string, phase v2.BackupPhase) {
	var backup *v2.Backup
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		backup = testKube.GetBackup(name, namespace)
		if backup.Status.Phase == v2.BackupFailed && phase != v2.BackupFailed {
			return true, fmt.Errorf("backup failed. Reason: %s", backup.Status.Reason)
		}
		return phase == backup.Status.Phase, nil
	})
	if err != nil {
		println(fmt.Sprintf("Expected Backup Phase %s, got %s:%s", phase, backup.Status.Phase, backup.Status.Reason))
	}
	tutils.ExpectNoError(err)
}

func waitForValidRestorePhase(name, namespace string, phase v2.RestorePhase) {
	var restore *v2.Restore
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		restore = testKube.GetRestore(name, namespace)
		if restore.Status.Phase == v2.RestoreFailed {
			return true, fmt.Errorf("restore failed. Reason: %s", restore.Status.Reason)
		}
		return phase == restore.Status.Phase, nil
	})
	if err != nil {
		println(fmt.Sprintf("Expected Restore Phase %s, got %s:%s", phase, restore.Status.Phase, restore.Status.Reason))
	}
	tutils.ExpectNoError(err)
}

func datagridServiceNoAuth(t *testing.T, name string, replicas int) *v1.Infinispan {
	infinispan := datagridService(t, name, replicas)
	infinispan.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	return infinispan
}

func datagridService(t *testing.T, name string, replicas int) *v1.Infinispan {
	return tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Name = name
		i.Spec.Replicas = int32(replicas)

		if tutils.CPU != "" {
			i.Spec.Container.CPU = tutils.CPU
		}
	})
}

func backupSpec(testName, name, namespace, cluster string) *v2alpha1.Backup {
	spec := &v2.Backup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Backup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: v2.BackupSpec{
			Cluster: cluster,
		},
	}
	if tutils.CPU != "" {
		spec.Spec.Container.CPU = tutils.CPU
	}
	return spec
}

func restoreSpec(testName, name, namespace, backup, cluster string) *v2alpha1.Restore {
	spec := &v2.Restore{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Restore",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: v2.RestoreSpec{
			Backup:  backup,
			Cluster: cluster,
		},
	}
	if tutils.CPU != "" {
		spec.Spec.Container.CPU = tutils.CPU
	}
	return spec
}

func waitForNoCluster(infinispan *v1.Infinispan) {
	statefulSet := &appsv1.StatefulSet{}
	namespacedName := types.NamespacedName{Namespace: tutils.Namespace, Name: infinispan.GetStatefulSetName()}
	err := wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		e := testKube.Kubernetes.Client.Get(context.Background(), namespacedName, statefulSet)
		return e != nil && k8errors.IsNotFound(e), nil
	})
	tutils.ExpectNoError(err)
}
