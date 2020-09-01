package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	v2 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	cconsts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnclient "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	tconst "github.com/infinispan/infinispan-operator/test/e2e/constants"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
	k8errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type clusterSpec func(name, namespace string, clusterSize int) *v1.Infinispan

func TestBackupRestore(t *testing.T) {
	t.Run(string(v1.ServiceTypeDataGrid), testBackupRestore(datagridService))
	t.Run(string(v1.ServiceTypeCache), testBackupRestore(cacheService))
}

func testBackupRestore(clusterSpec clusterSpec) func(*testing.T) {
	return func(t *testing.T) {
		// Create a resource without passing any config
		name := strcase.ToKebab(strings.Replace(t.Name(), "/", "", 1))
		namespace := tconst.Namespace
		clusterSize := 2
		numEntries := 100

		// 1. Create initial source cluster
		sourceCluster := name + "-source"
		infinispan := clusterSpec(sourceCluster, namespace, clusterSize)
		testKube.Create(infinispan)
		defer testKube.DeleteInfinispan(infinispan, tconst.SinglePodTimeout)
		waitForPodsOrFail(infinispan, clusterSize)

		// 2. Populate the cluster with some data to backup
		protocol := getSchemaForRest(infinispan)
		cluster := newCluster(cconsts.DefaultOperatorUser, infinispan.GetSecretName(), protocol, testKube.Kubernetes)
		cacheName := "someCache"
		populateCache(cacheName, sourceCluster+"-0", numEntries, cluster.Client)

		// 3. Backup the cluster's content
		backupSpec := &v2.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: v2.BackupSpec{
				Cluster: sourceCluster,
			},
		}
		testKube.Create(backupSpec)
		defer testKube.DeleteBackup(backupSpec)

		// Ensure the backup pod hased joined the cluster
		waitForValidBackupPhase(name, namespace, v2.BackupSucceeded)

		// Ensure that the backup pod has left the cluster, by checking a cluster pod's size
		waitForPodsOrFail(infinispan, clusterSize)

		// 4. Delete the original cluster
		testKube.DeleteInfinispan(infinispan, tconst.SinglePodTimeout)
		waitForNoCluster(sourceCluster)

		// 5. Create a new cluster to restore the backup to
		targetCluster := name + "-target"
		infinispan = clusterSpec(targetCluster, namespace, clusterSize)
		testKube.Create(infinispan)
		defer testKube.DeleteInfinispan(infinispan, tconst.SinglePodTimeout)
		waitForPodsOrFail(infinispan, clusterSize)

		// Recreate the cluster instance to use the credentials of the new cluster
		cluster = newCluster(cconsts.DefaultOperatorUser, infinispan.GetSecretName(), protocol, testKube.Kubernetes)

		// 6. Restore the backed up data from the volume to the target cluster
		restoreSpec := &v2.Restore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: v2.RestoreSpec{
				Cluster: targetCluster,
				Backup:  name,
			},
		}

		testKube.Create(restoreSpec)
		defer testKube.DeleteRestore(restoreSpec)

		// Ensure the restore pod hased joined the cluster
		waitForValidRestorePhase(name, namespace, v2.RestoreSucceeded)

		// Ensure that the restore pod has left the cluster, by checking a cluster pod's size
		waitForPodsOrFail(infinispan, clusterSize)

		// 7. Ensure that all data is in the target cluster
		assertNumEntries(cacheName, targetCluster+"-0", numEntries, cluster.Client)
	}
}

func waitForValidBackupPhase(name, namespace string, phase v2.BackupPhase) {
	var backup *v2.Backup
	err := wait.Poll(10*time.Millisecond, tconst.TestTimeout, func() (bool, error) {
		backup = testKube.GetBackup(name, namespace)
		if backup.Status.Phase == v2.BackupFailed && phase != v2.BackupFailed {
			return true, errors.New(fmt.Sprintf("backup failed. Reason: %s", backup.Status.Reason))
		}
		return phase == backup.Status.Phase, nil
	})
	if err != nil {
		println(fmt.Sprintf("Expected Backup Phase %s, got %s:%s", phase, backup.Status.Phase, backup.Status.Reason))
	}
	tutils.ExpectNoError(err)
}

func waitForValidRestorePhase(name, namespace string, phase v2.RestorePhase) {
	err := wait.Poll(10*time.Millisecond, tconst.TestTimeout, func() (bool, error) {
		restore := testKube.GetRestore(name, namespace)
		if restore.Status.Phase == v2.RestoreFailed {
			return true, errors.New(fmt.Sprintf("restore failed. Reason: %s", restore.Status.Reason))
		}
		return phase == restore.Status.Phase, nil
	})
	tutils.ExpectNoError(err)
}

func datagridService(name, namespace string, replicas int) *v1.Infinispan {
	infinispan := cacheService(name, namespace, replicas)
	infinispan.Spec.Service = v1.InfinispanServiceSpec{
		Type: v1.ServiceTypeDataGrid,
	}
	return infinispan
}

func cacheService(name, namespace string, replicas int) *v1.Infinispan {
	return &v1.Infinispan{
		TypeMeta: tconst.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.InfinispanSpec{
			Replicas: int32(replicas),
		},
	}
}

func populateCache(cacheName, pod string, numEntries int, client ispnclient.HttpClient) {
	headers := map[string]string{"Content-Type": "application/json"}

	post := func(url, payload string, status int) {
		rsp, err, _ := client.Post(pod, url, payload, headers)
		tutils.ExpectNoError(err)
		if rsp.StatusCode != status {
			panic(fmt.Sprintf("Unexpected response code %d", rsp.StatusCode))
		}
	}

	url := fmt.Sprintf("/rest/v2/caches/%s", cacheName)
	config := fmt.Sprintf("{\"distributed-cache\":{\"name\":\"%s\"}}", cacheName)
	post(url, config, http.StatusOK)

	for i := 0; i < numEntries; i++ {
		url = fmt.Sprintf("/rest/v2/caches/%s/%d", cacheName, i)
		value := fmt.Sprintf("{\"value\":\"%d\"}", i)
		post(url, value, http.StatusNoContent)
	}
}

func assertNumEntries(cacheName, pod string, numEntries int, client ispnclient.HttpClient) {
	url := fmt.Sprintf("/rest/v2/caches/%s?action=size", cacheName)
	rsp, err, _ := client.Get(pod, url, nil)

	tutils.ExpectNoError(err)
	if rsp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Unexpected response code %d", rsp.StatusCode))
	}
}

func waitForNoCluster(name string) {
	statefulSet := &appsv1.StatefulSet{}
	namespacedName := types.NamespacedName{Namespace: tconst.Namespace, Name: name}
	err := wait.Poll(tconst.DefaultPollPeriod, tconst.SinglePodTimeout, func() (done bool, err error) {
		e := testKube.Kubernetes.Client.Get(context.Background(), namespacedName, statefulSet)
		return e != nil && k8errors.IsNotFound(e), nil
	})
	tutils.ExpectNoError(err)
}
