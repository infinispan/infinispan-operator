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
	switch v.Major {
	case 13:
		return templates.LoadAndExecute("log4j.xml", nil, spec)
	default:
		return "", version.UnknownError(v)
	}
}
