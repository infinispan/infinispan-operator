package v2alpha1

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConditionType string

const (
	ConditionReady                ConditionType = "Ready"
	ConditionReconciliationPaused ConditionType = "ReconciliationPaused"
)

// Condition defines a status condition for a resource.
type Condition struct {
	// Type is the type of the condition.
	Type ConditionType `json:"type"`
	// Status is the status of the condition.
	Status metav1.ConditionStatus `json:"status"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

func (a ConditionType) equals(b ConditionType) bool {
	return strings.EqualFold(string(a), string(b))
}

func setCondition(conditions []Condition, condType ConditionType, status metav1.ConditionStatus, message string) (bool, []Condition) {
	for idx := range conditions {
		c := &conditions[idx]
		if c.Type.equals(condType) {
			changed := false
			if c.Status != status {
				c.Status = status
				changed = true
			}
			if c.Message != message {
				c.Message = message
				changed = true
			}
			return changed, conditions
		}
	}
	return true, append(conditions, Condition{Type: condType, Status: status, Message: message})
}

func removeCondition(conditions []Condition, condType ConditionType) (bool, []Condition) {
	for idx := range conditions {
		if conditions[idx].Type.equals(condType) {
			return true, append(conditions[:idx], conditions[idx+1:]...)
		}
	}
	return false, conditions
}

func getCondition(conditions []Condition, condType ConditionType) Condition {
	for _, c := range conditions {
		if c.Type.equals(condType) {
			return c
		}
	}
	return Condition{Type: condType, Status: metav1.ConditionFalse}
}
