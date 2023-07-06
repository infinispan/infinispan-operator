package provision

import (
	"fmt"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	cryostatv1beta1 "github.com/infinispan/infinispan-operator/pkg/apis/cryostat/v1beta1"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func Cryostat(i *v1.Infinispan, ctx pipeline.Context) {
	if !ctx.IsTypeSupported(pipeline.CryostatGVK) {
		return
	}

	cryostat := &cryostatv1beta1.Cryostat{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cryostatv1beta1.GroupVersion.String(),
			Kind:       "Cryostat",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetCryostatName(),
			Namespace: i.Namespace,
		},
	}

	mutateFn := func() error {
		cryostat.Spec.Minimal = false
		cryostat.Spec.EnableCertManager = pointer.Bool(true)
		return controllerutil.SetOwnerReference(i, cryostat, ctx.Kubernetes().Client.Scheme())
	}

	if _, err := ctx.Resources().CreateOrUpdate(cryostat, false, mutateFn); err != nil {
		ctx.Requeue(fmt.Errorf("unable to createOrUpdate Cryostat: %w", err))
	}
}
