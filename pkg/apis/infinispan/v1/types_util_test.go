package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const namespace = "testing-namespace"

var exposeRouteInfinispan = &Infinispan{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "extra-long-cluster-name-d----------------------------d",
		Namespace: namespace,
	},
	Spec: InfinispanSpec{
		Expose: &ExposeSpec{
			Type: ExposeTypeRoute,
		},
	},
}

func TestServiceExternalName(t *testing.T) {
	assert.Equal(t, "extra-long-cluster-name-d-------------------a", exposeRouteInfinispan.GetServiceExternalName(), "Route expose long name")
	assert.LessOrEqual(t, MaxRouteObjectNameLength, len(exposeRouteInfinispan.GetServiceExternalName())+len(namespace)+1, "Route expose name length")

	exposeRouteInfinispan.Name = "example-infinispan"
	assert.Equal(t, "example-infinispan-external", exposeRouteInfinispan.GetServiceExternalName(), "Route expose name")
}
