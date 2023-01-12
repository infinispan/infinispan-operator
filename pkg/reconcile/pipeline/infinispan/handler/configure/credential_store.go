package configure

import (
	"fmt"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
)

func CredentialStoreSecret(i *ispnv1.Infinispan, ctx pipeline.Context) {
	secret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetCredentialStoreSecretName(), secret); err != nil {
		ctx.Requeue(fmt.Errorf("unable to load CredentialStore secret %s: %w", i.GetCredentialStoreSecretName(), err))
		return
	}
	ctx.ConfigFiles().CredentialStoreEntries = secret.Data
}
