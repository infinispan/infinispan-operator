package utils

import (
	"fmt"
	"io/ioutil"
	"net/http"

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

type HttpError struct {
	Status int
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("unexpected response %v", e.Status)
}

func ThrowHTTPError(resp *http.Response) {
	errorBytes, _ := ioutil.ReadAll(resp.Body)
	panic(fmt.Errorf("unexpected HTTP status code (%d): %s", resp.StatusCode, string(errorBytes)))
}
