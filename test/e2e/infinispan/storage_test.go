package infinispan

import (
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	testifyRequire "github.com/stretchr/testify/require"
)

// Test if single node with a storage class
func TestNodeWithStorageClass(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube)

	// Get the default StorageClasses name in cluster
	defaultStorageClass := testKube.GetDefaultStorageClass()
	spec.Spec.Service.Container.EphemeralStorage = false
	spec.Spec.Service.Container.StorageClassName = defaultStorageClass

	// Register above created resource
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure a PVCs is bound from defaultStorageClass
	pvcName := "data-volume-test-node-with-storage-class-0"
	storageClassName := testKube.GetPVC(pvcName, spec.Namespace).Spec.StorageClassName

	require := testifyRequire.New(t)
	if defaultStorageClass == "" {
		require.Nil(storageClassName, "StorageClassName should be empty")
	} else {
		require.Equal(defaultStorageClass, *storageClassName, "StorageClassName should use default storage class")
	}
}
