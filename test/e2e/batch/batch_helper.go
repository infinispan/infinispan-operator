package batch

import (
	"fmt"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	batchCtrl "github.com/infinispan/infinispan-operator/controllers"
	corev1 "k8s.io/api/core/v1"
	"testing"
	"time"

	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

type BatchHelper struct {
	testKube *tutils.TestKubernetes
}

func NewBatchHelper(testKube *tutils.TestKubernetes) *BatchHelper {
	return &BatchHelper{
		testKube: testKube,
	}
}

func (b BatchHelper) CreateBatch(t *testing.T, name, cluster string, config, configMap *string, containerSpec *v2.BatchContainerSpec) *v2.Batch {
	testName := tutils.TestName(t)
	batch := &v2.Batch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Batch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: v2.BatchSpec{
			Cluster:   cluster,
			Config:    config,
			ConfigMap: configMap,
			Container: containerSpec,
		},
	}
	b.testKube.Create(batch)
	return batch
}

func (b BatchHelper) WaitForValidBatchPhase(name string, phase v2.BatchPhase) *v2.Batch {
	var batch *v2.Batch
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		batch = b.testKube.GetBatch(name, tutils.Namespace)
		if batch.Status.Phase == v2.BatchFailed && phase != v2.BatchFailed {
			return true, fmt.Errorf("Batch failed. Reason: %s", batch.Status.Reason)
		}
		return phase == batch.Status.Phase, nil
	})
	if err != nil {
		println(fmt.Sprintf("Expected Batch Phase %s, got %s:%s", phase, batch.Status.Phase, batch.Status.Reason))
	}
	tutils.ExpectNoError(err)
	return batch
}

func (b BatchHelper) CreateBatchCM(infinispan *ispnv1.Infinispan) *corev1.ConfigMap {
	configMapName := infinispan.Name + "-cm"
	data := map[string]string{
		batchCtrl.BatchFilename: "create cache --file=/etc/batch/mycache.xml mycache",
		"mycache.xml":           "<distributed-cache />",
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: infinispan.Namespace,
		},
		Data: data,
	}

	b.testKube.CreateConfigMap(configMap)
	return configMap
}
