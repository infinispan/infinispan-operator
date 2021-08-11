package infinispan

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ServerRoot                  = "/opt/infinispan/server"
	DataMountPath               = ServerRoot + "/data"
	DataMountVolume             = "data-volume"
	ConfigVolumeName            = "config-volume"
	EncryptKeystoreVolumeName   = "encrypt-volume"
	EncryptTruststoreVolumeName = "encrypt-trust-volume"
	IdentitiesVolumeName        = "identities-volume"
	AdminIdentitiesVolumeName   = "admin-identities-volume"
	InfinispanControllerName    = "controller_infinispan"

	EventReasonPrelimChecksFailed    = "PrelimChecksFailed"
	EventReasonLowPersistenceStorage = "LowPersistenceStorage"
	EventReasonEphemeralStorage      = "EphemeralStorageEnables"
	EventReasonParseValueProblem     = "ParseValueProblem"
	EventLoadBalancerUnsupported     = "LoadBalancerUnsupported"
)

func AddVolumeForUserAuthentication(i *infinispanv1.Infinispan, spec *corev1.PodSpec) bool {
	if _, index := findSecretInVolume(spec, IdentitiesVolumeName); !i.IsAuthenticationEnabled() || index >= 0 {
		return false
	}

	v := &spec.Volumes
	*v = append(*v, corev1.Volume{
		Name: IdentitiesVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: i.GetSecretName(),
			},
		},
	})

	vm := &spec.Containers[0].VolumeMounts
	*vm = append(*vm, corev1.VolumeMount{
		Name:      IdentitiesVolumeName,
		MountPath: consts.ServerUserIdentitiesRoot,
	})
	return true
}

func PodPorts() []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{ContainerPort: consts.InfinispanAdminPort, Name: consts.InfinispanAdminPortName, Protocol: corev1.ProtocolTCP},
		{ContainerPort: consts.InfinispanPingPort, Name: consts.InfinispanPingPortName, Protocol: corev1.ProtocolTCP},
		{ContainerPort: consts.InfinispanUserPort, Name: consts.InfinispanUserPortName, Protocol: corev1.ProtocolTCP},
	}
	return ports
}

func PodPortsWithXsite(i *infinispanv1.Infinispan) []corev1.ContainerPort {
	ports := PodPorts()
	if i.HasSites() {
		ports = append(ports, corev1.ContainerPort{ContainerPort: consts.CrossSitePort, Name: consts.CrossSitePortName, Protocol: corev1.ProtocolTCP})
	}
	return ports
}

func PodLivenessProbe() *corev1.Probe {
	return probe(5, 10, 10, 1, 80)
}

func PodReadinessProbe() *corev1.Probe {
	return probe(5, 10, 10, 1, 80)
}

func PodStartupProbe() *corev1.Probe {
	// Maximum 10 minutes (60 * 10s) to finish startup
	return probe(60, 10, 10, 1, 80)
}

func probe(failureThreshold, initialDelay, period, successThreshold, timeout int32) *corev1.Probe {
	return &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTP,
				Path:   consts.ServerHTTPHealthStatusPath,
				Port:   intstr.FromInt(consts.InfinispanAdminPort)},
		},
		FailureThreshold:    failureThreshold,
		InitialDelaySeconds: initialDelay,
		PeriodSeconds:       period,
		SuccessThreshold:    successThreshold,
		TimeoutSeconds:      timeout,
	}
}

func PodResources(spec infinispanv1.InfinispanContainerSpec) (*corev1.ResourceRequirements, error) {
	memory, err := resource.ParseQuantity(spec.Memory)
	if err != nil {
		return nil, err
	}
	cpuRequests, cpuLimits, err := spec.GetCpuResources()
	if err != nil {
		return nil, err
	}
	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    *cpuRequests,
			corev1.ResourceMemory: memory,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    *cpuLimits,
			corev1.ResourceMemory: memory,
		},
	}, nil
}

func PodEnv(i *infinispanv1.Infinispan, systemEnv *[]corev1.EnvVar) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: consts.ServerConfigPath},
		// Prevent the image from generating a user if authentication disabled
		{Name: "MANAGED_ENV", Value: "TRUE"},
		{Name: "JAVA_OPTIONS", Value: i.GetJavaOptions()},
		{Name: "EXTRA_JAVA_OPTIONS", Value: i.Spec.Container.ExtraJvmOpts},
		{Name: "DEFAULT_IMAGE", Value: consts.DefaultImageName},
		{Name: "ADMIN_IDENTITIES_PATH", Value: consts.ServerAdminIdentitiesPath},
	}

	// Adding additional variables listed in ADDITIONAL_VARS env var
	envVar, defined := os.LookupEnv("ADDITIONAL_VARS")
	if defined {
		var addVars []string
		err := json.Unmarshal([]byte(envVar), &addVars)
		if err == nil {
			for _, name := range addVars {
				value, defined := os.LookupEnv(name)
				if defined {
					envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
				}
			}
		}
	}

	if i.IsAuthenticationEnabled() {
		envVars = append(envVars, corev1.EnvVar{Name: "IDENTITIES_PATH", Value: consts.ServerUserIdentitiesPath})
	}

	if systemEnv != nil {
		envVars = append(envVars, *systemEnv...)
	}

	return envVars
}

// AddVolumeChmodInitContainer adds an init container that run chmod if needed
func AddVolumeChmodInitContainer(containerName, volumeName, mountPath string, spec *corev1.PodSpec) {
	if chmod, ok := os.LookupEnv("MAKE_DATADIR_WRITABLE"); ok && chmod == "true" {
		c := &spec.InitContainers
		*c = append(*c, chmodInitContainer(containerName, volumeName, mountPath))
	}
}

func chmodInitContainer(containerName, volumeName, mountPath string) corev1.Container {
	return corev1.Container{
		Image:   consts.InitContainerImageName,
		Name:    containerName,
		Command: []string{"sh", "-c", fmt.Sprintf("chmod -R g+w %s", mountPath)},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      volumeName,
			MountPath: mountPath,
		}},
	}
}

func AddVolumesForEncryption(i *infinispanv1.Infinispan, spec *corev1.PodSpec) {
	addSecretVolume := func(secretName, volumeName, mountPath string, spec *corev1.PodSpec) {
		v := &spec.Volumes

		if _, index := findSecretInVolume(spec, volumeName); index < 0 {
			*v = append(*v, corev1.Volume{Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretName,
					},
				},
			})
		}

		volumeMount := corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
		}

		index := -1
		volumeMounts := &spec.Containers[0].VolumeMounts
		for i, vm := range *volumeMounts {
			if vm.Name == volumeName {
				index = i
				break
			}
		}

		if index < 0 {
			*volumeMounts = append(*volumeMounts, volumeMount)
		} else {
			(*volumeMounts)[index] = volumeMount
		}
	}

	addSecretVolume(i.GetKeystoreSecretName(), EncryptKeystoreVolumeName, consts.ServerEncryptKeystoreRoot, spec)

	if i.IsClientCertEnabled() {
		addSecretVolume(i.GetTruststoreSecretName(), EncryptTruststoreVolumeName, consts.ServerEncryptTruststoreRoot, spec)
	}
}

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(pods []corev1.Pod, m *infinispanv1.Infinispan, cluster ispn.ClusterInterface) []infinispanv1.InfinispanCondition {
	var status []infinispanv1.InfinispanCondition
	clusterViews := make(map[string]bool)
	var errors []string
	// Avoid to inspect the system if we're still waiting for the pods
	if int32(len(pods)) < m.Spec.Replicas {
		errors = append(errors, fmt.Sprintf("Running %d pods. Needed %d", len(pods), m.Spec.Replicas))
	} else {
		for _, pod := range pods {
			if kube.IsPodReady(pod) {
				members, err := cluster.GetClusterMembers(pod.Name)
				if err == nil {
					sort.Strings(members)
					clusterView := strings.Join(members, ",")
					clusterViews[clusterView] = true
				} else {
					errors = append(errors, pod.Name+": "+err.Error())
				}
			} else {
				// Pod not ready, no need to query
				errors = append(errors, pod.Name+": pod not ready")
			}
		}
	}
	// Evaluating WellFormed condition
	wellformed := infinispanv1.InfinispanCondition{Type: infinispanv1.ConditionWellFormed}
	views := make([]string, len(clusterViews))
	i := 0
	for k := range clusterViews {
		views[i] = k
		i++
	}
	sort.Strings(views)
	if len(errors) == 0 {
		if len(views) == 1 {
			wellformed.Status = metav1.ConditionTrue
			wellformed.Message = "View: " + views[0]
		} else {
			wellformed.Status = metav1.ConditionFalse
			wellformed.Message = "Views: " + strings.Join(views, ",")
		}
	} else {
		wellformed.Status = metav1.ConditionUnknown
		wellformed.Message = "Errors: " + strings.Join(errors, ",") + " Views: " + strings.Join(views, ",")
	}
	status = append(status, wellformed)
	return status
}

func updateStatefulSetEnv(statefulSet *appsv1.StatefulSet, envName, newValue string) bool {
	env := &statefulSet.Spec.Template.Spec.Containers[0].Env
	envIndex := kube.GetEnvVarIndex(envName, env)
	if envIndex < 0 {
		// The env variable previously didn't exist, so append newValue to the end of the []EnvVar
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(*env, corev1.EnvVar{
			Name:  envName,
			Value: newValue,
		})
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		return true
	}
	prevEnvValue := (*env)[envIndex].Value
	if prevEnvValue != newValue {
		(*env)[envIndex].Value = newValue
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		return true
	}
	return false
}

func findSecretInVolume(pod *corev1.PodSpec, volumeName string) (string, int) {
	for i, volumes := range pod.Volumes {
		if volumes.Secret != nil && volumes.Name == volumeName {
			return volumes.Secret.SecretName, i
		}
	}
	return "", -1
}

// LabelsResource returns the labels that must me applied to the resource
func LabelsResource(name, resourceType string) map[string]string {
	m := map[string]string{"infinispan_cr": name, "clusterName": name}
	if resourceType != "" {
		m["app"] = resourceType
	}
	return m
}

func PodLabels(name string) map[string]string {
	return LabelsResource(name, "infinispan-pod")
}

func ServiceLabels(name string) map[string]string {
	return map[string]string{
		"clusterName": name,
		"app":         "infinispan-pod",
	}
}

func hashString(data string) string {
	hash := sha1.New()
	hash.Write([]byte(data))
	return hex.EncodeToString(hash.Sum(nil))
}

func HashByte(data []byte) string {
	hash := sha1.New()
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func HashMap(m map[string][]byte) string {
	hash := sha1.New()
	// Sort the map keys to ensure that the iteration order is the same on each call
	// Without this the computed sha will be different if the iteration order of the keys changes
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		hash.Write([]byte(k))
		hash.Write(v)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// TODO Replace with controllerutil.ContainsFinalizer after operator-sdk update (controller-runtime v0.6.1+)
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TODO Replace with controllerutil.RemoveFinalizer after operator-sdk update (controller-runtime v0.6.0+)
func remove(list []string, s string) []string {
	var slice []string
	for _, v := range list {
		if v != s {
			slice = append(slice, v)
		}
	}
	return slice
}
