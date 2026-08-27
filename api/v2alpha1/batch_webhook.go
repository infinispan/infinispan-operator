package v2alpha1

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (b *Batch) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(b).
		WithValidator(&BatchCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-batch,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=batches,verbs=create;update,versions=v2alpha1,name=vbatch.kb.io,admissionReviewVersions={v1,v1beta1}

// BatchCustomValidator validates Batch resources. It implements the
// webhook.CustomValidator interface.
// +kubebuilder:object:generate=false
type BatchCustomValidator struct{}

var _ webhook.CustomValidator = &BatchCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *BatchCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	b, ok := obj.(*Batch)
	if !ok {
		return nil, fmt.Errorf("expected a Batch object but got %T", obj)
	}

	var allErrs field.ErrorList
	if err := b.validate(); err != nil {
		return nil, err
	}
	if b.Spec.ConfigMap == nil && b.Spec.Config == nil {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("configMap"), "'Spec.config' OR 'spec.ConfigMap' must be configured"))
	} else if b.Spec.ConfigMap != nil && b.Spec.Config != nil {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("configMap"), "At most one of ['Spec.config', 'spec.ConfigMap'] must be configured"))
	}
	return nil, b.StatusError(allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *BatchCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	b, ok := newObj.(*Batch)
	if !ok {
		return nil, fmt.Errorf("expected a Batch object but got %T", newObj)
	}
	oldBatch, ok := oldObj.(*Batch)
	if !ok {
		return nil, fmt.Errorf("expected a Batch object but got %T", oldObj)
	}

	var allErrs field.ErrorList
	if err := b.validate(); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(b.Spec, oldBatch.Spec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"), "The Batch spec is immutable and cannot be updated after initial Batch creation"))
	}
	return nil, b.StatusError(allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (v *BatchCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil, nil
}

func (b *Batch) validate() error {
	var allErrs field.ErrorList
	if b.Spec.Container == nil {
		return nil
	}
	if b.Spec.Container.CPU != "" {
		req, limit, err := b.Spec.Container.CpuResources()
		if err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("cpu"), b.Spec.Container.CPU, err.Error()))
		}

		if req.Cmp(limit) > 0 {
			msg := fmt.Sprintf("CPU request '%s' exceeds limit '%s'", req.String(), limit.String())
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("cpu"), b.Spec.Container.CPU, msg))
		}
	}

	memReq, memLimit, err := b.Spec.Container.MemoryResources()
	if b.Spec.Container.Memory != "" {
		if err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("memory"), b.Spec.Container.Memory, err.Error()))
		}

		if memReq.Cmp(memLimit) > 0 {
			msg := fmt.Sprintf("Memory request '%s' exceeds limit '%s'", memReq.String(), memLimit.String())
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("memory"), b.Spec.Container.Memory, msg))
		}
	}
	return errorListToError(b, allErrs)
}

func errorListToError(b *Batch, allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Batch"},
			b.Name, allErrs)
	}
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
