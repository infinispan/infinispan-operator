package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetCondition set condition to status
func (cache *Cache) SetCondition(condition string, status metav1.ConditionStatus, message string) bool {
	changed := false
	for idx := range cache.Status.Conditions {
		c := &cache.Status.Conditions[idx]
		if c.Type == condition {
			if c.Status != status {
				c.Status = status
				changed = true
			}
			if c.Message != message {
				c.Message = message
				changed = true
			}

			return changed
		}
	}
	cache.Status.Conditions = append(cache.Status.Conditions, CacheCondition{Type: condition, Status: status, Message: message})
	return true
}

func (cache *Cache) GetCacheName() string {
	cacheName := cache.Name
	if cache.Spec.Name != "" {
		cacheName = cache.Spec.Name
	}
	return cacheName
}
