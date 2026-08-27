package v2alpha1

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (s *Schema) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(s).
		WithDefaulter(&SchemaCustomDefaulter{}).
		WithValidator(&SchemaCustomValidator{client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-schema,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=schemas,verbs=create;update,versions=v2alpha1,name=mschema.kb.io,admissionReviewVersions={v1,v1beta1}

// SchemaCustomDefaulter applies defaults to Schema resources. It implements the
// webhook.CustomDefaulter interface.
// +kubebuilder:object:generate=false
type SchemaCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &SchemaCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (d *SchemaCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	s, ok := obj.(*Schema)
	if !ok {
		return fmt.Errorf("expected a Schema object but got %T", obj)
	}

	if s.Spec.Name != "" && !strings.HasSuffix(s.Spec.Name, ".proto") {
		s.Spec.Name = s.Spec.Name + ".proto"
	}
	return nil
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-schema,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=schemas,verbs=create;update,versions=v2alpha1,name=vschema.kb.io,admissionReviewVersions={v1,v1beta1}

// SchemaCustomValidator validates Schema resources. It implements the
// webhook.CustomValidator interface.
// +kubebuilder:object:generate=false
type SchemaCustomValidator struct {
	client runtimeClient.Client
}

var _ webhook.CustomValidator = &SchemaCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *SchemaCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	s, ok := obj.(*Schema)
	if !ok {
		return nil, fmt.Errorf("expected a Schema object but got %T", obj)
	}

	var allErrs field.ErrorList
	if s.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("clusterName"), "'spec.clusterName' must be configured"))
	}
	if s.Spec.Schema == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("schema"), "'spec.schema' must be configured"))
	}

	list := &SchemaList{}
	if err := v.client.List(ctx, list, &runtimeClient.ListOptions{Namespace: s.Namespace}); err != nil {
		allErrs = append(allErrs, field.InternalError(field.NewPath("spec").Child("name"), err))
	} else {
		newSchemaName := s.GetSchemaName()
		for _, existing := range list.Items {
			if newSchemaName == existing.GetSchemaName() && s.Spec.ClusterName == existing.Spec.ClusterName {
				msg := fmt.Sprintf("Schema CR already exists for cluster '%s' with schema name '%s'", s.Spec.ClusterName, newSchemaName)
				allErrs = append(allErrs, field.Duplicate(field.NewPath("spec").Child("name"), msg))
			}
		}
	}
	return nil, schemaStatusError(s, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *SchemaCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	s, ok := newObj.(*Schema)
	if !ok {
		return nil, fmt.Errorf("expected a Schema object but got %T", newObj)
	}
	oldSchema, ok := oldObj.(*Schema)
	if !ok {
		return nil, fmt.Errorf("expected a Schema object but got %T", oldObj)
	}

	var allErrs field.ErrorList
	if oldSchema.Spec.ClusterName != s.Spec.ClusterName {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("clusterName"), "Schema clusterName is immutable and cannot be updated after initial Schema creation"))
	}
	if oldSchema.GetSchemaName() != s.GetSchemaName() {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("name"), "Schema name is immutable and cannot be updated after initial Schema creation"))
	}
	return nil, schemaStatusError(s, allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (v *SchemaCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil, nil
}

func schemaStatusError(s *Schema, allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Schema"},
			s.Name, allErrs)
	}
	return nil
}
