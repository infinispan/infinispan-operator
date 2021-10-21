package utils

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
)

func CloseHttpResponse(t *testing.T, rsp *http.Response) {
	_, err := io.Copy(ioutil.Discard, rsp.Body)
	require.NoError(t, err)
	require.NoError(t, rsp.Body.Close())
}

func LogError(err error) {
	if err != nil {
		fmt.Printf("Encountered error: %v", err)
	}
}

// ExpectNoError checks if an error exists, and if so halts execution
func PanicOnError(err error) {
	if err != nil {
		panic(err.Error())
	}
}

// PanicOnErrorExpectNotFound ignores errors related to a resource not being found.
// Any other type of errors halts execution.
func PanicOnErrorExpectNotFound(err error) {
	if err != nil && !errors.IsNotFound(err) {
		panic(err)
	}
}

// ExpectMaybeNotFound ignores errors related to a resource not being found.
// Any other type of error fails test immediately.
func ExpectMaybeNotFound(t *testing.T, err error) {
	if err != nil {
		require.True(t, errors.IsNotFound(err))
	}
}

func ExpectNotFound(t *testing.T, err error) {
	require.Error(t, err, "Expected NotFound error")
	require.True(t, errors.IsNotFound(err), "Unexpected error: %w", err)
}
