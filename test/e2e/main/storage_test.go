package main

import (
	"context"
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
)

// Test if single node with n ephemeral storage
func TestNodeWithEphemeralStorage(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube)
	spec.Spec.Service.Container = &ispnv1.InfinispanServiceContainerSpec{EphemeralStorage: true}

	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Making sure no PVCs were created
	pvcs := &corev1.PersistentVolumeClaimList{}
	err := testKube.Kubernetes.ResourcesList(spec.Namespace, controllers.PodLabels(spec.Name), pvcs, context.TODO())
	tutils.ExpectNoError(err)
	if len(pvcs.Items) > 0 {
		tutils.ExpectNoError(fmt.Errorf("persistent volume claims were found (count = %d) but not expected for ephemeral storage configuration", len(pvcs.Items)))
	}
}

// Test if single node with a storage class
func TestNodeWithStorageClass(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube)

	// Get the default StorageClasses name in cluster
	defaultStorageClass := testKube.GetDefaultStorageClass()
	spec.Spec.Service.Container = &ispnv1.InfinispanServiceContainerSpec{StorageClassName: defaultStorageClass}

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
