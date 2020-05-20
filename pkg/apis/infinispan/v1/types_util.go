package v1

import (
	"fmt"
	"net"
	"net/url"

	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	comutil "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// GetCondition return the Status of the given condition or nil
// if condition is not present
func (ispn *Infinispan) GetCondition(condition string) *string {
	for _, c := range ispn.Status.Conditions {
		if c.Type == condition {
			return &c.Status
		}
	}
	return nil
}

// SetCondition set condition to status
func (ispn *Infinispan) SetCondition(condition, status, message string) bool {
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

// SetConditions set provided conditions to status
func (ispn *Infinispan) SetConditions(conds []InfinispanCondition) bool {
	changed := false
	for _, c := range conds {
		changed = changed || ispn.SetCondition(c.Type, c.Status, c.Message)
	}
	return changed
}

func (ispn *Infinispan) ApplyDefaults() {
	if ispn.Status.Conditions == nil {
		ispn.Status.Conditions = []InfinispanCondition{}
	}
	if ispn.Spec.Service.Type == "" {
		ispn.Spec.Service.Type = ServiceTypeCache
	}
	if ispn.Spec.Image == "" {
		ispn.Spec.Image = constants.DefaultImageName
	}
	if ispn.Spec.Container.Memory == "" {
		ispn.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
}

func (ispn *Infinispan) IsDataGrid() bool {
	return ServiceTypeDataGrid == ispn.Spec.Service.Type
}

func (ispn *Infinispan) IsConditionTrue(name string) bool {
	upgradeCondition := ispn.GetCondition(name)
	return upgradeCondition != nil && *upgradeCondition == "True"
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
