package main

import (
	"os"
	"testing"

	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}
