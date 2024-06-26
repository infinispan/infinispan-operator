package provision

import (
	"fmt"
	"reflect"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
)

const (
	CustomLibrariesMountPath               = "/opt/infinispan/server/lib/custom-libraries"
	CustomLibrariesVolumeName              = "custom-libraries"
	ExternalArtifactsLibsRoot              = "server/lib/external-artifacts"
	ExternalArtifactsMountPath             = "/opt/infinispan/" + ExternalArtifactsLibsRoot + "/lib"
	ExternalArtifactsVolumeName            = "external-artifacts"
	ExternalArtifactsDownloadInitContainer = "external-artifacts-download"
)

func ApplyExternalDependenciesVolume(ispn *ispnv1.Infinispan, volumeMounts *[]corev1.VolumeMount, spec *corev1.PodSpec) (updated bool) {
	volumes := &spec.Volumes
	volumePosition := findVolume(*volumes, CustomLibrariesVolumeName)
	if ispn.HasDependenciesVolume() && volumePosition < 0 {
		*volumeMounts = append(*volumeMounts, corev1.VolumeMount{Name: CustomLibrariesVolumeName, MountPath: CustomLibrariesMountPath, ReadOnly: true})
		*volumes = append(*volumes, corev1.Volume{Name: CustomLibrariesVolumeName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: ispn.Spec.Dependencies.VolumeClaimName, ReadOnly: true}}})
		updated = true
	} else if !ispn.HasDependenciesVolume() && volumePosition >= 0 {
		volumeMountPosition := findVolumeMount(*volumeMounts, CustomLibrariesVolumeName)
		*volumes = append(spec.Volumes[:volumePosition], spec.Volumes[volumePosition+1:]...)
		*volumeMounts = append((*volumeMounts)[:volumeMountPosition], (*volumeMounts)[volumeMountPosition+1:]...)
		updated = true
	}
	return
}

func ApplyExternalArtifactsDownload(ispn *ispnv1.Infinispan, ispnContainer *corev1.Container, spec *corev1.PodSpec) (updated bool, retErr error) {
	initContainers := &spec.InitContainers
	volumes := &spec.Volumes
	volumeMounts := &ispnContainer.VolumeMounts
	containerPosition := kube.ContainerIndex(*initContainers, ExternalArtifactsDownloadInitContainer)

	if ispn.HasExternalArtifacts() {
		initContainerResources, err := getInitContainerResources(ispn)
		if err != nil {
			return false, fmt.Errorf("unable to calculate dependencies initContainer resources: %w", err)
		}

		serverLibs := serverLibs(ispn)
		if containerPosition >= 0 {
			if spec.InitContainers[containerPosition].Env[0].Value != serverLibs {
				spec.InitContainers[containerPosition].Env[0].Value = serverLibs
				updated = true
			}
			if !reflect.DeepEqual(&spec.InitContainers[containerPosition].Resources, initContainerResources) {
				spec.InitContainers[containerPosition].Resources = *initContainerResources
				updated = true
			}

		} else {
			container := corev1.Container{
				Image: ispn.ImageName(),
				Name:  ExternalArtifactsDownloadInitContainer,
				Env: []corev1.EnvVar{
					{Name: "SERVER_LIBS", Value: serverLibs},
					{Name: "SERVER_LIBS_DIR", Value: ExternalArtifactsLibsRoot},
					{Name: "INIT_CONTAINER", Value: "TRUE"},
					{Name: "MANAGED_ENV", Value: "TRUE"},
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      ExternalArtifactsVolumeName,
					MountPath: ExternalArtifactsMountPath,
				}},
			}
			if initContainerResources != nil {
				container.Resources = *initContainerResources
			}
			*initContainers = append(*initContainers, container)
			*volumeMounts = append(*volumeMounts, corev1.VolumeMount{Name: ExternalArtifactsVolumeName, MountPath: ExternalArtifactsMountPath, ReadOnly: true})
			*volumes = append(*volumes, corev1.Volume{Name: ExternalArtifactsVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
			updated = true
		}
	} else if containerPosition >= 0 {
		volumePosition := findVolume(*volumes, ExternalArtifactsVolumeName)
		volumeMountPosition := findVolumeMount(*volumeMounts, ExternalArtifactsVolumeName)
		*initContainers = append((*initContainers)[:containerPosition], (*initContainers)[containerPosition+1:]...)
		*volumes = append(spec.Volumes[:volumePosition], spec.Volumes[volumePosition+1:]...)
		*volumeMounts = append((*volumeMounts)[:volumeMountPosition], (*volumeMounts)[volumeMountPosition+1:]...)
		updated = true
	}
	return
}

func getInitContainerResources(i *ispnv1.Infinispan) (*corev1.ResourceRequirements, error) {
	if i.Spec.Dependencies == nil {
		return nil, nil
	}

	spec := i.Spec.Dependencies.InitContainer
	if spec.CPU == "" && spec.Memory == "" {
		return &corev1.ResourceRequirements{}, nil
	}

	req := &corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{},
		Requests: corev1.ResourceList{},
	}

	if spec.Memory != "" {
		memRequests, memLimits, err := spec.MemoryResources()
		if err != nil {
			return req, err
		}
		req.Requests[corev1.ResourceMemory] = memRequests
		req.Limits[corev1.ResourceMemory] = memLimits
	}

	if spec.CPU != "" {
		cpuRequests, cpuLimits, err := spec.CpuResources()
		if err != nil {
			return req, err
		}
		req.Requests[corev1.ResourceCPU] = cpuRequests
		req.Limits[corev1.ResourceCPU] = cpuLimits
	}
	return req, nil
}

func serverLibs(i *ispnv1.Infinispan) string {
	if !i.HasExternalArtifacts() {
		return ""
	}
	var libs strings.Builder
	for _, artifact := range i.Spec.Dependencies.Artifacts {
		if artifact.Url != "" {
			libs.WriteString(artifact.Url)
		} else {
			libs.WriteString(artifact.Maven)
		}
		if artifact.Hash != "" {
			hashParts := strings.Split(artifact.Hash, ":")
			libs.WriteString("|")
			libs.WriteString(hashParts[0])
			libs.WriteString("|")
			libs.WriteString(hashParts[1])
		}
		libs.WriteString(" ")
	}
	return libs.String()
}

func findVolume(volumes []corev1.Volume, volumeName string) int {
	for i, volume := range volumes {
		if volume.Name == volumeName {
			return i
		}
	}
	return -1
}

func findVolumeMount(volumeMounts []corev1.VolumeMount, volumeMountName string) int {
	for i, volumeMount := range volumeMounts {
		if volumeMount.Name == volumeMountName {
			return i
		}
	}
	return -1
}
