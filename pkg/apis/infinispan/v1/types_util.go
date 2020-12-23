package v1

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	comutil "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// equals compares two ConditionType's case insensitive
func (a ConditionType) equals(b ConditionType) bool {
	return strings.ToLower(string(a)) == strings.ToLower(string(b))
}

// GetCondition return the Status of the given condition or nil
// if condition is not present
func (ispn *Infinispan) GetCondition(condition ConditionType) InfinispanCondition {
	for _, c := range ispn.Status.Conditions {
		if c.Type.equals(condition) {
			return c
		}
	}
	// Absence of condition means `False` value
	return InfinispanCondition{Type: condition, Status: metav1.ConditionFalse}
}

// SetCondition set condition to status
func (ispn *Infinispan) SetCondition(condition ConditionType, status metav1.ConditionStatus, message string) bool {
	changed := false
	for idx := range ispn.Status.Conditions {
		c := &ispn.Status.Conditions[idx]
		if c.Type.equals(condition) {
			if c.Status != status {
				c.Status = status
				changed = true
			}
			if c.Message != message {
				c.Message = message
				changed = true
			}
			return changed
		}
	}
	ispn.Status.Conditions = append(ispn.Status.Conditions, InfinispanCondition{Type: condition, Status: status, Message: message})
	return true
}

// SetConditions set provided conditions to status
func (ispn *Infinispan) SetConditions(conds []InfinispanCondition) bool {
	changed := false
	for _, c := range conds {
		changed = changed || ispn.SetCondition(c.Type, c.Status, c.Message)
	}
	return changed
}

func (ispn *Infinispan) ExpectConditionStatus(expected map[ConditionType]metav1.ConditionStatus) error {
	for key, value := range expected {
		c := ispn.GetCondition(key)
		if c.Status != value {
			if c.Message == "" {
				return fmt.Errorf("key '%s' has Status '%s', expected '%s'", key, c.Status, value)
			} else {
				return fmt.Errorf("key '%s' has Status '%s', expected '%s' Reason '%s", key, c.Status, value, c.Message)
			}
		}
	}
	return nil
}

// ApplyDefaults applies default values to the Infinispan instance
func (ispn *Infinispan) ApplyDefaults() {
	if ispn.Status.Conditions == nil {
		ispn.Status.Conditions = []InfinispanCondition{}
	}
	if ispn.Spec.Service.Type == "" {
		ispn.Spec.Service.Type = ServiceTypeCache
	}
	if ispn.Spec.Service.Type == ServiceTypeCache && ispn.Spec.Service.ReplicationFactor == 0 {
		ispn.Spec.Service.ReplicationFactor = 2
	}
	if ispn.Spec.Image == "" {
		ispn.Spec.Image = constants.DefaultImageName
	}
	if ispn.Spec.Container.Memory == "" {
		ispn.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
}

// ApplyEndpointEncryptionSettings compute the ee object
func (ispn *Infinispan) ApplyEndpointEncryptionSettings(servingCertsMode string, reqLogger logr.Logger) {
	// Populate EndpointEncryption if serving cert service is available
	if servingCertsMode == "openshift.io" && !ispn.IsEncryptionCertSourceDefined() {
		if ispn.Spec.Security.EndpointEncryption == nil {
			ispn.Spec.Security.EndpointEncryption = &EndpointEncryption{}
		}
		reqLogger.Info("Serving certificate service present. Configuring into Infinispan CR")
		if ispn.Spec.Security.EndpointEncryption.Type == "" {
			ispn.Spec.Security.EndpointEncryption.Type = CertificateSourceTypeService
			ispn.Spec.Security.EndpointEncryption.CertServiceName = "service.beta.openshift.io"
		}
		if ispn.Spec.Security.EndpointEncryption.CertSecretName == "" {
			ispn.Spec.Security.EndpointEncryption.CertSecretName = ispn.Name + "-cert-secret"
		}
	}
}

// PreliminaryChecks performs all the possible initial checks
func (ispn *Infinispan) PreliminaryChecks() (*reconcile.Result, error) {
	// If a CacheService is requested, checks that the pods have enough memory
	if ispn.Spec.Service.Type == ServiceTypeCache {
		memoryQ := resource.MustParse(ispn.Spec.Container.Memory)
		memory := memoryQ.Value()
		nativeMemoryOverhead := (memory * consts.CacheServiceJvmNativePercentageOverhead) / 100
		occupiedMemory := (consts.CacheServiceJvmNativeMb * 1024 * 1024) +
			(consts.CacheServiceFixedMemoryXmxMb * 1024 * 1024) +
			nativeMemoryOverhead
		if memory < occupiedMemory {
			return &reconcile.Result{RequeueAfter: consts.DefaultRequeueOnWrongSpec}, fmt.Errorf("Not enough memory. Increase infinispan.spec.container.memory. Now is %s, needed at least %d.", memoryQ.String(), occupiedMemory)
		}
	}
	return nil, nil
}

func (ispn *Infinispan) IsDataGrid() bool {
	return ServiceTypeDataGrid == ispn.Spec.Service.Type
}

func (ispn *Infinispan) IsConditionTrue(name ConditionType) bool {
	return ispn.GetCondition(name).Status == metav1.ConditionTrue
}

func (ispn *Infinispan) IsUpgradeCondition() bool {
	return ispn.IsConditionTrue(ConditionUpgrade)
}

func (ispn *Infinispan) GetServiceExternalName() string {
	return fmt.Sprintf("%s-external", ispn.ObjectMeta.Name)
}

func (ispn *Infinispan) IsCache() bool {
	return ServiceTypeCache == ispn.Spec.Service.Type
}

func (ispn *Infinispan) HasSites() bool {
	return ispn.IsDataGrid() && ispn.Spec.Service.Sites != nil && len(ispn.Spec.Service.Sites.Locations) > 0
}

// IsExposed ...
func (ispn *Infinispan) IsExposed() bool {
	return ispn.Spec.Expose != nil && ispn.Spec.Expose.Type != ""
}

func (ispn *Infinispan) GetSiteServiceName() string {
	return fmt.Sprintf("%v-site", ispn.Name)
}

func (ispn *Infinispan) GetEndpointScheme() corev1.URIScheme {
	if ispn.IsEncryptionCertSourceDefined() && !ispn.IsEncryptionDisabled() {
		return corev1.URISchemeHTTPS
	}
	return corev1.URISchemeHTTP
}

// GetSecretName returns the secret name associated with a server
func (ispn *Infinispan) GetSecretName() string {
	if ispn.Spec.Security.EndpointSecretName == "" {
		return fmt.Sprintf("%v-generated-secret", ispn.GetName())
	}
	return ispn.Spec.Security.EndpointSecretName
}

func (ispn *Infinispan) GetEncryptionSecretName() string {
	if ispn.Spec.Security.EndpointEncryption == nil {
		return ""
	}
	return ispn.Spec.Security.EndpointEncryption.CertSecretName
}

// IsEncryptionCertSourceDefined returns true if encryption certificates source is defined
func (ispn *Infinispan) IsEncryptionCertSourceDefined() bool {
	ee := ispn.Spec.Security.EndpointEncryption
	return ee != nil && ee.Type != ""
}

// IsEncryptionDisabled returns true if encryption is disable by configuration
func (ispn *Infinispan) IsEncryptionDisabled() bool {
	ee := ispn.Spec.Security.EndpointEncryption
	return ee != nil && ee.Type == CertificateSourceTypeNoneNoEncryption
}

// IsEncryptionCertFromService returns true if encryption certificates comes from a cluster service
func (ispn *Infinispan) IsEncryptionCertFromService() bool {
	ee := ispn.Spec.Security.EndpointEncryption
	return ee != nil && (ee.Type == CertificateSourceTypeService || ee.Type == CertificateSourceTypeServiceLowCase)
}

func (ispn *Infinispan) GetCpuResources() (resource.Quantity, resource.Quantity) {
	if ispn.Spec.Container.CPU != "" {
		cpuLimits := resource.MustParse(ispn.Spec.Container.CPU)
		cpuRequestsMillis := cpuLimits.MilliValue() / 2
		cpuRequests := comutil.ToMilliDecimalQuantity(int64(cpuRequestsMillis))
		return cpuRequests, cpuLimits
	}

	cpuLimits := comutil.ToMilliDecimalQuantity(constants.DefaultCPULimit)
	cpuRequests := comutil.ToMilliDecimalQuantity(constants.DefaultCPULimit / 2)
	return cpuRequests, cpuLimits
}

func (ispn *Infinispan) GetJavaOptions() (string, error) {
	switch ispn.Spec.Service.Type {
	case ServiceTypeDataGrid:
		return ispn.Spec.Container.ExtraJvmOpts, nil
	case ServiceTypeCache:
		javaOptions := fmt.Sprintf(
			"-Xmx%dM -Xms%dM -XX:MaxRAM=%dM %s %s",
			constants.CacheServiceFixedMemoryXmxMb,
			constants.CacheServiceFixedMemoryXmxMb,
			constants.CacheServiceMaxRamMb,
			constants.CacheServiceAdditionalJavaOptions,
			ispn.Spec.Container.ExtraJvmOpts,
		)
		return javaOptions, nil
	default:
		return "", fmt.Errorf("unknown service type '%s'", ispn.Spec.Service.Type)
	}
}

func (ispn *Infinispan) CopyLoggingCategories() map[string]string {
	if ispn.Spec.Logging != nil {
		categories := ispn.Spec.Logging.Categories
		if categories != nil {
			copied := make(map[string]string, len(categories))
			for category, level := range categories {
				copied[category] = level
			}
			return copied
		}
	}

	return make(map[string]string)
}

// IsWellFormed return true if cluster is well formed
func (ispn *Infinispan) IsWellFormed() bool {
	return ispn.EnsureClusterStability() == nil
}

// NotClusterFormed return true is cluster is not well formed
func (ispn *Infinispan) NotClusterFormed(pods, replicas int) bool {
	notFormed := !ispn.IsWellFormed()
	notEnoughMembers := pods < replicas
	return notFormed || notEnoughMembers
}

func (ispn *Infinispan) EnsureClusterStability() error {
	conditions := map[ConditionType]metav1.ConditionStatus{
		ConditionGracefulShutdown:   metav1.ConditionFalse,
		ConditionPrelimChecksPassed: metav1.ConditionTrue,
		ConditionUpgrade:            metav1.ConditionFalse,
		ConditionStopping:           metav1.ConditionFalse,
		ConditionWellFormed:         metav1.ConditionTrue,
	}
	return ispn.ExpectConditionStatus(conditions)
}
