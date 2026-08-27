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

func (b *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(b).
		WithDefaulter(&BackupCustomDefaulter{}).
		WithValidator(&BackupCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-backup,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=backups,verbs=create;update,versions=v2alpha1,name=mbackup.kb.io,admissionReviewVersions={v1,v1beta1}

// BackupCustomDefaulter applies defaults to Backup resources. It implements the
// webhook.CustomDefaulter interface.
// +kubebuilder:object:generate=false
type BackupCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &BackupCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (d *BackupCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	b, ok := obj.(*Backup)
	if !ok {
		return fmt.Errorf("expected a Backup object but got %T", obj)
	}

	if b.Spec.Container.Memory == "" {
		b.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
	resources := b.Spec.Resources
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

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-backup,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=backups,verbs=create;update,versions=v2alpha1,name=vbackup.kb.io,admissionReviewVersions={v1,v1beta1}

// BackupCustomValidator validates Backup resources. It implements the
// webhook.CustomValidator interface.
// +kubebuilder:object:generate=false
type BackupCustomValidator struct{}

var _ webhook.CustomValidator = &BackupCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *BackupCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	b, ok := obj.(*Backup)
	if !ok {
		return nil, fmt.Errorf("expected a Backup object but got %T", obj)
	}

	var allErrs field.ErrorList
	if b.Spec.Cluster == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("cluster"), "'spec.cluster' must be configured"))
	}
	return nil, b.StatusError(allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *BackupCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	b, ok := newObj.(*Backup)
	if !ok {
		return nil, fmt.Errorf("expected a Backup object but got %T", newObj)
	}
	oldBackup, ok := oldObj.(*Backup)
	if !ok {
		return nil, fmt.Errorf("expected a Backup object but got %T", oldObj)
	}

	var allErrs field.ErrorList
	if !reflect.DeepEqual(b.Spec, oldBackup.Spec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"), "The Backup spec is immutable and cannot be updated after initial Backup creation"))
	}
	return nil, b.StatusError(allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (v *BackupCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil, nil
}

func (b *Backup) StatusError(allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Backup"},
			b.Name, allErrs)
	}
	return nil
}
