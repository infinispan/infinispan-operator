package v2alpha1

import (
	"context"
	"fmt"
	"reflect"

	"github.com/infinispan/infinispan-operator/controllers/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (r *Restore) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithDefaulter(&RestoreCustomDefaulter{}).
		WithValidator(&RestoreCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-restore,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=restores,verbs=create;update,versions=v2alpha1,name=mrestore.kb.io,admissionReviewVersions={v1,v1beta1}

// RestoreCustomDefaulter applies defaults to Restore resources. It implements the
// webhook.CustomDefaulter interface.
// +kubebuilder:object:generate=false
type RestoreCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &RestoreCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (d *RestoreCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	r, ok := obj.(*Restore)
	if !ok {
		return fmt.Errorf("expected a Restore object but got %T", obj)
	}

	if r.Spec.Container.Memory == "" {
		r.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
	resources := r.Spec.Resources
	if resources == nil {
		return nil
	}

	if len(resources.CacheConfigs) > 0 {
		resources.Templates = resources.CacheConfigs
		resources.CacheConfigs = nil
	}

	if len(resources.Scripts) > 0 {
		resources.Tasks = resources.Scripts
		resources.Scripts = nil
	}
	return nil
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-restore,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=restores,verbs=create;update,versions=v2alpha1,name=vrestore.kb.io,admissionReviewVersions={v1,v1beta1}

// RestoreCustomValidator validates Restore resources. It implements the
// webhook.CustomValidator interface.
// +kubebuilder:object:generate=false
type RestoreCustomValidator struct{}

var _ webhook.CustomValidator = &RestoreCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *RestoreCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	r, ok := obj.(*Restore)
	if !ok {
		return nil, fmt.Errorf("expected a Restore object but got %T", obj)
	}

	var allErrs field.ErrorList
	if r.Spec.Cluster == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("cluster"), "'spec.cluster' must be configured"))
	}
	if r.Spec.Backup == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("backup"), "'spec.backup' must be configured"))
	}
	return nil, r.StatusError(allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *RestoreCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	r, ok := newObj.(*Restore)
	if !ok {
		return nil, fmt.Errorf("expected a Restore object but got %T", newObj)
	}
	oldRestore, ok := oldObj.(*Restore)
	if !ok {
		return nil, fmt.Errorf("expected a Restore object but got %T", oldObj)
	}

	var allErrs field.ErrorList
	if !reflect.DeepEqual(r.Spec, oldRestore.Spec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"), "The Restore spec is immutable and cannot be updated after initial Restore creation"))
	}
	return nil, r.StatusError(allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (v *RestoreCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil, nil
}

func (r *Restore) StatusError(allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Restore"},
			r.Name, allErrs)
	}
	return nil
}
