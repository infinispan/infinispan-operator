package kubernetes

import (
	"context"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// FindStorageClass finds an existed storage class
func FindStorageClass(name string, client crclient.Client, ctx context.Context) (*storagev1.StorageClass, error) {
	storageClass := &storagev1.StorageClass{}
	err := client.Get(ctx, types.NamespacedName{Name: name}, storageClass)
	return storageClass, err
}
