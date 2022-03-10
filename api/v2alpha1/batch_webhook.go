package v2alpha1

import (
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (b *Batch) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(b).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-batch,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=batches,verbs=create;update,versions=v2alpha1,name=vbatch.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &Batch{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (b *Batch) ValidateCreate() error {
	var allErrs field.ErrorList
	if b.Spec.ConfigMap == nil && b.Spec.Config == nil {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("configMap"), "'Spec.config' OR 'spec.ConfigMap' must be configured"))
	} else if b.Spec.ConfigMap != nil && b.Spec.Config != nil {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("configMap"), "At most one of ['Spec.config', 'spec.ConfigMap'] must be configured"))
	}
	return b.StatusError(allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (b *Batch) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	oldBatch := old.(*Batch)
	if !reflect.DeepEqual(b.Spec, oldBatch.Spec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"), "The Batch spec is immutable and cannot be updated after initial Batch creation"))
	}
	return b.StatusError(allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (b *Batch) ValidateDelete() error {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil
}

func (b *Batch) StatusError(allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Batch"},
			b.Name, allErrs)
	}
	return nil
}
