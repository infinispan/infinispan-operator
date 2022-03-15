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

func (r *Restore) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-restore,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=restores,verbs=create;update,versions=v2alpha1,name=mrestore.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &Restore{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Restore) Default() {
	if r.Spec.Container.Memory == "" {
		r.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
	resources := r.Spec.Resources
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

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-restore,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=restores,verbs=create;update,versions=v2alpha1,name=vrestore.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &Restore{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (b *Restore) ValidateCreate() error {
	var allErrs field.ErrorList
	if b.Spec.Cluster == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("cluster"), "'spec.cluster' must be configured"))
	}
	if b.Spec.Backup == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("backup"), "'spec.backup' must be configured"))
	}
	return b.StatusError(allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (b *Restore) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	oldRestore := old.(*Restore)
	if !reflect.DeepEqual(b.Spec, oldRestore.Spec) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"), "The Restore spec is immutable and cannot be updated after initial Restore creation"))
	}
	return b.StatusError(allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (b *Restore) ValidateDelete() error {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil
}

func (b *Restore) StatusError(allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Restore"},
			b.Name, allErrs)
	}
	return nil
}
