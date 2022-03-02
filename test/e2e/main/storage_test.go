package main

import (
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
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
	if *testKube.GetPVC(pvcName, spec.Namespace).Spec.StorageClassName != defaultStorageClass {
		tutils.ExpectNoError(fmt.Errorf("persistent volume claim (%s) was created, but not bound to the cluster's default storage class (%s)",
			pvcName, defaultStorageClass))
	}
}
