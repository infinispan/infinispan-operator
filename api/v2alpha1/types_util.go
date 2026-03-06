package v2alpha1

import (
	"strings"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

// SetCondition set condition to status
func (s *Schema) SetCondition(condition SchemaConditionType, status metav1.ConditionStatus, message string) bool {
	changed := false
	for idx := range s.Status.Conditions {
		c := &s.Status.Conditions[idx]
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
	s.Status.Conditions = append(s.Status.Conditions, SchemaCondition{Type: condition, Status: status, Message: message})
	return true
}

// GetCondition return the Status of the given condition or nil if condition is not present
func (s *Schema) GetCondition(condition SchemaConditionType) SchemaCondition {
	for _, c := range s.Status.Conditions {
		if strings.EqualFold(string(c.Type), string(condition)) {
			return c
		}
	}
	// Absence of condition means `False` value
	return SchemaCondition{Type: condition, Status: metav1.ConditionFalse}
}

func (s *Schema) GetSchemaName() string {
	name := s.Spec.Name
	if name == "" {
		name = s.Name
	}
	if !strings.HasSuffix(name, ".proto") {
		name = name + ".proto"
	}
	return name
}

// CpuResources returns the CPU request and limit values to be used by Batch pod
func (spec *BatchContainerSpec) CpuResources() (requests resource.Quantity, limits resource.Quantity, err error) {
	return v1.GetRequestLimits(spec.CPU)
}

// MemoryResources returns the Memory request and limit values to be used by by Batch pod
func (spec *BatchContainerSpec) MemoryResources() (requests resource.Quantity, limits resource.Quantity, err error) {
	return v1.GetRequestLimits(spec.Memory)
}
