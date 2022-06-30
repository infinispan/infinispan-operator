package v2alpha1

import (
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var log logr.Logger = ctrl.Log.WithName("webhook").WithName("Cache")

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

var _ webhook.Validator = &Cache{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (c *Cache) ValidateCreate() error {
	var allErrs field.ErrorList
	if c.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("clusterName"), "'spec.clusterName' must be configured"))
	}
	return c.StatusError(allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (c *Cache) ValidateUpdate(old runtime.Object) error {
	var allErrs field.ErrorList
	oldCache := old.(*Cache)
	if oldCache.Spec.ClusterName != c.Spec.ClusterName {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("clusterName"), "Cache clusterName is immutable and cannot be updated after initial Cache creation"))
	}
	return c.StatusError(allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (c *Cache) ValidateDelete() error {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil
}

func (c *Cache) StatusError(allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Cache"},
			c.Name, allErrs)
	}
	return nil
}
