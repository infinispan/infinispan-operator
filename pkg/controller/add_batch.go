package controller

import (
	"github.com/infinispan/infinispan-operator/pkg/controller/batch"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, batch.Add)
}
