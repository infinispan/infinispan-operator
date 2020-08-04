package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CopyWithDefaultsForEmptyVals return a copy of with defaults in place of empty fields
func (c *Cache) CopyWithDefaultsForEmptyVals() *Cache {
	ret := c.DeepCopy()
	if ret.Spec.AdminAuth.Password.Key == "" {
		ret.Spec.AdminAuth.Password.Key = "password"
	}
	if ret.Spec.AdminAuth.Username.Key == "" {
		ret.Spec.AdminAuth.Username.Key = "username"
	}
	if ret.Spec.AdminAuth.Password.Name == "" {
		ret.Spec.AdminAuth.Password.Name = c.Spec.AdminAuth.SecretName
	}
	if ret.Spec.AdminAuth.Username.Name == "" {
		ret.Spec.AdminAuth.Username.Name = c.Spec.AdminAuth.SecretName
	}
	return ret
}

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
