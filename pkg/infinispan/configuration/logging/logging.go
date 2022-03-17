package logging

import (
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/templates"
)

type Spec struct {
	Categories map[string]string
}

func Generate(v *version.Version, spec *Spec) (string, error) {
	if v == nil {
		v = &version.Version{Major: 13, Minor: 0, Patch: 0}
	}
	switch v.Major {
	case 13:
		return templates.LoadAndExecute("log4j.xml", nil, spec)
	default:
		return "", version.UnknownError(v)
	}
}
