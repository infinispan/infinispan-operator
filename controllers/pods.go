package controllers

import (
	"encoding/json"
	"fmt"
	"os"

	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

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

func GossipRouterLivenessProbe() *corev1.Probe {
	return TcpProbe(consts.CrossSitePort, 5, 5, 10, 1, 60)
}

func GossipRouterReadinessProbe() *corev1.Probe {
	return TcpProbe(consts.CrossSitePort, 5, 5, 10, 1, 60)
}

func GossipRouterStartupProbe() *corev1.Probe {
	return TcpProbe(consts.CrossSitePort, 5, 5, 10, 1, 60)
}

func TcpProbe(port, failureThreshold, initialDelay, period, successThreshold, timeout int32) *corev1.Probe {
	return &corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.IntOrString{IntVal: port},
			},
		},
		FailureThreshold:    failureThreshold,
		InitialDelaySeconds: initialDelay,
		PeriodSeconds:       period,
		SuccessThreshold:    successThreshold,
		TimeoutSeconds:      timeout,
	}
}

func PodResources(spec infinispanv1.InfinispanContainerSpec) (*corev1.ResourceRequirements, error) {
	memRequests, memLimits, err := spec.GetMemoryResources()
	if err != nil {
		return nil, err
	}

	cpuRequests, cpuLimits, err := spec.GetCpuResources()
	if err != nil {
		return nil, err
	}
	return &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    cpuRequests,
			corev1.ResourceMemory: memRequests,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    cpuLimits,
			corev1.ResourceMemory: memLimits,
		},
	}, nil
}

func PodEnv(i *infinispanv1.Infinispan, systemEnv *[]corev1.EnvVar) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		// Prevent the image from generating a user if authentication disabled
		{Name: "MANAGED_ENV", Value: "TRUE"},
		{Name: "JAVA_OPTIONS", Value: i.GetJavaOptions()},
		{Name: "EXTRA_JAVA_OPTIONS", Value: i.Spec.Container.ExtraJvmOpts},
		{Name: "DEFAULT_IMAGE", Value: consts.DefaultImageName},
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

	if systemEnv != nil {
		envVars = append(envVars, *systemEnv...)
	}

	return envVars
}

// AddVolumeForUserAuthentication returns true if the volume has been added
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
	AddSecretVolume(i.GetKeystoreSecretName(), EncryptKeystoreVolumeName, consts.ServerEncryptKeystoreRoot, spec)

	if i.IsClientCertEnabled() {
		AddSecretVolume(i.GetTruststoreSecretName(), EncryptTruststoreVolumeName, consts.ServerEncryptTruststoreRoot, spec)
	}
}

// AddSecretVolume creates a volume to a secret
func AddSecretVolume(secretName, volumeName, mountPath string, spec *corev1.PodSpec) {
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
