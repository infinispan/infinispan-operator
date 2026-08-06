package v2alpha1

import (
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (cache *Cache) SetCondition(condition ConditionType, status metav1.ConditionStatus, message string) bool {
	changed, conditions := setCondition(cache.Status.Conditions, condition, status, message)
	cache.Status.Conditions = conditions
	return changed
}

func (cache *Cache) GetCondition(condition ConditionType) Condition {
	return getCondition(cache.Status.Conditions, condition)
}

func (cache *Cache) RemoveCondition(condition ConditionType) bool {
	changed, conditions := removeCondition(cache.Status.Conditions, condition)
	cache.Status.Conditions = conditions
	return changed
}

func (batch *Batch) GetCondition(condition ConditionType) Condition {
	return getCondition(batch.Status.Conditions, condition)
}

func (batch *Batch) SetCondition(condition ConditionType, status metav1.ConditionStatus, message string) bool {
	changed, conditions := setCondition(batch.Status.Conditions, condition, status, message)
	batch.Status.Conditions = conditions
	return changed
}

func (batch *Batch) RemoveCondition(condition ConditionType) bool {
	changed, conditions := removeCondition(batch.Status.Conditions, condition)
	batch.Status.Conditions = conditions
	return changed
}

func (backup *Backup) GetCondition(condition ConditionType) Condition {
	return getCondition(backup.Status.Conditions, condition)
}

func (backup *Backup) SetCondition(condition ConditionType, status metav1.ConditionStatus, message string) bool {
	changed, conditions := setCondition(backup.Status.Conditions, condition, status, message)
	backup.Status.Conditions = conditions
	return changed
}

func (backup *Backup) RemoveCondition(condition ConditionType) bool {
	changed, conditions := removeCondition(backup.Status.Conditions, condition)
	backup.Status.Conditions = conditions
	return changed
}

func (restore *Restore) GetCondition(condition ConditionType) Condition {
	return getCondition(restore.Status.Conditions, condition)
}

func (restore *Restore) SetCondition(condition ConditionType, status metav1.ConditionStatus, message string) bool {
	changed, conditions := setCondition(restore.Status.Conditions, condition, status, message)
	restore.Status.Conditions = conditions
	return changed
}

func (restore *Restore) RemoveCondition(condition ConditionType) bool {
	changed, conditions := removeCondition(restore.Status.Conditions, condition)
	restore.Status.Conditions = conditions
	return changed
}

func (cache *Cache) SetPausedCondition(message string) bool {
	return cache.SetCondition(ConditionReconciliationPaused, metav1.ConditionTrue, message)
}

func (cache *Cache) RemovePausedCondition() bool {
	return cache.RemoveCondition(ConditionReconciliationPaused)
}

func (batch *Batch) SetPausedCondition(message string) bool {
	return batch.SetCondition(ConditionReconciliationPaused, metav1.ConditionTrue, message)
}

func (batch *Batch) RemovePausedCondition() bool {
	return batch.RemoveCondition(ConditionReconciliationPaused)
}

func (backup *Backup) SetPausedCondition(message string) bool {
	return backup.SetCondition(ConditionReconciliationPaused, metav1.ConditionTrue, message)
}

func (backup *Backup) RemovePausedCondition() bool {
	return backup.RemoveCondition(ConditionReconciliationPaused)
}

func (restore *Restore) SetPausedCondition(message string) bool {
	return restore.SetCondition(ConditionReconciliationPaused, metav1.ConditionTrue, message)
}

func (restore *Restore) RemovePausedCondition() bool {
	return restore.RemoveCondition(ConditionReconciliationPaused)
}

func (cache *Cache) GetCacheName() string {
	if cache.Spec.Name != "" {
		return cache.Spec.Name
	}
	return cache.Name
}

func (b *Batch) ConfigMapName() string {
	if b.Spec.ConfigMap != nil {
		return *b.Spec.ConfigMap
	}
	return b.Name
}

// CpuResources returns the CPU request and limit values to be used by Batch pod
func (spec *BatchContainerSpec) CpuResources() (requests resource.Quantity, limits resource.Quantity, err error) {
	return v1.GetRequestLimits(spec.CPU)
}

// MemoryResources returns the Memory request and limit values to be used by by Batch pod
func (spec *BatchContainerSpec) MemoryResources() (requests resource.Quantity, limits resource.Quantity, err error) {
	return v1.GetRequestLimits(spec.Memory)
}
