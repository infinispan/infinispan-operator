package manage

import (
	"fmt"
	"reflect"
	"strings"
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
	"k8s.io/utils/ptr"
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

	var updateReasons []string

	// Changes to podLabels
	currentLabels := provision.StatefulSetPodLabels(i.GetStatefulSetName(), i)
	previousLabels := statefulSet.Spec.Template.Labels
	if !reflect.DeepEqual(currentLabels, previousLabels) {
		updateReasons = append(updateReasons, "labels changed")
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
			updateReasons = append(updateReasons, "memory changed")
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
			updateReasons = append(updateReasons, "cpu changed")
		}
	}

	requestedOperand := ctx.Operand()
	// Changes to probes
	probedChanged := func(current, new *corev1.Probe) bool {
		if reflect.DeepEqual(current, new) {
			return false
		}
		*current = *new
		return true
	}
	if probedChanged(container.LivenessProbe, provision.PodLivenessProbe(i, requestedOperand)) ||
		probedChanged(container.ReadinessProbe, provision.PodReadinessProbe(i, requestedOperand)) ||
		probedChanged(container.StartupProbe, provision.PodStartupProbe(i, requestedOperand)) {
		updateReasons = append(updateReasons, "probes changed")
	}

	// Check if the base-image has been upgraded due to a CVE
	userProvidedImage := i.Spec.Image != nil
	operandMismatch := container.Image != requestedOperand.Image
	cveRespin := !userProvidedImage && requestedOperand.CVE && operandMismatch

	inPlaceRolling := i.Spec.Upgrades.Type == ispnv1.UpgradeTypeInPlaceRolling && operandMismatch

	if cveRespin || inPlaceRolling {
		ctx.Log().Info("New server version requested", "version", requestedOperand.Ref(), "cve", cveRespin)
		updateReasons = append(updateReasons, "image changed")
		container.Image = requestedOperand.Image

		err := ctx.UpdateInfinispan(func() {
			i.Status.Operand = OperandStatus(i, ispnv1.OperandPhasePending, requestedOperand)
		})
		if err != nil {
			return
		}
	}

	requestedGracePeriod := i.TerminationGracePeriodSeconds()
	if requestedGracePeriod == nil {
		defaultGracePeriod := int64(corev1.DefaultTerminationGracePeriodSeconds)
		requestedGracePeriod = &defaultGracePeriod
	}
	if !reflect.DeepEqual(spec.TerminationGracePeriodSeconds, requestedGracePeriod) {
		spec.TerminationGracePeriodSeconds = requestedGracePeriod
		updateReasons = append(updateReasons, "terminationGracePeriod changed")
	}

	if !reflect.DeepEqual(spec.Affinity, i.Affinity()) {
		spec.Affinity = i.Affinity()
		updateReasons = append(updateReasons, "affinity changed")
	}

	if !reflect.DeepEqual(spec.Tolerations, i.Tolerations()) {
		spec.Tolerations = i.Tolerations()
		updateReasons = append(updateReasons, "tolerations changed")
	}

	if !reflect.DeepEqual(spec.TopologySpreadConstraints, i.TopologySpreadConstraints()) {
		spec.TopologySpreadConstraints = i.TopologySpreadConstraints()
		updateReasons = append(updateReasons, "topologySpreadConstraints changed")
	}

	if spec.PriorityClassName != i.PriorityClassName() {
		spec.PriorityClassName = i.PriorityClassName()
		updateReasons = append(updateReasons, "priorityClassName changed")
	}

	// Note: removing serviceAccountName entirely (setting to "") may not take effect because
	// K8s auto-populates the deprecated spec.ServiceAccount field, which then re-sets ServiceAccountName.
	// Users can work around this by explicitly setting serviceAccountName to "default".
	if spec.ServiceAccountName != i.Spec.ServiceAccountName {
		spec.ServiceAccountName = i.Spec.ServiceAccountName
		updateReasons = append(updateReasons, "serviceAccountName changed")
	}

	if updateCmdArgs, err := updateStartupArgs(container, configFiles); err != nil {
		ctx.Requeue(err)
		return
	} else if updateCmdArgs {
		updateReasons = append(updateReasons, "startup args changed")
	}

	var hashVal string
	if configFiles.UserConfig.ServerConfig != "" {
		hashVal = hash.HashString(configFiles.UserConfig.ServerConfig)
	}
	if updateStatefulSetAnnotations(statefulSet, "checksum/overlayConfig", hashVal) {
		updateReasons = append(updateReasons, "overlay config changed")
	}
	if updateStatefulSetAnnotations(statefulSet, "checksum/credentialStore", hash.HashMap(configFiles.CredentialStoreEntries)) {
		updateReasons = append(updateReasons, "credential store changed")
	}
	podEnvs, podEnvHash := provision.PodEnvsAndHash(i, configFiles)
	if updateStatefulSetAnnotations(statefulSet, "checksum/podEnvs", podEnvHash) {
		updateReasons = append(updateReasons, "pod envs changed")
		container.Env = podEnvs
	}
	if applyOverlayConfigVolume(container, i.Spec.ConfigMapName, spec) {
		updateReasons = append(updateReasons, "config volume changed")
	}

	externalArtifactsUpd, err := provision.ApplyExternalArtifactsDownload(i, container, spec)
	if err != nil {
		ctx.Requeue(err)
		return
	}
	if externalArtifactsUpd {
		updateReasons = append(updateReasons, "external artifacts changed")
	}
	if provision.ApplyExternalDependenciesVolume(i, &container.VolumeMounts, spec) {
		updateReasons = append(updateReasons, "external dependencies changed")
	}

	// Validate identities Secret name changes
	if secretName, secretIndex := findSecretInVolume(spec, provision.IdentitiesVolumeName); secretIndex >= 0 && secretName != i.GetSecretName() {
		// Update new Secret name inside StatefulSet.Spec.Template
		statefulSet.Spec.Template.Spec.Volumes[secretIndex].Secret.SecretName = i.GetSecretName()
		statefulSet.Spec.Template.Annotations["updateDate"] = time.Now().String()
		updateReasons = append(updateReasons, "identities secret changed")
	}

	if i.IsAuthenticationEnabled() {
		if provision.AddVolumeForUserAuthentication(i, spec) {
			container.Env = append(container.Env,
				corev1.EnvVar{Name: "IDENTITIES_HASH", Value: hash.HashByte(configFiles.UserIdentities)},
			)
			updateReasons = append(updateReasons, "auth volume added")
		} else {
			// Validate Secret changes (by the hash of the identities.yaml key value)
			if updateStatefulSetEnv(container, statefulSet, "IDENTITIES_HASH", hash.HashByte(configFiles.UserIdentities)) {
				updateReasons = append(updateReasons, "identities hash changed")
			}
		}
	}

	if i.IsEncryptionEnabled() {
		if provision.AddVolumesForEncryption(i, spec) {
			updateReasons = append(updateReasons, "encryption volumes changed")
		}

		// Only trigger a StatefulSet rolling upgrade for Keystore and Truststore updates from 15.0.7 onwards as
		// Infinispan and JGroups automatically reload certificate changes
		if requestedOperand.UpstreamVersion.LT(consts.MinVersionAutomaticCertificateReloading) {
			if updateStatefulSetEnv(container, statefulSet, "KEYSTORE_HASH", hash.HashByte(configFiles.Keystore.PemFile)+hash.HashByte(configFiles.Keystore.File)) {
				updateReasons = append(updateReasons, "keystore hash changed")
			}

			if i.IsClientCertEnabled() {
				if updateStatefulSetEnv(container, statefulSet, "TRUSTSTORE_HASH", hash.HashByte(configFiles.Truststore.File)) {
					updateReasons = append(updateReasons, "truststore hash changed")
				}
			}
		}
	}

	if provision.AddXSiteTLSVolumes(ctx, i, statefulSet) {
		updateReasons = append(updateReasons, "xsite TLS volumes changed")
	}

	// Any update reason prior to this point will result in rollout
	rollingUpgrade := len(updateReasons) > 0

	// Ensure the deployment size is the same as the spec
	replicas := i.Spec.Replicas
	previousReplicas := *statefulSet.Spec.Replicas
	if previousReplicas != replicas {
		statefulSet.Spec.Replicas = &replicas
		log.Info("Replicas changed", "requested", replicas, "current", previousReplicas)
		updateReasons = append(updateReasons, "replicas changed")
	}

	if len(updateReasons) > 0 {
		// If updating the parameters results in a rolling upgrade, we can update the labels here too
		if rollingUpgrade {
			log.Info("StatefulSet spec changed, triggering rolling update", "reason", strings.Join(updateReasons, ", "))
			labelsForPod := i.PodLabels()
			labelsForPod[consts.StatefulSetPodLabel] = i.GetStatefulSetName()
			statefulSet.Spec.Template.Labels = labelsForPod

			// Configure new defaults when the user change results in rollout
			spec.AutomountServiceAccountToken = ptr.To(false)
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

func updateStartupArgs(ispnContainer *corev1.Container, config *pipeline.ConfigFiles) (bool, error) {
	newArgs := provision.BuildServerContainerArgs(config)
	if len(newArgs) == len(ispnContainer.Args) {
		var changed bool
		for i := range newArgs {
			if newArgs[i] != ispnContainer.Args[i] {
				changed = true
				break
			}
		}
		if !changed {
			return false, nil
		}
	}
	ispnContainer.Args = newArgs
	return true, nil
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
			if (*volumes)[volumePosition].ConfigMap.Name != configMapName {
				(*volumes)[volumePosition].ConfigMap.Name = configMapName
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
