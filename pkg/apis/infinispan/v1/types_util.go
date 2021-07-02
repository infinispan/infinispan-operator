package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ImageType string

const (
	// Container image based on JDK
	ImageTypeJVM ImageType = "JVM"

	// Container image based on Quarkus native runtime
	ImageTypeNative ImageType = "Native"
)

const (
	// PodTargetLabels labels propagated to pods
	PodTargetLabels string = "infinispan.org/podTargetLabels"
	// TargetLabels labels propagated to services/ingresses/routes
	TargetLabels string = "infinispan.org/targetLabels"
	// OperatorPodTargetLabels labels propagated by the operator to pods
	OperatorPodTargetLabels string = "infinispan.org/operatorPodTargetLabels"
	// OperatorTargetLabels labels propagated by the operator to services/ingresses/routes
	OperatorTargetLabels string = "infinispan.org/operatorTargetLabels"
	// OperatorTargetLabelsEnvVarName is the name of the envvar containg operator label/value map for services/ingresses/routes
	OperatorTargetLabelsEnvVarName string = "INFINISPAN_OPERATOR_TARGET_LABELS"
	// OperatorPodTargetLabelsEnvVarName is the name of the envvar containg operator label/value map for pods
	OperatorPodTargetLabelsEnvVarName string = "INFINISPAN_OPERATOR_POD_TARGET_LABELS"

	MaxRouteObjectNameLength = 63

	// ServiceMonitoringAnnotation defines if we need to create ServiceMonitor or not
	ServiceMonitoringAnnotation string = "infinispan.org/monitoring"

	SiteServiceNameTemplate = "%v-site"
	SiteServiceFQNTemplate  = "%s.%s.svc.cluster.local"
)

// equals compares two ConditionType's case insensitive
func (a ConditionType) equals(b ConditionType) bool {
	return strings.EqualFold(strings.ToLower(string(a)), strings.ToLower(string(b)))
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

// RemoveCondition remove condition from Status
func (ispn *Infinispan) RemoveCondition(condition ConditionType) bool {
	for idx := range ispn.Status.Conditions {
		c := &ispn.Status.Conditions[idx]
		if c.Type.equals(condition) {
			ispn.Status.Conditions = append(ispn.Status.Conditions[:idx], ispn.Status.Conditions[idx+1:]...)
			return true
		}
	}
	return false
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
	if ispn.Spec.Container.Memory == "" {
		ispn.Spec.Container.Memory = constants.DefaultMemorySize.String()
	}
	if ispn.Spec.Container.CPU == "" {
		cpuLimitString := toMilliDecimalQuantity(constants.DefaultCPULimit)
		ispn.Spec.Container.CPU = cpuLimitString.String()
	}
	if ispn.IsDataGrid() {
		if ispn.Spec.Service.Container == nil {
			ispn.Spec.Service.Container = &InfinispanServiceContainerSpec{}
		}
		if ispn.Spec.Service.Container.Storage == nil {
			ispn.Spec.Service.Container.Storage = pointer.StringPtr(consts.DefaultPVSize.String())
		}
	}
	if ispn.Spec.Security.EndpointAuthentication == nil {
		ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(true)
	}
	if *ispn.Spec.Security.EndpointAuthentication {
		ispn.Spec.Security.EndpointSecretName = ispn.GetSecretName()
	} else if ispn.IsGeneratedSecret() {
		ispn.Spec.Security.EndpointSecretName = ""
	}
}

func (ispn *Infinispan) ApplyMonitoringAnnotation() {
	if ispn.Annotations == nil {
		ispn.Annotations = make(map[string]string)
	}
	_, ok := ispn.GetAnnotations()[ServiceMonitoringAnnotation]
	if !ok {
		ispn.Annotations[ServiceMonitoringAnnotation] = strconv.FormatBool(true)
	}
}

// ApplyEndpointEncryptionSettings compute the EndpointEncryption object
func (ispn *Infinispan) ApplyEndpointEncryptionSettings(servingCertsMode string, reqLogger logr.Logger) {
	// Populate EndpointEncryption if serving cert service is available
	encryption := ispn.Spec.Security.EndpointEncryption
	if servingCertsMode == "openshift.io" && (!ispn.IsEncryptionCertSourceDefined() || ispn.IsEncryptionCertFromService()) {
		if encryption == nil {
			encryption = &EndpointEncryption{}
			ispn.Spec.Security.EndpointEncryption = encryption
		}
		if encryption.CertServiceName == "" || encryption.Type == "" {
			reqLogger.Info("Serving certificate service present. Configuring into Infinispan CR")
			encryption.Type = CertificateSourceTypeService
			encryption.CertServiceName = "service.beta.openshift.io"
		}
		if encryption.CertSecretName == "" {
			encryption.CertSecretName = ispn.Name + "-cert-secret"
		}
	}

	if encryption != nil {
		if encryption.ClientCert == "" {
			encryption.ClientCert = ClientCertNone
		}

		if encryption.ClientCert != ClientCertNone && encryption.ClientCertSecretName == "" {
			encryption.ClientCertSecretName = ispn.Name + "-client-cert-secret"
		}
	}
}

// PreliminaryChecks performs all the possible initial checks
func (ispn *Infinispan) PreliminaryChecks() (*reconcile.Result, error) {
	// If a CacheService is requested, checks that the pods have enough memory
	if ispn.Spec.Service.Type == ServiceTypeCache {
		memoryQ, err := resource.ParseQuantity(ispn.Spec.Container.Memory)
		if err != nil {
			return &reconcile.Result{RequeueAfter: consts.DefaultRequeueOnWrongSpec}, err
		}
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

func (ispn *Infinispan) ImageName() string {
	if ispn.Spec.Image != nil && *ispn.Spec.Image != "" {
		return *ispn.Spec.Image
	}
	return constants.DefaultImageName
}

func (ispn *Infinispan) ImageType() ImageType {
	if strings.Contains(ispn.ImageName(), consts.NativeImageMarker) {
		return ImageTypeNative
	}
	return ImageTypeJVM
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
	externalServiceName := fmt.Sprintf("%s-external", ispn.Name)
	if ispn.IsExposed() && ispn.GetExposeType() == ExposeTypeRoute && len(externalServiceName)+len(ispn.Namespace) >= MaxRouteObjectNameLength {
		return externalServiceName[0:MaxRouteObjectNameLength-len(ispn.Namespace)-2] + "a"
	}
	return externalServiceName
}

func (ispn *Infinispan) GetServiceName() string {
	return ispn.Name
}

func (ispn *Infinispan) GetPingServiceName() string {
	return fmt.Sprintf("%s-ping", ispn.Name)
}

func (ispn *Infinispan) IsCache() bool {
	return ServiceTypeCache == ispn.Spec.Service.Type
}

func (ispn *Infinispan) HasSites() bool {
	return ispn.IsDataGrid() && ispn.Spec.Service.Sites != nil && len(ispn.Spec.Service.Sites.Locations) > 0
}

// GetRemoteSiteLocations returns remote site locations
func (ispn *Infinispan) GetRemoteSiteLocations() (remoteLocations map[string]InfinispanSiteLocationSpec) {
	remoteLocations = make(map[string]InfinispanSiteLocationSpec)
	for _, location := range ispn.Spec.Service.Sites.Locations {
		if ispn.Spec.Service.Sites.Local.Name != location.Name {
			remoteLocations[location.Name] = location
		}
	}
	return
}

// GetSiteLocationsName returns all site locations (remote and local) name
func (ispn *Infinispan) GetSiteLocationsName() (locations []string) {
	for _, location := range ispn.Spec.Service.Sites.Locations {
		if ispn.Spec.Service.Sites.Local.Name == location.Name {
			continue
		}
		locations = append(locations, location.Name)
	}
	locations = append(locations, ispn.Spec.Service.Sites.Local.Name)
	sort.Strings(locations)
	return
}

// IsExposed ...
func (ispn *Infinispan) IsExposed() bool {
	return ispn.Spec.Expose != nil && ispn.Spec.Expose.Type != ""
}

func (ispn *Infinispan) GetExposeType() ExposeType {
	return ispn.Spec.Expose.Type
}

func (ispn *Infinispan) GetSiteServiceName() string {
	return fmt.Sprintf(SiteServiceNameTemplate, ispn.Name)
}

func (ispn *Infinispan) GetRemoteSiteServiceName(locationName string) string {
	return fmt.Sprintf(SiteServiceNameTemplate, ispn.GetRemoteSiteClusterName(locationName))
}

func (ispn *Infinispan) GetRemoteSiteServiceFQN(locationName string) string {
	return fmt.Sprintf(SiteServiceFQNTemplate, ispn.GetRemoteSiteServiceName(locationName), ispn.GetRemoteSiteNamespace(locationName))
}

func (ispn *Infinispan) GetRemoteSiteNamespace(locationName string) string {
	remoteLocation := ispn.GetRemoteSiteLocations()[locationName]
	return consts.GetWithDefault(remoteLocation.Namespace, ispn.Namespace)
}

func (ispn *Infinispan) GetRemoteSiteClusterName(locationName string) string {
	remoteLocation := ispn.GetRemoteSiteLocations()[locationName]
	return consts.GetWithDefault(remoteLocation.ClusterName, ispn.Name)
}

// GetEndpointScheme returns the protocol scheme used by the Infinispan cluster
func (ispn *Infinispan) GetEndpointScheme() string {
	endPointSchema := corev1.URISchemeHTTP
	if ispn.IsEncryptionEnabled() {
		endPointSchema = corev1.URISchemeHTTPS
	}
	return strings.ToLower(string(endPointSchema))
}

// GetSecretName returns the secret name associated with a server
func (ispn *Infinispan) GetSecretName() string {
	if ispn.Spec.Security.EndpointSecretName == "" {
		return ispn.GenerateSecretName()
	}
	return ispn.Spec.Security.EndpointSecretName
}

func (ispn *Infinispan) GenerateSecretName() string {
	return fmt.Sprintf("%v-%v", ispn.GetName(), consts.GeneratedSecretSuffix)
}

// GetAdminSecretName returns the admin secret name associated with a server
func (ispn *Infinispan) GetAdminSecretName() string {
	return fmt.Sprintf("%v-generated-operator-secret", ispn.GetName())
}

func (ispn *Infinispan) GetAuthorizationRoles() []AuthorizationRole {
	if !ispn.IsAuthorizationEnabled() {
		return make([]AuthorizationRole, 0)
	}
	return ispn.Spec.Security.Authorization.Roles
}

func (ispn *Infinispan) IsAuthorizationEnabled() bool {
	return ispn.Spec.Security.Authorization != nil && ispn.Spec.Security.Authorization.Enabled
}

func (ispn *Infinispan) IsAuthenticationEnabled() bool {
	return ispn.Spec.Security.EndpointAuthentication == nil || *ispn.Spec.Security.EndpointAuthentication
}

func (ispn *Infinispan) IsClientCertEnabled() bool {
	return ispn.IsEncryptionEnabled() && ispn.Spec.Security.EndpointEncryption.ClientCert != "" && ispn.Spec.Security.EndpointEncryption.ClientCert != ClientCertNone
}

// IsGeneratedSecret verifies that the Secret should be generated by the controller
func (ispn *Infinispan) IsGeneratedSecret() bool {
	return ispn.Spec.Security.EndpointSecretName == ispn.GenerateSecretName()
}

// GetConfigName returns the ConfigMap name for the cluster
func (ispn *Infinispan) GetConfigName() string {
	return fmt.Sprintf("%v-configuration", ispn.Name)
}

// GetServiceMonitorName returns the ServiceMonitor name for the cluster
func (ispn *Infinispan) GetServiceMonitorName() string {
	return fmt.Sprintf("%v-monitor", ispn.Name)
}

// GetKeystoreSecretName ...
func (ispn *Infinispan) GetKeystoreSecretName() string {
	if ispn.Spec.Security.EndpointEncryption == nil {
		return ""
	}
	return ispn.Spec.Security.EndpointEncryption.CertSecretName
}

func (ispn *Infinispan) GetTruststoreSecretName() string {
	if ispn.Spec.Security.EndpointEncryption == nil {
		return ""
	}
	return ispn.Spec.Security.EndpointEncryption.ClientCertSecretName
}

func (spec *InfinispanContainerSpec) GetCpuResources() (*resource.Quantity, *resource.Quantity, error) {
	cpuLimits, err := resource.ParseQuantity(spec.CPU)
	if err != nil {
		return nil, nil, err
	}
	cpuRequestsMillis := cpuLimits.MilliValue()
	cpuRequests := toMilliDecimalQuantity(cpuRequestsMillis)
	return &cpuRequests, &cpuLimits, nil
}

func (ispn *Infinispan) GetJavaOptions() string {
	switch ispn.Spec.Service.Type {
	case ServiceTypeDataGrid:
		return ispn.Spec.Container.ExtraJvmOpts
	case ServiceTypeCache:
		switch ispn.ImageType() {
		case ImageTypeJVM:
			return fmt.Sprintf(consts.CacheServiceJavaOptions, consts.CacheServiceFixedMemoryXmxMb, consts.CacheServiceFixedMemoryXmxMb, consts.CacheServiceMaxRamMb,
				consts.CacheServiceMinHeapFreeRatio, consts.CacheServiceMaxHeapFreeRatio, ispn.Spec.Container.ExtraJvmOpts)
		case ImageTypeNative:
			return fmt.Sprintf(consts.CacheServiceNativeJavaOptions, consts.CacheServiceFixedMemoryXmxMb, consts.CacheServiceFixedMemoryXmxMb, ispn.Spec.Container.ExtraJvmOpts)
		}
	}
	return ""
}

// GetLogCategoriesForConfig return a map of log category for the Infinispan configuration
func (ispn *Infinispan) GetLogCategoriesForConfig() map[string]string {
	if ispn.Spec.Logging != nil {
		categories := ispn.Spec.Logging.Categories
		if categories != nil {
			copied := make(map[string]string, len(categories))
			for category, level := range categories {
				copied[category] = string(level)
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

func (ispn *Infinispan) IsUpgradeNeeded(logger logr.Logger) bool {
	if ispn.IsUpgradeCondition() {
		if ispn.GetCondition(ConditionStopping).Status == metav1.ConditionFalse {
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

	return false
}

func (ispn *Infinispan) IsEncryptionEnabled() bool {
	ee := ispn.Spec.Security.EndpointEncryption
	return ee != nil && ee.Type != CertificateSourceTypeNoneNoEncryption
}

// IsEncryptionCertFromService returns true if encryption certificates comes from a cluster service
func (ispn *Infinispan) IsEncryptionCertFromService() bool {
	ee := ispn.Spec.Security.EndpointEncryption
	return ee != nil && (ee.Type == CertificateSourceTypeService || ee.Type == CertificateSourceTypeServiceLowCase)
}

// IsEncryptionCertSourceDefined returns true if encryption certificates source is defined
func (ispn *Infinispan) IsEncryptionCertSourceDefined() bool {
	ee := ispn.Spec.Security.EndpointEncryption
	return ee != nil && ee.Type != ""
}

func toMilliDecimalQuantity(value int64) resource.Quantity {
	return *resource.NewMilliQuantity(value, resource.DecimalSI)
}

// IsEphemeralStorage
func (ispn *Infinispan) IsEphemeralStorage() bool {
	cont := ispn.Spec.Service.Container
	if cont != nil {
		return cont.EphemeralStorage
	}
	return false
}

// StorageClassName returns a storage class name if it defined
func (ispn *Infinispan) StorageClassName() string {
	sc := ispn.Spec.Service.Container
	if sc != nil {
		return sc.StorageClassName
	}
	return ""
}

// StorageSize returns persistence storage size if it defined
func (ispn *Infinispan) StorageSize() string {
	sc := ispn.Spec.Service.Container
	if sc != nil && sc.Storage != nil {
		return *sc.Storage
	}
	return ""
}

// AddLabelsForPods adds to the user maps the labels defined for pods in the infinispan CR. New values override old ones in map.
func (ispn *Infinispan) AddLabelsForPods(uMap map[string]string) {
	addLabelsFor(ispn, PodTargetLabels, uMap)
}

// AddLabelsForServices adds to the user maps the labels defined for services and ingresses/routes in the infinispan CR. New values override old ones in map.
func (ispn *Infinispan) AddLabelsForServices(uMap map[string]string) {
	addLabelsFor(ispn, TargetLabels, uMap)
}

// AddOperatorLabelsForPods adds to the user maps the labels defined for pods in the infinispan CR by the operator. New values override old ones in map.
func (ispn *Infinispan) AddOperatorLabelsForPods(uMap map[string]string) {
	addLabelsFor(ispn, OperatorPodTargetLabels, uMap)
}

// AddOperatorLabelsForServices adds to the user maps the labels defined for services and ingresses/routes in the infinispan CR. New values override old ones in map.
func (ispn *Infinispan) AddOperatorLabelsForServices(uMap map[string]string) {
	addLabelsFor(ispn, OperatorTargetLabels, uMap)
}

func addLabelsFor(ispn *Infinispan, target string, uMap map[string]string) {
	if ispn.Annotations == nil {
		return
	}
	labels := ispn.Annotations[target]
	for _, label := range strings.Split(labels, ",") {
		tLabel := strings.Trim(label, " ")
		if lval := strings.Trim(ispn.Labels[tLabel], " "); lval != "" {
			uMap[tLabel] = lval
		}
	}
}

// ApplyOperatorLabels applies operator labels to be propagated to pods and services
// Env vars INFINISPAN_OPERATOR_TARGET_LABELS, INFINISPAN_OPERATOR_POD_TARGET_LABELS
// must contain a json map of labels, the former will be applied to services/ingresses/routes, the latter to pods
func (ispn *Infinispan) ApplyOperatorLabels() error {
	var errStr string
	err := applyLabels(ispn, OperatorTargetLabelsEnvVarName, OperatorTargetLabels)
	if err != nil {
		errStr = fmt.Sprintf("Error unmarshalling %s environment variable: %v\n", OperatorTargetLabelsEnvVarName, err)
	}
	err = applyLabels(ispn, OperatorPodTargetLabelsEnvVarName, OperatorPodTargetLabels)
	if err != nil {
		errStr = errStr + fmt.Sprintf("Error unmarshalling %s environment variable: %v", OperatorPodTargetLabelsEnvVarName, err)
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}

func applyLabels(ispn *Infinispan, envvar, annotationName string) error {
	labels := os.Getenv(envvar)
	if labels == "" {
		return nil
	}
	labelMap := make(map[string]string)
	err := json.Unmarshal([]byte(labels), &labelMap)
	if err == nil {
		if len(labelMap) > 0 {
			svcLabels := ""
			if ispn.Labels == nil {
				ispn.Labels = make(map[string]string)
			}
			for name, value := range labelMap {
				ispn.Labels[name] = value
				svcLabels = svcLabels + name + ","
			}
			if ispn.Annotations == nil {
				ispn.Annotations = make(map[string]string)
			}
			ispn.Annotations[annotationName] = strings.TrimRight(svcLabels, ", ")
		}
	}
	return err
}

// HasCustomLibraries true if custom libraries are defined
func (ispn *Infinispan) HasCustomLibraries() bool {
	return ispn.Spec.Dependencies != nil && ispn.Spec.Dependencies.VolumeClaimName != ""
}

// IsServiceMonitorEnabled validates that "infinispan.org/monitoring":true annotation defines or not
func (ispn *Infinispan) IsServiceMonitorEnabled() bool {
	monitor, ok := ispn.GetAnnotations()[ServiceMonitoringAnnotation]
	if ok {
		isMonitor, err := strconv.ParseBool(monitor)
		return err == nil && isMonitor
	}
	return false
}
