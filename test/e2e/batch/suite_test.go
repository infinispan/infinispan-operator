package batch

import (
	"testing"

	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
)

// Executes tests that have potential to find regression in new OpenShift versions
func TestOpenShiftIntegration(t *testing.T) {
	// Prevent execution unless explicitly configured
	if !tutils.SuiteMode {
		t.SkipNow()
	}

	// Smoke tests
	t.Run("TestBatchInlineConfig", TestBatchInlineConfig)
	t.Run("TestBatchConfigMap", TestBatchConfigMap)
}
