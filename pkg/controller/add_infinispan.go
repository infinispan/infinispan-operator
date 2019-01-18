package controller

import (
	"github.com/rigazilla/infinispan-operator/pkg/controller/infinispan"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, infinispan.Add)
}
