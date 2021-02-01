package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
)

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
		panic(fmt.Errorf("Unexpected error: %w", err))
	}
}
