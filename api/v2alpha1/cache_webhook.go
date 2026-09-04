package v2alpha1

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var log = ctrl.Log.WithName("webhook").WithName("Cache")

func (c *Cache) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		WithDefaulter(&CacheCustomDefaulter{}).
		WithValidator(&CacheCustomValidator{client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v2alpha1-cache,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=caches,verbs=create;update,versions=v2alpha1,name=mcache.kb.io,admissionReviewVersions={v1,v1beta1}

// CacheCustomDefaulter applies defaults to Cache resources. It implements the
// webhook.CustomDefaulter interface.
// +kubebuilder:object:generate=false
type CacheCustomDefaulter struct{}

var _ webhook.CustomDefaulter = &CacheCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (d *CacheCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	c, ok := obj.(*Cache)
	if !ok {
		return fmt.Errorf("expected a Cache object but got %T", obj)
	}

	if c.Spec.AdminAuth != nil {
		log.Info("Ignoring and removing 'spec.AdminAuth' field. The operator's admin credentials are now used to perform cache operations")
		c.Spec.AdminAuth = nil
	}

	if c.Spec.Updates == nil {
		c.Spec.Updates = &CacheUpdateSpec{
			Strategy: CacheUpdateRetain,
		}
	}

	if c.Spec.Updates.Strategy == "" {
		c.Spec.Updates.Strategy = CacheUpdateRetain
	}
	return nil
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v2alpha1-cache,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=caches,verbs=create;update,versions=v2alpha1,name=vcache.kb.io,admissionReviewVersions={v1,v1beta1}

// CacheCustomValidator validates Cache resources. It implements the
// webhook.CustomValidator interface. Unlike the other CRDs, Cache validation
// requires the runtime client (and the request context) in order to ensure that
// no other Cache CR already exists in the namespace with the same spec.Name.
// +kubebuilder:object:generate=false
type CacheCustomValidator struct {
	client runtimeClient.Client
}

var _ webhook.CustomValidator = &CacheCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *CacheCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	c, ok := obj.(*Cache)
	if !ok {
		return nil, fmt.Errorf("expected a Cache object but got %T", obj)
	}

	var allErrs field.ErrorList
	if c.Spec.ClusterName == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec").Child("clusterName"), "'spec.clusterName' must be configured"))
	}

	// Ensure that a Cache CR does not already exist in this namespace with the same spec.Name
	list := &CacheList{}
	if err := v.client.List(ctx, list, &runtimeClient.ListOptions{Namespace: c.Namespace}); err != nil {
		allErrs = append(allErrs, field.InternalError(field.NewPath("spec").Child("name"), err))
	} else {
		for _, cache := range list.Items {
			if c.Spec.Name == cache.Spec.Name && c.Spec.ClusterName == cache.Spec.ClusterName {
				msg := fmt.Sprintf("Cache CR already exists for cluster '%s' with spec.Name '%s'", c.Spec.ClusterName, c.Spec.Name)
				allErrs = append(allErrs, field.Duplicate(field.NewPath("spec").Child("name"), msg))
			}
		}
	}
	return nil, StatusError(c, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (v *CacheCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	c, ok := newObj.(*Cache)
	if !ok {
		return nil, fmt.Errorf("expected a Cache object but got %T", newObj)
	}
	oldCache, ok := oldObj.(*Cache)
	if !ok {
		return nil, fmt.Errorf("expected a Cache object but got %T", oldObj)
	}

	var allErrs field.ErrorList
	if oldCache.Spec.ClusterName != c.Spec.ClusterName {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("clusterName"), "Cache clusterName is immutable and cannot be updated after initial Cache creation"))
	}
	if oldCache.Spec.Name != c.Spec.Name {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("name"), "Cache name is immutable and cannot be updated after initial Cache creation"))
	}
	return nil, StatusError(c, allErrs)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (v *CacheCustomValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil, nil
}

func StatusError(c *Cache, allErrs field.ErrorList) error {
	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Cache"},
			c.Name, allErrs)
	}
	return nil
}
