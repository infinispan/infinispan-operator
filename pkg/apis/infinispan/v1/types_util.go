package v1

import "fmt"

// GetCondition return the Status of the given condition or nil
// if condition is not present
func (ispn *Infinispan) GetCondition(condition string) *string {
	for _, c := range ispn.Status.Conditions {
		if c.Type == condition {
			return &c.Status
		}
	}
	return nil
}

// SetCondition set condition to status
func (ispn *Infinispan) SetCondition(condition, status, message string) bool {
	changed := false
	for idx := range ispn.Status.Conditions {
		c := &ispn.Status.Conditions[idx]
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
	ispn.Status.Conditions = append(ispn.Status.Conditions, InfinispanCondition{Type: condition, Status: status, Message: message})
	return true
}

// SetConditions set provided conditions to status
func (ispn *Infinispan) SetConditions(conds []InfinispanCondition) bool {
	changed := false
	for _, c := range conds {
		changed = changed || ispn.SetCondition(c.Type, c.Status, c.Message)
	}
	return changed
}

// GetSecretName returns the secret name associated with a server
func (ispn *Infinispan) GetSecretName() string {
	if ispn.Spec.Security.EndpointSecretName == "" {
		return fmt.Sprintf("%v-generated-secret", ispn.GetName())
	}
	return ispn.Spec.Security.EndpointSecretName
}