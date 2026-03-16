package v2alpha1

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	v1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var schemaLog = ctrl.Log.WithName("webhook").WithName("Schema")

func (s *Schema) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(s).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-schema,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=schemas,verbs=create;update,versions=v2alpha1,name=mschema.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &Schema{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (s *Schema) Default() {
	schemaLog.Info("Applying defaults", "name", s.Name)
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-schema,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=schemas,verbs=create;update,versions=v2alpha1,name=vschema.kb.io,admissionReviewVersions={v1,v1beta1}

// RegisterSchemaValidatingWebhook explicitly adds the validating webhook to the Webhook Server
func RegisterSchemaValidatingWebhook(mgr ctrl.Manager) {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/validate-infinispan-org-v2alpha1-schema", &webhook.Admission{
		Handler: &schemaValidator{},
	})
}

type schemaValidator struct {
	client  runtimeClient.Client
	decoder *admission.Decoder
}

var _ inject.Client = &schemaValidator{}
var _ admission.Handler = &schemaValidator{}

func (sv *schemaValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	s := &Schema{}
	if req.Operation == v1.Create {
		err := sv.decoder.Decode(req, s)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		err = sv.validateCreate(ctx, s)
		if err != nil {
			var apiStatus apierrors.APIStatus
			if errors.As(err, &apiStatus) {
				return validationResponseFromStatus(false, apiStatus.Status())
			}
			return admission.Denied(err.Error())
		}
	}

	if req.Operation == v1.Update {
		oldSchema := &Schema{}

		err := sv.decoder.DecodeRaw(req.Object, s)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		err = sv.decoder.DecodeRaw(req.OldObject, oldSchema)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		err = sv.validateUpdate(s, oldSchema)
		if err != nil {
			var apiStatus apierrors.APIStatus
			if errors.As(err, &apiStatus) {
				return validationResponseFromStatus(false, apiStatus.Status())
			}
			return admission.Denied(err.Error())
		}
	}
	return admission.Allowed("")
}

// InjectClient injects the client.
func (sv *schemaValidator) InjectClient(c runtimeClient.Client) error {
	sv.client = c
	return nil
}

// InjectDecoder injects the decoder.
func (sv *schemaValidator) InjectDecoder(d *admission.Decoder) error {
	sv.decoder = d
	return nil
}

func (sv *schemaValidator) validateCreate(ctx context.Context, s *Schema) error {
	var allErrs field.ErrorList
	if s.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("clusterName"), "'spec.clusterName' must be configured"))
	}
	if s.Spec.Schema == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("schema"), "'spec.schema' must be configured"))
	}

	// Ensure that a Schema CR does not already exist in this namespace with the same spec.Name
	list := &SchemaList{}
	if err := sv.client.List(ctx, list, &runtimeClient.ListOptions{Namespace: s.Namespace}); err != nil {
		allErrs = append(allErrs, field.InternalError(field.NewPath("spec").Child("name"), err))
	} else {
		for _, existing := range list.Items {
			if s.GetSchemaName() == existing.GetSchemaName() && s.Spec.ClusterName == existing.Spec.ClusterName {
				msg := fmt.Sprintf("Schema CR already exists for cluster '%s' with name '%s'", s.Spec.ClusterName, s.GetSchemaName())
				allErrs = append(allErrs, field.Duplicate(field.NewPath("spec").Child("name"), msg))
			}
		}
	}
	return schemaStatusError(s, allErrs)
}

func (sv *schemaValidator) validateUpdate(s *Schema, oldSchema *Schema) error {
	var allErrs field.ErrorList
	if oldSchema.Spec.ClusterName != s.Spec.ClusterName {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("clusterName"), "Schema clusterName is immutable and cannot be updated after initial Schema creation"))
	}
	if oldSchema.Spec.Name != s.Spec.Name {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("name"), "Schema name is immutable and cannot be updated after initial Schema creation"))
	}
	return schemaStatusError(s, allErrs)
}

func schemaStatusError(s *Schema, allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Schema"},
			s.Name, allErrs)
	}
	return nil
}
