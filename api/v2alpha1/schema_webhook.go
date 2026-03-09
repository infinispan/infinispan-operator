package v2alpha1

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (s *Schema) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(s).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-schema,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=schemas,verbs=create;update,versions=v2alpha1,name=vschema.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &Schema{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (s *Schema) ValidateCreate() error {
	var allErrs field.ErrorList
	if s.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("clusterName"), "'spec.clusterName' must be configured"))
	}
	if s.Spec.Schema == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("schema"), "'spec.schema' must be configured"))
	}
	return schemaStatusError(s, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (s *Schema) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	oldSchema := old.(*Schema)
	if oldSchema.Spec.ClusterName != s.Spec.ClusterName {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("clusterName"), "Schema clusterName is immutable and cannot be updated after initial Schema creation"))
	}
	if oldSchema.Spec.Name != s.Spec.Name {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("name"), "Schema name is immutable and cannot be updated after initial Schema creation"))
	}
	return schemaStatusError(s, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (s *Schema) ValidateDelete() error {
	return nil
}

func schemaStatusError(s *Schema, allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Schema"},
			s.Name, allErrs)
	}
	return nil
}
