package manage

import (
	"fmt"
	"reflect"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

func StatefulSetRollingUpgrade(i *ispnv1.Infinispan, ctx pipeline.Context) {
	// Skip StatefulSet upgrades until HotRod Rolling upgrade is complete
	if i.IsHotRodUpgrade() {
		return
	}
	log := ctx.Log()
	configFiles := ctx.ConfigFiles()

	statefulSet := &appsv1.StatefulSet{}
	if err := ctx.Resources().Load(i.GetStatefulSetName(), statefulSet, pipeline.InvalidateCache); err != nil {
		if errors.IsNotFound(err) {
			// No existing StatefulSet so nothing todo
			return
		}
		ctx.Requeue(fmt.Errorf("unable to retrieve StatefulSet in StatefulSetRollingUpgrade: %w", err))
		return
	}

	updateNeeded := false
	rollingUpgrade := true

	// Ensure the deployment size is the same as the spec
	replicas := i.Spec.Replicas
	previousReplicas := *statefulSet.Spec.Replicas
	if previousReplicas != replicas {
		statefulSet.Spec.Replicas = &replicas
		log.Info("replicas changed, update i", "replicas", replicas, "previous replicas", previousReplicas)
		updateNeeded = true
		rollingUpgrade = false
	}

	// Changes to statefulset.spec.template.spec.containers[].resources
	spec := &statefulSet.Spec.Template.Spec
	container := kube.GetContainer(provision.InfinispanContainer, spec)
	res := container.Resources
	ispnContr := &i.Spec.Container
	if ispnContr.Memory != "" {
		memRequests, memLimits, _ := i.Spec.Container.GetMemoryResources()
		previousMemRequests := res.Requests["memory"]
		previousMemLimits := res.Limits["memory"]
		if memRequests.Cmp(previousMemRequests) != 0 || memLimits.Cmp(previousMemLimits) != 0 {
			res.Requests["memory"] = memRequests
			res.Limits["memory"] = memLimits
			log.Info("memory changed, update i", "memLim", memLimits, "cpuReq", memRequests, "previous cpuLim", previousMemLimits, "previous cpuReq", previousMemRequests)
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			updateNeeded = true
		}
	}
	if ispnContr.CPU != "" {
		cpuReq, cpuLim, _ := i.Spec.Container.GetCpuResources()
		previousCPUReq := res.Requests["cpu"]
		previousCPULim := res.Limits["cpu"]
		if cpuReq.Cmp(previousCPUReq) != 0 || cpuLim.Cmp(previousCPULim) != 0 {
			res.Requests["cpu"] = cpuReq
			res.Limits["cpu"] = cpuLim
			log.Info("cpu changed, update i", "cpuLim", cpuLim, "cpuReq", cpuReq, "previous cpuLim", previousCPULim, "previous cpuReq", previousCPUReq)
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			updateNeeded = true
		}
	}

	// Check if the base-image has been upgraded due to a CVE
	requestedOperand := ctx.Operand()
	if requestedOperand.CVE && container.Image != requestedOperand.Image {
		ctx.Log().Info(fmt.Sprintf("CVE release '%s'. StatefulSet Rolling upgrade required", requestedOperand.Ref()))
		updateNeeded = true
		container.Image = requestedOperand.Image

		err := ctx.UpdateInfinispan(func() {
			i.Status.Operand.Version = requestedOperand.Ref()
			i.Status.Operand.Image = requestedOperand.Image
			i.Status.Operand.Phase = ispnv1.OperandPhasePending
		})
		if err != nil {
			return
		}
	}

	if !reflect.DeepEqual(spec.Affinity, i.Spec.Affinity) {
		spec.Affinity = i.Spec.Affinity
		updateNeeded = true
	}

	// Validate ConfigMap changes (by the hash of the i.yaml key value)
	updateNeeded = updateStatefulSetEnv(container, statefulSet, "CONFIG_HASH", hash.HashString(configFiles.ServerBaseConfig, configFiles.ServerAdminConfig)) || updateNeeded
	updateNeeded = updateStatefulSetEnv(container, statefulSet, "ADMIN_IDENTITIES_HASH", hash.HashByte(configFiles.AdminIdentities.IdentitiesFile)) || updateNeeded
	updateNeeded = updateStartupCmdArgs(i, container, ctx) || updateNeeded

	var hashVal string
	if configFiles.UserConfig.ServerConfig != "" {
		hashVal = hash.HashString(configFiles.UserConfig.ServerConfig)
	}
	updateNeeded = updateStatefulSetAnnotations(statefulSet, "checksum/overlayConfig", hashVal) || updateNeeded
	updateNeeded = applyOverlayConfigVolume(container, i.Spec.ConfigMapName, spec) || updateNeeded

	externalArtifactsUpd, err := provision.ApplyExternalArtifactsDownload(i, container, spec)
	if err != nil {
		ctx.Requeue(err)
		return
	}
	updateNeeded = externalArtifactsUpd || updateNeeded
	updateNeeded = provision.ApplyExternalDependenciesVolume(i, &container.VolumeMounts, spec) || updateNeeded

	// Validate identities Secret name changes
	if secretName, secretIndex := findSecretInVolume(spec, provision.IdentitiesVolumeName); secretIndex >= 0 && secretName != i.GetSecretName() {
		// Update new Secret name inside StatefulSet.Spec.Template
		statefulSet.Spec.Template.Spec.Volumes[secretIndex].Secret.SecretName = i.GetSecretName()
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		updateNeeded = true
	}

	if i.IsAuthenticationEnabled() {
		if provision.AddVolumeForUserAuthentication(i, spec) {
			container.Env = append(container.Env,
				corev1.EnvVar{Name: "IDENTITIES_HASH", Value: hash.HashByte(configFiles.UserIdentities)},
			)
			updateNeeded = true
		} else {
			// Validate Secret changes (by the hash of the identities.yaml key value)
			updateNeeded = updateStatefulSetEnv(container, statefulSet, "IDENTITIES_HASH", hash.HashByte(configFiles.UserIdentities)) || updateNeeded
		}
	}

	if i.IsEncryptionEnabled() {
		provision.AddVolumesForEncryption(i, spec)
		updateNeeded = updateStatefulSetEnv(container, statefulSet, "KEYSTORE_HASH", hash.HashByte(configFiles.Keystore.PemFile)+hash.HashByte(configFiles.Keystore.File)) || updateNeeded

		if i.IsClientCertEnabled() {
			updateNeeded = updateStatefulSetEnv(container, statefulSet, "TRUSTSTORE_HASH", hash.HashByte(configFiles.Truststore.File)) || updateNeeded
		}
	}

	// Validate extra Java options changes
	if updateStatefulSetEnv(container, statefulSet, "EXTRA_JAVA_OPTIONS", ispnContr.ExtraJvmOpts) {
		updateStatefulSetEnv(container, statefulSet, "JAVA_OPTIONS", i.GetJavaOptions())
		updateNeeded = true
	}
	if updateStatefulSetEnv(container, statefulSet, "CLI_JAVA_OPTIONS", ispnContr.CliExtraJvmOpts) {
		updateNeeded = true
	}

	if updateNeeded {
		log.Info("updateNeeded")
		// If updating the parameters results in a rolling upgrade, we can update the labels here too
		if rollingUpgrade {
			labelsForPod := i.PodLabels()
			labelsForPod[consts.StatefulSetPodLabel] = i.GetStatefulSetName()
			statefulSet.Spec.Template.Labels = labelsForPod
		}
		err := ctx.Resources().Update(statefulSet, pipeline.RetryOnErr)
		if err != nil {
			log.Error(err, "failed to update StatefulSet", "StatefulSet.Name", statefulSet.Name)
		}
	}
}

func updateStatefulSetEnv(ispnContainer *corev1.Container, statefulSet *appsv1.StatefulSet, envName, newValue string) bool {
	env := &ispnContainer.Env
	envIndex := kube.GetEnvVarIndex(envName, env)
	if envIndex < 0 {
		// The env variable previously didn't exist, so append newValue to the end of the []EnvVar
		*env = append(*env, corev1.EnvVar{
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

func updateStartupCmdArgs(i *ispnv1.Infinispan, ispnContainer *corev1.Container, ctx pipeline.Context) bool {
	arrayChanged := func(l, r []string) bool {
		if len(l) == len(r) {
			for i := range l {
				if l[i] != r[i] {
					return true
				}
			}
			return false
		}
		return true
	}

	newArgs := provision.BuildServerContainerArgs(i, ctx)
	newCmd := provision.BuildServerContainerCmd(i, ctx)
	if arrayChanged(newArgs, ispnContainer.Args) || arrayChanged(newCmd, ispnContainer.Command) {
		ispnContainer.Args = newArgs
		ispnContainer.Command = newCmd
		return true
	}
	return false
}

func updateStatefulSetAnnotations(statefulSet *appsv1.StatefulSet, name, value string) bool {
	// Annotation has non-empty value
	if value != "" {
		// map doesn't exists, must be created
		if statefulSet.Annotations == nil {
			statefulSet.Annotations = make(map[string]string)
		}
		if statefulSet.Annotations[name] != value {
			statefulSet.Annotations[name] = value
			statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
			return true
		}
	} else {
		// Annotation doesn't exist
		if statefulSet.Annotations == nil || statefulSet.Annotations[name] == "" {
			return false
		}
		// delete it
		delete(statefulSet.Annotations, name)
		return true
	}
	return false
}

// TODO create generic function for adding/removing volumes from PodSpec
func applyOverlayConfigVolume(ispnContainer *corev1.Container, configMapName string, spec *corev1.PodSpec) bool {
	volumes := &spec.Volumes
	volumeMounts := &ispnContainer.VolumeMounts
	volumePosition := findVolume(*volumes, provision.UserConfVolumeName)
	if configMapName != "" {
		// Add the overlay volume if needed
		if volumePosition < 0 {
			*volumeMounts = append(*volumeMounts, corev1.VolumeMount{Name: provision.UserConfVolumeName, MountPath: provision.OverlayConfigMountPath})
			*volumes = append(*volumes, corev1.Volume{
				Name: provision.UserConfVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: configMapName}}}})
			return true
		} else {
			// Update the overlay volume if needed
			if (*volumes)[volumePosition].VolumeSource.ConfigMap.Name != configMapName {
				(*volumes)[volumePosition].VolumeSource.ConfigMap.Name = configMapName
				return true
			}
		}
	}
	// Delete overlay volume mount if no more needed
	if configMapName == "" && volumePosition >= 0 {
		volumeMountPosition := findVolumeMount(*volumeMounts, provision.UserConfVolumeName)
		*volumes = append(spec.Volumes[:volumePosition], spec.Volumes[volumePosition+1:]...)
		*volumeMounts = append((*volumeMounts)[:volumeMountPosition], (*volumeMounts)[volumeMountPosition+1:]...)
		return true
	}
	return false
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

func findSecretInVolume(pod *corev1.PodSpec, volumeName string) (string, int) {
	for i, volumes := range pod.Volumes {
		if volumes.Secret != nil && volumes.Name == volumeName {
			return volumes.Secret.SecretName, i
		}
	}
	return "", -1
}
