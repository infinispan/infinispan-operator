package logging

import (
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/templates"
)

type Spec struct {
	Categories map[string]string
}

func Generate(operand version.Operand, spec *Spec) (string, error) {
	v := operand.UpstreamVersion

	if v.Major < 13 && v.Major > 15 {
		return "", version.NewUnknownError(v)
	}
	return templates.LoadAndExecute("log4j.xml", nil, spec)
}
