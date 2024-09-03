package utils

import (
	"fmt"
	"testing"

	"github.com/blang/semver"
	"k8s.io/apimachinery/pkg/api/errors"
)

func LogError(err error) {
	if err != nil {
		fmt.Printf("Encountered error: %v\n", err)
	}
}

// ExpectNoError checks if an error exists, and if so halts execution
func ExpectNoError(err error) {
	if err != nil {
		panic(err.Error())
	}
}

// ExpectMaybeNotFound ignores errors related to a resource not being found.
// Any other type of error halts execution.
func ExpectMaybeNotFound(err error) {
	if err != nil && !errors.IsNotFound(err) {
		panic(err.Error())
	}
}

func ExpectNotFound(err error) {
	if err == nil {
		panic("Expected NotFound error")
	}

	if !errors.IsNotFound(err) {
		panic(fmt.Errorf("unexpected error: %w", err))
	}
}

func SkipForMajor(t *testing.T, infinispanMajor uint64, message string) {
	if OperandVersion != "" {
		operand, _ := VersionManager().WithRef(OperandVersion)
		if operand.UpstreamVersion.Major == infinispanMajor {
			t.Skip(message)
		}
	}
}

func IsVersionAtLeast(version string) bool {
	if OperandVersion != "" {
		operand_cur, _ := VersionManager().WithRef(OperandVersion)
		version_min, _ := semver.Parse(version)
		return operand_cur.UpstreamVersion.GE(version_min)
	}

	// Assume yes if OperandVersion isn't specified
	return true
}

func SkipPriorTo(t *testing.T, version, message string) {
	if !IsVersionAtLeast(version) {
		t.Skip(message)
	}
}

func SkipForAWS(t *testing.T, message string) {
	if Infrastructure == "AWS" {
		t.Skip(message)
	}
}

func SkipForOpenShift(t *testing.T, message string) {
	if Platform == "OpenShift" {
		t.Skip(message)
	}
}
