package provision

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/blang/semver"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func PodPortsWithXsite(i *ispnv1.Infinispan) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{ContainerPort: consts.InfinispanAdminPort, Name: consts.InfinispanAdminPortName, Protocol: corev1.ProtocolTCP},
		{ContainerPort: consts.InfinispanPingPort, Name: consts.InfinispanPingPortName, Protocol: corev1.ProtocolTCP},
		{ContainerPort: consts.InfinispanUserPort, Name: consts.InfinispanUserPortName, Protocol: corev1.ProtocolTCP},
	}

	if i.IsJmxExposed() {
		ports = append(ports, corev1.ContainerPort{ContainerPort: consts.InfinispanJmxPort, Name: consts.InfinispanJmxPortName, Protocol: corev1.ProtocolTCP})
	}

	if i.HasSites() {
		ports = append(ports, corev1.ContainerPort{ContainerPort: consts.CrossSitePort, Name: consts.CrossSitePortName, Protocol: corev1.ProtocolTCP})
	}
	return ports
}

func PodLifecycle() *corev1.Lifecycle {
	if !consts.ThreadDumpPreStopFlag {
		return nil
	}

	return &corev1.Lifecycle{
		// Execute kill -3 on the Server process to obtain a thread dump in the logs
		PreStop: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/bash",
					"-c",
					"kill -3 1",
				},
			},
		},
	}
}

func PodLivenessProbe(i *ispnv1.Infinispan, operand version.Operand) *corev1.Probe {
	i.InitServiceContainer()
	return probe(i.Spec.Service.Container.LivenessProbe, operand)
}

func PodReadinessProbe(i *ispnv1.Infinispan, operand version.Operand) *corev1.Probe {
	i.InitServiceContainer()
	return probe(i.Spec.Service.Container.ReadinessProbe, operand)
}

func PodStartupProbe(i *ispnv1.Infinispan, operand version.Operand) *corev1.Probe {
	i.InitServiceContainer()
	return probe(i.Spec.Service.Container.StartupProbe, operand)
}

func probe(p ispnv1.ContainerProbeSpec, operand version.Operand) *corev1.Probe {
	var path string
	if operand.UpstreamVersion.GTE(semver.Version{Major: 15}) {
		path = "rest/v2/container/health/status"
	} else {
		path = "rest/v2/cache-managers/default/health/status"
	}
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Scheme: corev1.URISchemeHTTP,
				Path:   path,
				Port:   intstr.FromInt(consts.InfinispanAdminPort)},
		},
		FailureThreshold:    *p.FailureThreshold,
		InitialDelaySeconds: *p.InitialDelaySeconds,
		PeriodSeconds:       *p.PeriodSeconds,
		SuccessThreshold:    *p.SuccessThreshold,
		TimeoutSeconds:      *p.TimeoutSeconds,
	}
}

func TcpProbe(port, failureThreshold, initialDelay, period, successThreshold, timeout int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
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

func PodResources(spec ispnv1.InfinispanContainerSpec) (*corev1.ResourceRequirements, error) {
	memRequests, memLimits, err := spec.GetMemoryResources()
	if err != nil {
		return nil, err
	}

	req := &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: memRequests,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: memLimits,
		},
	}

	if spec.CPU != "" {
		cpuRequests, cpuLimits, err := spec.GetCpuResources()
		if err != nil {
			return nil, err
		}
		req.Requests[corev1.ResourceCPU] = cpuRequests
		req.Limits[corev1.ResourceCPU] = cpuLimits
	}
	return req, nil
}

func PodEnv(i *ispnv1.Infinispan, systemEnv *[]corev1.EnvVar) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		// Prevent the image from generating a user if authentication disabled
		{Name: "MANAGED_ENV", Value: "TRUE"},
		{Name: "JAVA_OPTIONS", Value: i.GetJavaOptions()},
		{Name: "CLI_JAVA_OPTIONS", Value: i.Spec.Container.CliExtraJvmOpts},
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
func AddVolumeForUserAuthentication(i *ispnv1.Infinispan, spec *corev1.PodSpec) bool {
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

	vm := &kube.GetContainer(InfinispanContainer, spec).VolumeMounts
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

func AddVolumesForEncryption(i *ispnv1.Infinispan, spec *corev1.PodSpec) {
	AddSecretVolume(i.GetKeystoreSecretName(), EncryptKeystoreVolumeName, consts.ServerEncryptKeystoreRoot, spec, InfinispanContainer)

	if i.IsClientCertEnabled() {
		AddSecretVolume(i.GetTruststoreSecretName(), EncryptTruststoreVolumeName, consts.ServerEncryptTruststoreRoot, spec, InfinispanContainer)
	}
}

// AddSecretVolume creates a volume to a secret
func AddSecretVolume(secretName, volumeName, mountPath string, spec *corev1.PodSpec, containerName string) {
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
	volumeMounts := &kube.GetContainer(containerName, spec).VolumeMounts
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

func findSecretInVolume(pod *corev1.PodSpec, volumeName string) (string, int) {
	for i, volumes := range pod.Volumes {
		if volumes.Secret != nil && volumes.Name == volumeName {
			return volumes.Secret.SecretName, i
		}
	}
	return "", -1
}
