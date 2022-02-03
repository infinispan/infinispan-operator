package controllers

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	rice "github.com/GeertJohan/go.rice"
	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
)

const (
	CustomLibrariesMountPath               = "/opt/infinispan/server/lib/custom-libraries"
	CustomLibrariesVolumeName              = "custom-libraries"
	ExternalArtifactsMountPath             = "/opt/infinispan/server/lib/external-artifacts"
	ExternalArtifactsVolumeName            = "external-artifacts"
	ExternalArtifactsDownloadInitContainer = "external-artifacts-download"
	ExternalArtifactsHashValidationCommand = "echo %s %s | %ssum -c"
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
		*volumeMounts = append((*volumeMounts)[:volumeMountPosition], (*volumeMounts)[volumeMountPosition+1:]...)
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
		extractCommands, err := externalArtifactsExtractCommand(ispn)
		if err != nil {
			retErr = err
			return
		}
		if containerPosition >= 0 {
			if spec.InitContainers[containerPosition].Args[0] != extractCommands {
				spec.InitContainers[containerPosition].Args = []string{extractCommands}
				updated = true
			}
		} else {
			*initContainers = append(*initContainers, corev1.Container{
				Image:   ispn.ImageName(),
				Name:    ExternalArtifactsDownloadInitContainer,
				Command: []string{"sh", "-c"},
				Args:    []string{extractCommands},
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

func externalArtifactsExtractCommand(ispn *infinispanv1.Infinispan) (string, error) {
	box, err := rice.FindBox("resources")
	if err != nil {
		return "", err
	}
	dependenciesTemplate, err := box.String("dependencies.gotmpl")
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("init-container").Funcs(template.FuncMap{
		"hashCmd": func(artifact infinispanv1.InfinispanExternalArtifacts, fileName string) (string, error) {
			if artifact.Hash == "" {
				return "", nil
			}

			hashParts := strings.Split(artifact.Hash, ":")
			if len(hashParts) != 2 {
				return "", fmt.Errorf("expected hash to be in the format `<hash-type>:<hash>`")
			}
			return fmt.Sprintf(ExternalArtifactsHashValidationCommand, hashParts[1], fileName, hashParts[0]), nil
		},
	}).Parse(dependenciesTemplate)

	if err != nil {
		return "", err
	}

	var tpl bytes.Buffer
	err = tmpl.Execute(&tpl, struct {
		MountPath string
		Artifacts []infinispanv1.InfinispanExternalArtifacts
	}{
		MountPath: ExternalArtifactsMountPath,
		Artifacts: ispn.Spec.Dependencies.Artifacts,
	})

	if err != nil {
		return "", err
	}
	return tpl.String(), nil
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
