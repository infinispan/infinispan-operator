package v1

import (
	"context"
	"fmt"

	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	log              = ctrl.Log.WithName("webhook").WithName("Infinispan")
	eventRec         record.EventRecorder
	servingCertsMode string
)

func (i *Infinispan) SetupWebhookWithManager(mgr ctrl.Manager) error {
	kubernetes := kube.NewKubernetesFromController(mgr)
	eventRec = mgr.GetEventRecorderFor("webhook-infinispan")
	servingCertsMode = kubernetes.GetServingCertsMode(context.Background())

	return ctrl.NewWebhookManagedBy(mgr).
		For(i).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v1-infinispan,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=infinispans,verbs=create;update,versions=v1,name=minfinispan.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &Infinispan{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (i *Infinispan) Default() {
	if i.Spec.Service.Type == "" {
		i.Spec.Service.Type = ServiceTypeCache
	}
	if i.Spec.Service.Type == ServiceTypeCache && i.Spec.Service.ReplicationFactor == 0 {
		i.Spec.Service.ReplicationFactor = 2
	}
	if i.Spec.Container.Memory == "" {
		i.Spec.Container.Memory = consts.DefaultMemorySize.String()
	}
	if i.IsDataGrid() {
		if i.Spec.Service.Container == nil {
			i.Spec.Service.Container = &InfinispanServiceContainerSpec{}
		}
		if i.Spec.Service.Container.Storage == nil {
			i.Spec.Service.Container.Storage = pointer.StringPtr(consts.DefaultPVSize.String())
		}
	}
	if i.Spec.Security.EndpointAuthentication == nil {
		i.Spec.Security.EndpointAuthentication = pointer.BoolPtr(true)
	}
	if *i.Spec.Security.EndpointAuthentication {
		i.Spec.Security.EndpointSecretName = i.GetSecretName()
	} else if i.IsGeneratedSecret() {
		i.Spec.Security.EndpointSecretName = ""
	}
	if i.Spec.Upgrades == nil {
		i.Spec.Upgrades = &InfinispanUpgradesSpec{
			Type: UpgradeTypeShutdown,
		}
	}
	if i.Spec.ConfigListener == nil {
		i.Spec.ConfigListener = &ConfigListenerSpec{
			Enabled: true,
		}
	}

	if i.Spec.Affinity == nil {
		// The user hasn't configured Affinity, so we utilise the default strategy of preferring pods are deployed on distinct nodes
		i.Spec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"infinispan_cr": i.Name,
								"clusterName":   i.Name,
								"app":           "infinispan-pod",
							},
						},
						TopologyKey: "r.kubernetes.io/hostname",
					},
				}},
			},
		}
	}

	i.ApplyEndpointEncryptionSettings(servingCertsMode)

	if i.HasSites() {
		// Migrate Spec.Service.Locations Host and Port parameters into the unified URL schema
		for idx, location := range i.Spec.Service.Sites.Locations {
			if location.Host != nil && *location.Host != "" {
				port := consts.CrossSitePort
				if location.Port != nil && *location.Port > 0 {
					port = int(*location.Port)
				}
				// It's not possible to unset the Host and Port values so we must leave their values in place but never use them
				i.Spec.Service.Sites.Locations[idx].URL = fmt.Sprintf("%s://%s:%d", consts.StaticCrossSiteUriSchema, *location.Host, port)
			}
		}
	}
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v1-infinispan,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=infinispans,verbs=create;update,versions=v1,name=vinfinispan.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &Infinispan{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (i *Infinispan) ValidateCreate() error {
	return i.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (i *Infinispan) ValidateUpdate(old runtime.Object) error {
	return i.validate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (i *Infinispan) ValidateDelete() error {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil
}

func (i *Infinispan) validate() error {
	var allErrs field.ErrorList

	if i.Spec.Container.CPU != "" {
		req, limit, err := i.Spec.Container.GetCpuResources()
		if err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("cpu"), i.Spec.Container.CPU, err.Error()))
		}

		if req.Cmp(limit) > 0 {
			msg := fmt.Sprintf("CCPU request '%s' exceeds limit '%s'", req.String(), limit.String())
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("cpu"), i.Spec.Container.CPU, msg))
		}
	}

	memReq, memLimit, err := i.Spec.Container.GetMemoryResources()
	if i.Spec.Container.Memory != "" {
		if err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("memory"), i.Spec.Container.Memory, err.Error()))
		}

		if memReq.Cmp(memLimit) > 0 {
			msg := fmt.Sprintf("Memory request '%s' exceeds limit '%s'", memReq.String(), memLimit.String())
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("memory"), i.Spec.Container.CPU, msg))
		}
	}

	// Warn if memory size exceeds persistent vol
	if i.IsDataGrid() && !i.IsEphemeralStorage() && i.StorageSize() != "" {
		size, err := resource.ParseQuantity(i.StorageSize())
		if err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("service").Child("container").Child("storage"), i.Spec.Service.Container.Storage, err.Error()))
		} else if size.Cmp(memLimit) < 0 {
			errMsg := "Persistent volume size is less than memory size. Graceful shutdown may not work."
			eventRec.Event(i, corev1.EventTypeWarning, "LowPersistenceStorage", errMsg)
			log.Info(errMsg, "Request.Namespace", i.Namespace, "Request.Name", i.Name)
		}
	}

	if err := i.validateCacheService(); err != nil {
		allErrs = append(allErrs, err)
	}

	if i.IsEncryptionEnabled() && i.Spec.Security.EndpointEncryption.CertSecretName == "" {
		msg := fmt.Sprintf("field must be provided for 'spec.security.endpointEncryption.certificateSourceType=%s' to be configured", CertificateSourceTypeSecret)
		err := field.Required(field.NewPath("spec").Child("security").Child("endpointEncryption").Child("certSecretName"), msg)
		allErrs = append(allErrs, err)
	}

	// Validate Hot Rod Rolling Upgrades
	if i.Spec.Upgrades.Type == UpgradeTypeHotRodRolling {
		if !i.IsDataGrid() {
			msg := fmt.Sprintf("%s upgrades only supported with 'spec.service.type=%s'", UpgradeTypeHotRodRolling, ServiceTypeDataGrid)
			err := field.Forbidden(field.NewPath("spec").Child("service").Child("type"), msg)
			allErrs = append(allErrs, err)
		}

		if i.Spec.Service.Sites != nil {
			msg := fmt.Sprintf("XSite not supported with %s upgrades", UpgradeTypeHotRodRolling)
			err := field.Forbidden(field.NewPath("spec").Child("service").Child("sites"), msg)
			allErrs = append(allErrs, err)
		}
	}

	if i.HasExternalArtifacts() {
		for i, artifact := range i.Spec.Dependencies.Artifacts {
			f := field.NewPath("spec").Child("dependencies").Child("artifacts").Index(i)
			if artifact.Url == "" && artifact.Maven == "" {
				allErrs = append(allErrs, field.Required(f, "'artifact.Url' OR 'artifact.Maven' must be supplied"))
			} else if artifact.Url != "" && artifact.Maven != "" {
				allErrs = append(allErrs, field.Duplicate(f, "At most one of ['artifact.Url', 'artifact.Maven'] must be configured"))
			}
		}
	}

	if len(allErrs) != 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: GroupVersion.Group, Kind: "Infinispan"},
			i.Name, allErrs)
	}
	return nil
}

func (i *Infinispan) validateCacheService() *field.Error {
	// If a CacheService is requested, checks that the pods have enough memory
	if i.Spec.Service.Type == ServiceTypeCache {
		// We can ignore error here as we have already checked Memory spec is valid
		_, memoryQ, _ := i.Spec.Container.GetMemoryResources()
		memory := memoryQ.Value()
		nativeMemoryOverhead := (memory * consts.CacheServiceJvmNativePercentageOverhead) / 100
		occupiedMemory := (consts.CacheServiceJvmNativeMb * 1024 * 1024) +
			(consts.CacheServiceFixedMemoryXmxMb * 1024 * 1024) +
			nativeMemoryOverhead
		if memory < occupiedMemory {
			msg := fmt.Sprintf("Not enough memory allocated. The Cache Service requires at least %d bytes", occupiedMemory)
			return field.Invalid(field.NewPath("spec").Child("container").Child("memory"), i.Spec.Container.Memory, msg)
		}
	}
	return nil
}
