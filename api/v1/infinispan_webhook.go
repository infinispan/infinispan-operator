package v1

import (
	"context"
	"fmt"
	"strconv"

	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
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
	ServingCertsMode string
	versionManager   *version.Manager
)

func (i *Infinispan) SetupWebhookWithManager(mgr ctrl.Manager) (err error) {
	kubernetes := kube.NewKubernetesFromController(mgr)
	eventRec = mgr.GetEventRecorderFor("webhook-infinispan")
	ServingCertsMode = kubernetes.GetServingCertsMode(context.Background())

	// Initialize supported Operand versions
	versionManager, err = version.ManagerFromEnv(OperatorOperandVersionEnvVarName)
	if err != nil {
		return
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(i).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infinispan-org-v1-infinispan,mutating=true,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=infinispans,verbs=create;update,versions=v1,name=minfinispan.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Defaulter = &Infinispan{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (i *Infinispan) Default() {
	if i.Spec.Version == "" {
		i.Spec.Version = versionManager.Latest().Ref()
	}
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
	if i.Spec.ConfigListener.Logging == nil {
		i.Spec.ConfigListener.Logging = &ConfigListenerLoggingSpec{
			Level: ConfigListenerLoggingInfo,
		}
	}

	if i.Spec.Scheduling == nil {
		i.Spec.Scheduling = &SchedulingSpec{}
	}

	if i.Spec.Scheduling.Affinity == nil {
		if i.Spec.Affinity != nil {
			i.Spec.Scheduling.Affinity = i.Spec.Affinity
			i.Spec.Affinity = nil
		} else {
			// The user hasn't configured Affinity, so we utilise the default strategy of preferring pods are deployed on distinct nodes
			i.Spec.Scheduling.Affinity = &corev1.Affinity{
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
							TopologyKey: "kubernetes.io/hostname",
						},
					}},
				},
			}
		}
	}

	i.ApplyEndpointEncryptionSettings(ServingCertsMode)

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

		if i.Spec.Service.Sites.Local.Discovery == nil {
			i.Spec.Service.Sites.Local.Discovery = &DiscoverySiteSpec{}
		}
		if i.Spec.Service.Sites.Local.Discovery.Type == "" {
			i.Spec.Service.Sites.Local.Discovery.Type = GossipRouterType
		}
		if i.Spec.Service.Sites.Local.Discovery.LaunchGossipRouter == nil {
			i.Spec.Service.Sites.Local.Discovery.LaunchGossipRouter = pointer.Bool(true)
		}
		if i.Spec.Service.Sites.Local.Discovery.Heartbeats == nil {
			i.Spec.Service.Sites.Local.Discovery.Heartbeats = &GossipRouterHeartbeatSpec{}
		}
		if i.Spec.Service.Sites.Local.Discovery.Heartbeats.Enabled == nil {
			i.Spec.Service.Sites.Local.Discovery.Heartbeats.Enabled = pointer.Bool(true)
		}
		if i.Spec.Service.Sites.Local.Discovery.Heartbeats.Interval == nil {
			i.Spec.Service.Sites.Local.Discovery.Heartbeats.Interval = pointer.Int64(10000)
		}
		if i.Spec.Service.Sites.Local.Discovery.Heartbeats.Timeout == nil {
			i.Spec.Service.Sites.Local.Discovery.Heartbeats.Timeout = pointer.Int64(30000)
		}
	}

	if i.Spec.Jmx == nil {
		i.Spec.Jmx = &JmxSpec{}
	}
}

// +kubebuilder:webhook:path=/validate-infinispan-org-v1-infinispan,mutating=false,failurePolicy=fail,sideEffects=None,groups=infinispan.org,resources=infinispans,verbs=create;update,versions=v1,name=vinfinispan.kb.io,admissionReviewVersions={v1,v1beta1}

var _ webhook.Validator = &Infinispan{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (i *Infinispan) ValidateCreate() error {
	return i.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (i *Infinispan) ValidateUpdate(oldRuntimeObj runtime.Object) error {
	if err := i.validate(); err != nil {
		return err
	}

	var allErrs field.ErrorList
	old := oldRuntimeObj.(*Infinispan)
	if old.Spec.Version != "" {
		// We know the versions must be valid as they have already been validated, so the error will always be nil
		operand, _ := versionManager.WithRef(i.Spec.Version)
		oldOperand, _ := versionManager.WithRef(old.Spec.Version)

		if i.GracefulShutdownUpgrades() {
			// Version downgrades are not supported with Graceful Shutdown
			if operand.LT(oldOperand) {
				detail := fmt.Sprintf("Version downgrading not supported. Existing='%s', Requested='%s'.", oldOperand.Ref(), operand.Ref())
				allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("version"), detail))
			}
		} else if operand.LT(oldOperand) {
			if old.Status.HotRodRollingUpgradeStatus == nil {
				detail := fmt.Sprintf("Version rollback only supported when a Hot Rolling Upgrade is in progress. Existing='%s', Requested='%s'.", oldOperand.Ref(), operand.Ref())
				allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("version"), detail))
			} else {
				// Only allow upgrades to be rolled back to the original source version
				validRollbackOperand, _ := versionManager.WithRef(i.Status.HotRodRollingUpgradeStatus.SourceVersion)
				if !validRollbackOperand.EQ(operand) {
					detail := fmt.Sprintf("Hot Rod Rolling Upgrades can only be rolled back to the original source version. Existing='%s', Source='%s', Requested='%s'.",
						oldOperand.Ref(), validRollbackOperand.Ref(), operand.Ref())
					allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("version"), detail))
				}
			}
		}
	}

	if old.Spec.Jmx != nil && old.Spec.Jmx.Enabled != i.Spec.Jmx.Enabled {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec").Child("jmx"), "JMX configuration is immutable and cannot be updated after initial Infinispan creation"))
	}
	return errorListToError(i, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (i *Infinispan) ValidateDelete() error {
	// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
	return nil
}

func (i *Infinispan) validate() error {
	var allErrs field.ErrorList

	operand, err := versionManager.WithRef(i.Spec.Version)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("version"), i.Spec.Version, err.Error()))
	}
	if operand.Deprecated {
		eventRec.Event(i, corev1.EventTypeWarning, "DeprecatedOperandVersion", "Configured Infinispan version will be removed in a subsequent Operator release. You must upgrade to a non-deprecated release before upgrading the Operator.")
	}

	if i.Spec.Container.CPU != "" {
		req, limit, err := i.Spec.Container.GetCpuResources()
		if err != nil {
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("cpu"), i.Spec.Container.CPU, err.Error()))
		}

		if req.Cmp(limit) > 0 {
			msg := fmt.Sprintf("CPU request '%s' exceeds limit '%s'", req.String(), limit.String())
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
			allErrs = append(allErrs, field.Invalid(field.NewPath("spec").Child("container").Child("memory"), i.Spec.Container.Memory, msg))
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

	if i.IsEncryptionEnabled() {
		e := i.Spec.Security.EndpointEncryption
		if e.CertSecretName == "" {
			msg := fmt.Sprintf("field must be provided for 'spec.security.endpointEncryption.certificateSourceType=%s' to be configured", CertificateSourceTypeSecret)
			err := field.Required(field.NewPath("spec").Child("security").Child("endpointEncryption").Child("certSecretName"), msg)
			allErrs = append(allErrs, err)
		}

		if e.CertServiceName != "" && e.Type == CertificateSourceTypeSecret {
			msg := fmt.Sprintf(".certServiceName cannot be configured with Encryption .type=%s", CertificateSourceTypeSecret)
			err := field.Forbidden(field.NewPath("spec").Child("security").Child("endpointEncryption").Child("certServiceName"), msg)
			allErrs = append(allErrs, err)
		}
	}

	if cl := i.Spec.ConfigListener; cl != nil {
		path := field.NewPath("spec").Child("configListener")
		if cl.CPU != "" {
			req, limit, err := cl.CpuResources()
			if err != nil {
				allErrs = append(allErrs, field.Invalid(path.Child("cpu"), cl.CPU, err.Error()))
			}

			if req.Cmp(limit) > 0 {
				msg := fmt.Sprintf("CPU request '%s' exceeds limit '%s'", req.String(), limit.String())
				allErrs = append(allErrs, field.Invalid(path.Child("cpu"), cl.CPU, msg))
			}
		}

		if cl.Memory != "" {
			req, limit, err := cl.MemoryResources()
			if err != nil {
				allErrs = append(allErrs, field.Invalid(path.Child("memory"), cl.Memory, err.Error()))
			}

			if req.Cmp(limit) > 0 {
				msg := fmt.Sprintf("Memory request '%s' exceeds limit '%s'", req.String(), limit.String())
				allErrs = append(allErrs, field.Invalid(path.Child("memory"), cl.Memory, msg))
			}
		}
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

	if i.IsEphemeralStorage() {
		errMsg := "Ephemeral storage configured. All data will be lost on cluster shutdown and restart."
		eventRec.Event(i, corev1.EventTypeWarning, "EphemeralStorageEnables", "Ephemeral storage configured. All data will be lost on cluster shutdown and restart.")
		log.Info(errMsg, "Request.Namespace", i.Namespace, "Request.Name", i.Name)
	}

	// validate Gossip Router resources requests
	if i.HasSites() {
		gr := i.Spec.Service.Sites.Local.Discovery
		if gr != nil {
			path := field.NewPath("spec").Child("service").Child("sites").Child("local").Child("discovery")
			if gr.CPU != "" {
				req, limit, err := gr.CpuResources()
				if err != nil {
					allErrs = append(allErrs, field.Invalid(path.Child("cpu"), gr.CPU, err.Error()))
				}

				if req.Cmp(limit) > 0 {
					msg := fmt.Sprintf("CPU request '%s' exceeds limit '%s'", req.String(), limit.String())
					allErrs = append(allErrs, field.Invalid(path.Child("cpu"), gr.CPU, msg))
				}
			}

			if gr.Memory != "" {
				req, limit, err := gr.MemoryResources()
				if err != nil {
					allErrs = append(allErrs, field.Invalid(path.Child("memory"), gr.Memory, err.Error()))
				}

				if req.Cmp(limit) > 0 {
					msg := fmt.Sprintf("Memory request '%s' exceeds limit '%s'", req.String(), limit.String())
					allErrs = append(allErrs, field.Invalid(path.Child("memory"), gr.Memory, msg))
				}
			}

			// validate heartbeats interval and timeout
			if gr.Heartbeats != nil && gr.Heartbeats.Enabled != nil && *gr.Heartbeats.Enabled {
				var interval, timeout int64
				if gr.Heartbeats.Interval == nil {
					interval = 10000
				} else {
					interval = *gr.Heartbeats.Interval
				}
				if gr.Heartbeats.Timeout == nil {
					timeout = 30000
				} else {
					timeout = *gr.Heartbeats.Timeout
				}
				if interval <= 0 {
					msg := fmt.Sprintf("Heartbeats interval must be a positive integer ('%s')", strconv.FormatInt(interval, 10))
					allErrs = append(allErrs, field.Invalid(path.Child("hearbeats").Child("interval"), interval, msg))
				}
				if timeout <= 0 {
					msg := fmt.Sprintf("Heartbeats timeout must be a positive integer ('%s')", strconv.FormatInt(timeout, 10))
					allErrs = append(allErrs, field.Invalid(path.Child("hearbeats").Child("timeout"), timeout, msg))
				}
				if interval >= timeout {
					msg := fmt.Sprintf("Heartbeats interval ('%s') must be less than timeout ('%s')", strconv.FormatInt(interval, 10), strconv.FormatInt(timeout, 10))
					allErrs = append(allErrs, field.Invalid(path.Child("hearbeats").Child("interval"), interval, msg))
					allErrs = append(allErrs, field.Invalid(path.Child("hearbeats").Child("timeout"), timeout, msg))
				}
			}
		}

		// print a warning if the truststore is not configured
		if i.IsSiteTLSEnabled() && i.Spec.Service.Sites.Local.Encryption.TrustStore == nil {
			errMsg := "The Trust Store for Cross-Site Encryption is recommended but it is not configured. It will fallback to the JVM default Trust Store."
			eventRec.Event(i, corev1.EventTypeWarning, "CrossSiteTrustStoreMissing", errMsg)
			log.Info(errMsg, "Request.Namespace", i.Namespace, "Request.Name", i.Name)
		}
	}

	return errorListToError(i, allErrs)
}

func errorListToError(i *Infinispan, allErrs field.ErrorList) error {
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
