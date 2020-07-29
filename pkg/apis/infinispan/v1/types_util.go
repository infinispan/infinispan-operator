package v1

import (
	"fmt"
	"net"
	"net/url"

	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	comutil "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GetCondition return the Status of the given condition or nil
// if condition is not present
func (ispn *Infinispan) GetCondition(condition string) *metav1.ConditionStatus {
	for _, c := range ispn.Status.Conditions {
		if c.Type == condition {
			return &c.Status
		}
	}
	return nil
}

// SetCondition set condition to status
func (ispn *Infinispan) SetCondition(condition string, status metav1.ConditionStatus, message string) bool {
	changed := false
	for idx := range ispn.Status.Conditions {
		c := &ispn.Status.Conditions[idx]
		if c.Type == condition {
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

// RemoveCondition remove condition from Status
func (ispn *Infinispan) RemoveCondition(condition string) bool {
	for idx := range ispn.Status.Conditions {
		c := &ispn.Status.Conditions[idx]
		if c.Type == condition {
			ispn.Status.Conditions = append(ispn.Status.Conditions[:idx], ispn.Status.Conditions[idx+1:]...)
			return true
		}
	}
	return false
}

// SetConditions set provided conditions to status
func (ispn *Infinispan) SetConditions(conds []InfinispanCondition) bool {
	changed := false
	for _, c := range conds {
		changed = changed || ispn.SetCondition(c.Type, c.Status, c.Message)
	}
	return changed
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
	// This field specifies the flavor of the
	// Infinispan cluster. "" is plain community edition (vanilla)
	if ispn.Spec.Image == "" {
		ispn.Spec.Image = constants.DefaultImageName
	}
	if ispn.Spec.Container.Memory == "" {
		ispn.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
	if ispn.Spec.Container.CPU == "" {
		cpuLimitString := comutil.ToMilliDecimalQuantity(constants.DefaultCPULimit)
		ispn.Spec.Container.CPU = cpuLimitString.String()
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

func (ispn *Infinispan) IsConditionTrue(name string) bool {
	upgradeCondition := ispn.GetCondition(name)
	return upgradeCondition != nil && *upgradeCondition == metav1.ConditionTrue
}

func (ispn *Infinispan) IsUpgradeCondition() bool {
	return ispn.IsConditionTrue("upgrade")
}

func (ispn *Infinispan) GetServiceExternalName() string {
	return fmt.Sprintf("%s-external", ispn.ObjectMeta.Name)
}

func (ispn *Infinispan) IsCache() bool {
	return ServiceTypeCache == ispn.Spec.Service.Type
}

func (ispn *Infinispan) HasSites() bool {
	return ispn.IsDataGrid() && len(ispn.Spec.Service.Sites.Locations) > 0
}

// IsExposed ...
func (ispn *Infinispan) IsExposed() bool {
	return ispn.Spec.Expose != nil && ispn.Spec.Expose.Type != ""
}

func (ispn *Infinispan) GetSiteServiceName() string {
	return fmt.Sprintf("%v-site", ispn.Name)
}

func (ispn *Infinispan) GetEndpointScheme() corev1.URIScheme {
	if ispn.Spec.Security.EndpointEncryption.Type != "" {
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
	return ispn.Spec.Security.EndpointEncryption.CertSecretName
}

func (ispn *Infinispan) GetCpuResources() (resource.Quantity, resource.Quantity) {
	cpuLimits := resource.MustParse(ispn.Spec.Container.CPU)
	cpuRequestsMillis := cpuLimits.MilliValue() / 2
	cpuRequests := comutil.ToMilliDecimalQuantity(cpuRequestsMillis)
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

func (ispn *Infinispan) FindNodePortExternalIP() (string, error) {
	localSiteName := ispn.Spec.Service.Sites.Local.Name
	locations := ispn.Spec.Service.Sites.Locations
	for _, location := range locations {
		if location.Name == localSiteName {
			masterURL, err := url.Parse(location.URL)
			if err != nil {
				return "", nil
			}
			host, _, err := net.SplitHostPort(masterURL.Host)
			if err != nil {
				return "", nil
			}
			return host, nil
		}
	}
	return "", fmt.Errorf("could not find node port external IP, check local site name matches one of the members")
}

func (ispn *Infinispan) FindRemoteLocations(localSiteName string) (remoteLocations []InfinispanSiteLocationSpec) {
	locations := ispn.Spec.Service.Sites.Locations
	for _, location := range locations {
		if localSiteName != location.Name {
			remoteLocations = append(remoteLocations, location)
		}
	}

	return
}

func (ispn *Infinispan) CopyLoggingCategories() map[string]string {
	categories := ispn.Spec.Logging.Categories
	if categories != nil {
		copied := make(map[string]string, len(categories))
		for category, level := range categories {
			copied[category] = level
		}
		return copied
	}

	return make(map[string]string)
}

// IsWellFormed return true if cluster is well formed
func (ispn *Infinispan) IsWellFormed() bool {
	return ispn.IsConditionTrue("wellFormed")
}

// NotClusterFormed return true is cluster is not well formed
func (ispn *Infinispan) NotClusterFormed(pods, replicas int) bool {
	notFormed := ispn.Status.Conditions[0].Status != metav1.ConditionTrue
	notEnoughMembers := pods < replicas
	return notFormed || notEnoughMembers
}

func (ispn *Infinispan) IsUpgradeNeeded(logger logr.Logger) bool {
	if ispn.IsUpgradeCondition() {
		stoppingCondition := ispn.GetCondition("stopping")
		if stoppingCondition != nil {
			stoppingCompleted := *stoppingCondition == metav1.ConditionFalse
			if stoppingCompleted {
				if ispn.Status.ReplicasWantedAtRestart > 0 {
					logger.Info("graceful shutdown after upgrade completed, continue upgrade process")
					return true
				}
				logger.Info("replicas to restart with not yet set, wait for graceful shutdown to complete")
				return false
			}
			logger.Info("wait for graceful shutdown before update to complete")
			return false
		}
	}

	return false
}
