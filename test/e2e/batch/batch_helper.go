package batch

import (
	"fmt"
	v2 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"time"
)

type BatchHelper struct {
	testKube *tutils.TestKubernetes
}

func NewBatchHelper(testKube *tutils.TestKubernetes) *BatchHelper {
	return &BatchHelper{
		testKube: testKube,
	}
}

func (b BatchHelper) CreateBatch(name, cluster string, config, configMap *string) *v2.Batch {
	batch := &v2.Batch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Batch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tutils.Namespace,
		},
		Spec: v2.BatchSpec{
			Cluster:   cluster,
			Config:    config,
			ConfigMap: configMap,
		},
	}
	batch.Labels = map[string]string{"test-name": name}
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
