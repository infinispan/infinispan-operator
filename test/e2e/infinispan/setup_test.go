package infinispan

import (
	"os"
	"testing"

	"github.com/go-logr/zapr"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	ctrl "sigs.k8s.io/controller-runtime"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

func TestMain(m *testing.M) {
	ctrl.SetLogger(zapr.NewLogger(tutils.Log().Desugar()))
	tutils.RunOperator(m, testKube)
}
