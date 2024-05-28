package v2alpha1

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetCondition set condition to status
func (cache *Cache) SetCondition(condition CacheConditionType, status metav1.ConditionStatus, message string) bool {
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

// GetCondition return the Status of the given condition or nil if condition is not present
func (cache *Cache) GetCondition(condition CacheConditionType) CacheCondition {
	for _, c := range cache.Status.Conditions {
		if c.Type.equals(condition) {
			return c
		}
	}
	// Absence of condition means `False` value
	return CacheCondition{Type: condition, Status: metav1.ConditionFalse}
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

// equals compares two ConditionType's case insensitive
func (a CacheConditionType) equals(b CacheConditionType) bool {
	return strings.EqualFold(strings.ToLower(string(a)), strings.ToLower(string(b)))
}
