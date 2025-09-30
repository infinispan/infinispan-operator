package utils

import (
	"strings"
)

func OperandAllowSet() map[string]struct{} {
	allowedVersions := strings.Split(OperandAllowList, ",")
	operands := make(map[string]struct{}, len(allowedVersions))
	for _, v := range allowedVersions {
		operands[v] = struct{}{}
	}
	return operands
}

func OperandSkipSet() map[string]struct{} {
	skippedVersions := strings.Split(OperandSkipList, ",")
	operands := make(map[string]struct{}, len(skippedVersions))
	for _, v := range skippedVersions {
		operands[v] = struct{}{}
	}
	return operands
}
