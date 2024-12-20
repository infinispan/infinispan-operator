package utils

import (
	"strings"
)

func OperandSkipSet() map[string]struct{} {
	skippedVersions := strings.Split(OperandSkipList, ",")
	operands := make(map[string]struct{}, len(skippedVersions))
	for _, v := range skippedVersions {
		operands[v] = struct{}{}
	}
	return operands
}
