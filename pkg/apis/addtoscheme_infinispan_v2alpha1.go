package apis

import (
	"github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, v2alpha1.SchemeBuilder.AddToScheme)
}
