package utils

import (
	"strings"

	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
)

func OperandAllowSet() map[string]struct{} {
	if OperandAllowList == "" {
		return make(map[string]struct{})
	}
	allowedVersions := strings.Split(OperandAllowList, ",")
	operands := make(map[string]struct{}, len(allowedVersions))
	for _, v := range allowedVersions {
		operands[v] = struct{}{}
	}
	return operands
}

func OperandSkipSet() map[string]struct{} {
	if OperandSkipList == "" {
		return make(map[string]struct{})
	}
	skippedVersions := strings.Split(OperandSkipList, ",")
	operands := make(map[string]struct{}, len(skippedVersions))
	for _, v := range skippedVersions {
		operands[v] = struct{}{}
	}
	return operands
}

func IsTestOperand(operand version.Operand) bool {
	skippedOperands := OperandSkipSet()
	if _, skip := skippedOperands[operand.Ref()]; skip {
		// Skip Operand upgrade if explicitly ignored
		Log().Infof("Operand %s is defined in the skip list", operand.Ref())
		return false
	}

	allowedOperands := OperandAllowSet()
	if _, allow := allowedOperands[operand.Ref()]; len(allowedOperands) > 0 && !allow {
		// Only upgrade to Operands explicitly allowed if a range is defined
		Log().Infof("Operand %s is not defined in the allow list", operand.Ref())
		return false
	}

	return true
}
