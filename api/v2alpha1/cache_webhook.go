package v2alpha1

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var log = ctrl.Log.WithName("webhook").WithName("Cache")

func (c *Cache) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-cache,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=caches,verbs=create;update,versions=v2alpha1,name=mcache.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &Cache{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (c *Cache) Default() {
	if c.Spec.AdminAuth != nil {
		log.Info("Ignoring and removing 'spec.AdminAuth' field. The operator's admin credentials are now used to perform cache operations")
		c.Spec.AdminAuth = nil
	}
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-cache,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=caches,verbs=create;update,versions=v2alpha1,name=vcache.kb.io,admissionReviewVersions={v1,v1beta1}

// RegisterCacheValidatingWebhook explicitly adds the validating webhook to the Webhook Server
// This is necessary as we need to implement admission.Handler interface directly so that the request context can be
// used by the runtime client
func RegisterCacheValidatingWebhook(mgr ctrl.Manager) {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register("/validate-infinispan-org-v2alpha1-cache", &webhook.Admission{
		Handler: &cacheValidator{},
	})
}

type cacheValidator struct {
	client  runtimeClient.Client
	decoder *admission.Decoder
}

var _ inject.Client = &cacheValidator{}
var _ admission.Handler = &cacheValidator{}

func (cv *cacheValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Get the object in the request
	cache := &Cache{}
	if req.Operation == v1.Create {
		err := cv.decoder.Decode(req, cache)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		err = cv.Create(ctx, cache)
		if err != nil {
			var apiStatus apierrors.APIStatus
			if errors.As(err, &apiStatus) {
				return validationResponseFromStatus(false, apiStatus.Status())
			}
			return admission.Denied(err.Error())
		}
	}

	if req.Operation == v1.Update {
		oldCache := &Cache{}

		err := cv.decoder.DecodeRaw(req.Object, cache)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		err = cv.decoder.DecodeRaw(req.OldObject, oldCache)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}

		err = cv.Update(cache, oldCache)
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
func (cv *cacheValidator) InjectClient(c runtimeClient.Client) error {
	cv.client = c
	return nil
}

// InjectDecoder injects the decoder.
func (cv *cacheValidator) InjectDecoder(d *admission.Decoder) error {
	cv.decoder = d
	return nil
}

func (cv *cacheValidator) Create(ctx context.Context, c *Cache) error {
	var allErrs field.ErrorList
	if c.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("clusterName"), "'spec.clusterName' must be configured"))
	}

	// Ensure that a Cache CR does not already exist in this namespace with the same spec.Name
	list := &CacheList{}
	if err := cv.client.List(ctx, list); err != nil {
		allErrs = append(allErrs, field.InternalError(field.NewPath("spec").Child("name"), err))
	} else {
		for _, cache := range list.Items {
			if c.Spec.Name == cache.Spec.Name && c.Spec.ClusterName == cache.Spec.ClusterName {
				msg := fmt.Sprintf("Cache CR already exists for cluster '%s' with spec.Name '%s'", c.Spec.ClusterName, c.Spec.Name)
				allErrs = append(allErrs, field.Duplicate(field.NewPath("spec").Child("name"), msg))
			}
		}
	}
	return StatusError(c, allErrs)
}

func (cv *cacheValidator) Update(c *Cache, oldCache *Cache) error {
	var allErrs field.ErrorList
	if oldCache.Spec.ClusterName != c.Spec.ClusterName {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("clusterName"), "Cache clusterName is immutable and cannot be updated after initial Cache creation"))
	}
	if oldCache.Spec.Name != c.Spec.Name {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("name"), "Cache name is immutable and cannot be updated after initial Cache creation"))
	}
	return StatusError(c, allErrs)
}

func StatusError(c *Cache, allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Cache"},
			c.Name, allErrs)
	}
	return nil
}

// validationResponseFromStatus returns a response for admitting a request with provided Status object.
func validationResponseFromStatus(allowed bool, status metav1.Status) admission.Response {
	return admission.Response{
		AdmissionResponse: admissionv1.AdmissionResponse{
			Allowed: allowed,
			Result:  &status,
		},
	}
}
