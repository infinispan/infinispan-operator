package v2alpha1

import (
	"reflect"

	"github.com/infinispan/infinispan-operator/controllers/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (b *Backup) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(b).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-backup,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=backups,verbs=create;update,versions=v2alpha1,name=mbackup.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &Backup{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (b *Backup) Default() {
	if b.Spec.Container.Memory == "" {
		b.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
	resources := b.Spec.Resources
	if resources == nil {
		return
	}

	if len(resources.CacheConfigs) > 0 {
		resources.Templates = resources.CacheConfigs
		resources.CacheConfigs = nil
	}

	if len(resources.Scripts) > 0 {
		resources.Tasks = resources.Scripts
		resources.Scripts = nil
	}
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-backup,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=backups,verbs=create;update,versions=v2alpha1,name=vbackup.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &Backup{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (b *Backup) ValidateCreate() error {
	var allErrs field.ErrorList
	if b.Spec.Cluster == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("cluster"), "'spec.cluster' must be configured"))
	}
	return b.StatusError(allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (b *Backup) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	oldBackup := old.(*Backup)
	if !reflect.DeepEqual(b.Spec, oldBackup.Spec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"), "The Backup spec is immutable and cannot be updated after initial Backup creation"))
	}
	return b.StatusError(allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (b *Backup) ValidateDelete() error {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil
}

func (b *Backup) StatusError(allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Backup"},
			b.Name, allErrs)
	}
	return nil
}
