package controllers

import (
	"strings"

	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
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

func applyExternalDependenciesVolume(ispn *infinispanv1.Infinispan, spec *corev1.PodSpec) (updated bool) {
	ispnContainer := GetContainer(InfinispanContainer, spec)
	volumes := &spec.Volumes
	volumeMounts := &ispnContainer.VolumeMounts
	volumePosition := findVolume(*volumes, CustomLibrariesVolumeName)
	if ispn.HasDependenciesVolume() && volumePosition < 0 {
		*volumeMounts = append(*volumeMounts, corev1.VolumeMount{Name: CustomLibrariesVolumeName, MountPath: CustomLibrariesMountPath, ReadOnly: true})
		*volumes = append(*volumes, corev1.Volume{Name: CustomLibrariesVolumeName, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: ispn.Spec.Dependencies.VolumeClaimName, ReadOnly: true}}})
		updated = true
	} else if !ispn.HasDependenciesVolume() && volumePosition >= 0 {
		volumeMountPosition := findVolumeMount(*volumeMounts, CustomLibrariesVolumeName)
		*volumes = append(spec.Volumes[:volumePosition], spec.Volumes[volumePosition+1:]...)
		*volumeMounts = append(ispnContainer.VolumeMounts[:volumeMountPosition], ispnContainer.VolumeMounts[volumeMountPosition+1:]...)
		updated = true
	}
	return
}

func applyExternalArtifactsDownload(ispn *infinispanv1.Infinispan, spec *corev1.PodSpec) (updated bool, retErr error) {
	initContainers := &spec.InitContainers
	ispnContainer := GetContainer(InfinispanContainer, spec)
	volumes := &spec.Volumes
	volumeMounts := &ispnContainer.VolumeMounts
	containerPosition := kube.ContainerIndex(*initContainers, ExternalArtifactsDownloadInitContainer)
	if ispn.HasExternalArtifacts() {
		serverLibs := serverLibs(ispn)
		if containerPosition >= 0 {
			if spec.InitContainers[containerPosition].Env[0].Value != serverLibs {
				spec.InitContainers[containerPosition].Env[0].Value = serverLibs
				updated = true
			}
		} else {
			*initContainers = append(*initContainers, corev1.Container{
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
			})
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

func serverLibs(i *infinispanv1.Infinispan) string {
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
