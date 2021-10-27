package backup_restore

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
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

type clusterSpec func(name, namespace string, clusterSize int) *v1.Infinispan

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func TestBackupRestore(t *testing.T) {
	testBackupRestore(t, datagridService, 2)
}

func TestBackupRestoreNoAuth(t *testing.T) {
	testBackupRestore(t, datagridServiceNoAuth, 1)
}

func TestBackupRestoreTransformations(t *testing.T) {
	// Create a resource without passing any config
	clusterName := strcase.ToKebab(t.Name())
	namespace := tutils.Namespace

	infinispan := datagridService(clusterName, namespace, 1)
	testKube.Create(infinispan)
	defer testKube.CleanNamespaceAndLogOnPanic(namespace, nil)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(infinispan.Name, namespace, v1.ConditionWellFormed)

	backupName := "backup"
	backupSpec := &v2.Backup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Backup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupName,
			Namespace: namespace,
		},
		Spec: v2.BackupSpec{
			Cluster: clusterName,
			Resources: &v2.BackupResources{
				CacheConfigs: []string{"some-config"},
				Scripts:      []string{"some-script"},
			},
		},
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
	restoreSpec := &v2.Restore{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Restore",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreName,
			Namespace: namespace,
		},
		Spec: v2.RestoreSpec{
			Backup:  backupName,
			Cluster: clusterName,
			Resources: &v2.RestoreResources{
				CacheConfigs: []string{"some-config"},
				Scripts:      []string{"some-script"},
			},
		},
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

func testBackupRestore(t *testing.T, clusterSpec clusterSpec, clusterSize int) {
	// Create a resource without passing any config
	name := strcase.ToKebab(t.Name())
	namespace := tutils.Namespace
	numEntries := 10000

	// 1. Create initial source cluster
	sourceCluster := name + "-source"
	infinispan := clusterSpec(sourceCluster, namespace, clusterSize)
	testKube.Create(infinispan)
	defer testKube.CleanNamespaceAndLogOnPanic(namespace, nil)
	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(sourceCluster, namespace, v1.ConditionWellFormed)

	// 2. Populate the cluster with some data to backup
	hostAddr, client := utils.HTTPClientAndHost(infinispan, testKube)
	cacheName := "someCache"
	populateCache(cacheName, hostAddr, numEntries, infinispan, client)
	assertNumEntries(cacheName, hostAddr, numEntries, client)

	// 3. Backup the cluster's content
	backupName := "backup"
	backupSpec := &v2.Backup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Backup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupName,
			Namespace: namespace,
		},
		Spec: v2.BackupSpec{
			Cluster: sourceCluster,
		},
	}
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
	waitForNoCluster(sourceCluster)

	// 5. Create a new cluster to restore the backup to
	targetCluster := name + "-target"
	infinispan = clusterSpec(targetCluster, namespace, clusterSize)
	testKube.Create(infinispan)

	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(targetCluster, namespace, v1.ConditionWellFormed)

	// Recreate the cluster instance to use the credentials of the new cluster
	hostAddr, client = utils.HTTPClientAndHost(infinispan, testKube)

	// 6. Restore the backed up data from the volume to the target cluster
	restoreName := "restore"
	restoreSpec := &v2.Restore{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Restore",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      restoreName,
		},
		Spec: v2.RestoreSpec{
			Cluster: targetCluster,
			Backup:  backupName,
		},
	}

	testKube.Create(restoreSpec)

	// Ensure the restore pod hased joined the cluster
	waitForValidRestorePhase(restoreName, namespace, v2.RestoreSucceeded)

	// Ensure that the restore pod has left the cluster, by checking a cluster pod's size
	testKube.WaitForInfinispanPods(clusterSize, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)

	// 7. Ensure that all data is in the target cluster
	assertNumEntries(cacheName, hostAddr, numEntries, client)
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

func datagridServiceNoAuth(name, namespace string, replicas int) *v1.Infinispan {
	infinispan := datagridService(name, namespace, replicas)
	infinispan.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	return infinispan
}

func datagridService(name, namespace string, replicas int) *v1.Infinispan {
	infinispan := cacheService(name, namespace, replicas)
	infinispan.Spec.Service = v1.InfinispanServiceSpec{
		Type: v1.ServiceTypeDataGrid,
	}
	return infinispan
}

func cacheService(name, namespace string, replicas int) *v1.Infinispan {
	tutils.DefaultSpec(testKube)
	return &v1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.InfinispanSpec{
			Replicas: int32(replicas),
			Expose:   tutils.ExposeServiceSpec(testKube),
		},
	}
}

func populateCache(cacheName, host string, numEntries int, infinispan *v1.Infinispan, client tutils.HTTPClient) {
	post := func(url, payload string, status int, headers map[string]string) {
		rsp, err := client.Post(url, payload, headers)
		tutils.ExpectNoError(err)
		defer tutils.CloseHttpResponse(rsp)
		if rsp.StatusCode != status {
			panic(fmt.Sprintf("Unexpected response code %d", rsp.StatusCode))
		}
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if infinispan.Spec.Service.Type == v1.ServiceTypeCache {
		url := fmt.Sprintf("%s/rest/v2/caches/%s?template=default", host, cacheName)
		post(url, "", http.StatusOK, nil)
	} else {
		url := fmt.Sprintf("%s/rest/v2/caches/%s", host, cacheName)
		config := "{\"distributed-cache\":{\"mode\":\"SYNC\", \"statistics\":\"true\"}}"
		post(url, config, http.StatusOK, headers)
	}

	client.Quiet(true)
	for i := 0; i < numEntries; i++ {
		url := fmt.Sprintf("%s/rest/v2/caches/%s/%d", host, cacheName, i)
		value := fmt.Sprintf("{\"value\":\"%d\"}", i)
		post(url, value, http.StatusNoContent, headers)
	}
	client.Quiet(false)
}

func assertNumEntries(cacheName, host string, numEntries int, client tutils.HTTPClient) {
	url := fmt.Sprintf("%s/rest/v2/caches/%s?action=size", host, cacheName)
	err := wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		rsp, err := client.Get(url, nil)
		tutils.ExpectNoError(err)
		if rsp.StatusCode != http.StatusOK {
			panic(fmt.Sprintf("Unexpected response code %d", rsp.StatusCode))
		}

		body, err := ioutil.ReadAll(rsp.Body)
		tutils.ExpectNoError(rsp.Body.Close())
		tutils.ExpectNoError(err)
		numRead, err := strconv.ParseInt(string(body), 10, 64)
		return int(numRead) == numEntries, err
	})
	tutils.ExpectNoError(err)
}

func waitForNoCluster(name string) {
	statefulSet := &appsv1.StatefulSet{}
	namespacedName := types.NamespacedName{Namespace: tutils.Namespace, Name: name}
	err := wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		e := testKube.Kubernetes.Client.Get(context.Background(), namespacedName, statefulSet)
		return e != nil && k8errors.IsNotFound(e), nil
	})
	tutils.ExpectNoError(err)
}
