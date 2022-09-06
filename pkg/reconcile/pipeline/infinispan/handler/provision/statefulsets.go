package provision

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	InfinispanContainer          = "infinispan"
	GossipRouterContainer        = "gossiprouter"
	DataMountPath                = consts.ServerRoot + "/data"
	OperatorConfMountPath        = consts.ServerRoot + "/conf/operator"
	DataMountVolume              = "data-volume"
	ConfigVolumeName             = "config-volume"
	EncryptKeystoreVolumeName    = "encrypt-volume"
	EncryptTruststoreVolumeName  = "encrypt-trust-volume"
	IdentitiesVolumeName         = "identities-volume"
	UserConfVolumeName           = "user-conf-volume"
	InfinispanSecurityVolumeName = "infinispan-security-volume"
	OverlayConfigMountPath       = consts.ServerRoot + "/conf/user"

	SiteTransportKeystoreVolumeName = "encrypt-transport-site-tls-volume"
	SiteRouterKeystoreVolumeName    = "encrypt-router-site-tls-volume"
	SiteTruststoreVolumeName        = "encrypt-truststore-site-tls-volume"
)

func ClusterStatefulSet(i *ispnv1.Infinispan, ctx pipeline.Context) {
	statefulSetName := i.GetStatefulSetName()
	// If StatefulSet already exists, continue to the next handler in the pipeline
	if err := ctx.Resources().Load(statefulSetName, &appsv1.StatefulSet{}); err == nil {
		return
	} else if client.IgnoreNotFound(err) != nil {
		ctx.Requeue(err)
		return
	}

	statefulSet, err := ClusterStatefulSetSpec(statefulSetName, i, ctx)
	if err != nil {
		ctx.Requeue(fmt.Errorf("unable to create StatefulSet spec: %w", err))
		return
	}

	if err := ctx.Resources().Create(statefulSet, true, pipeline.RetryOnErr); err != nil {
		return
	}

	_ = ctx.UpdateInfinispan(func() {
		i.Status.StatefulSetName = statefulSet.Name
	})
}

func ClusterStatefulSetSpec(statefulSetName string, i *ispnv1.Infinispan, ctx pipeline.Context) (*appsv1.StatefulSet, error) {
	labelsForPod := i.PodLabels()
	labelsForPod[consts.StatefulSetPodLabel] = statefulSetName

	annotationsForPod := i.PodAnnotations()
	annotationsForPod["updateDate"] = time.Now().String()

	// We can ignore the err here as the validating webhook ensures that the resources are valid
	podResources, _ := PodResources(i.Spec.Container)
	configFiles := ctx.ConfigFiles()

	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        statefulSetName,
			Namespace:   i.Namespace,
			Annotations: consts.DeploymentAnnotations,
			Labels:      map[string]string{},
		},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: labelsForPod,
			},
			Replicas: &i.Spec.Replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labelsForPod,
					Annotations: annotationsForPod,
				},
				Spec: corev1.PodSpec{
					Affinity: i.Spec.Affinity,
					Containers: []corev1.Container{{
						Image: i.ImageName(),
						Args:  BuildServerContainerArgs(ctx.ConfigFiles().UserConfig, ctx.FIPS()),
						Name:  InfinispanContainer,
						Env: PodEnv(i, &[]corev1.EnvVar{
							{Name: "CONFIG_HASH", Value: hash.HashString(configFiles.ServerBaseConfig, configFiles.ServerAdminConfig)},
							{Name: "ADMIN_IDENTITIES_HASH", Value: hash.HashByte(configFiles.AdminIdentities.IdentitiesFile)},
							{Name: "IDENTITIES_BATCH", Value: consts.ServerOperatorSecurity + "/" + consts.ServerIdentitiesBatchFilename},
						}),
						LivenessProbe:  PodLivenessProbe(),
						Ports:          PodPortsWithXsite(i),
						ReadinessProbe: PodReadinessProbe(),
						StartupProbe:   PodStartupProbe(),
						Resources:      *podResources,
						VolumeMounts: []corev1.VolumeMount{{
							Name:      ConfigVolumeName,
							MountPath: OperatorConfMountPath,
						}, {
							Name:      InfinispanSecurityVolumeName,
							MountPath: consts.ServerOperatorSecurity,
						}, {
							Name:      DataMountVolume,
							MountPath: DataMountPath,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: ConfigVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: i.GetConfigName()},
							},
						},
					}, {
						Name: InfinispanSecurityVolumeName,
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: i.GetInfinispanSecuritySecretName(),
							},
						},
					},
					},
				},
			},
		},
	}

	if err := addDataMountVolume(ctx, i, statefulSet); err != nil {
		return nil, err
	}

	container := kube.GetContainer(InfinispanContainer, &statefulSet.Spec.Template.Spec)
	if _, err := ApplyExternalArtifactsDownload(i, container, &statefulSet.Spec.Template.Spec); err != nil {
		return nil, err
	}
	ApplyExternalDependenciesVolume(i, &container.VolumeMounts, &statefulSet.Spec.Template.Spec)
	addUserIdentities(ctx, i, statefulSet)
	addUserConfigVolumes(ctx, i, statefulSet)
	addTLS(ctx, i, statefulSet)
	addXSiteTLS(ctx, i, statefulSet)
	return statefulSet, nil
}

func addUserIdentities(ctx pipeline.Context, i *ispnv1.Infinispan, statefulset *appsv1.StatefulSet) {
	// Only append IDENTITIES_HASH and secret volume if authentication is enabled
	spec := &statefulset.Spec.Template.Spec
	ispnContainer := kube.GetContainer(InfinispanContainer, spec)
	if AddVolumeForUserAuthentication(i, spec) {
		ispnContainer.Env = append(ispnContainer.Env,
			corev1.EnvVar{
				Name:  "IDENTITIES_HASH",
				Value: hash.HashByte(ctx.ConfigFiles().UserIdentities),
			})
	}
}

func addDataMountVolume(ctx pipeline.Context, i *ispnv1.Infinispan, statefulset *appsv1.StatefulSet) error {
	if i.IsEphemeralStorage() {
		volumes := &statefulset.Spec.Template.Spec.Volumes
		ephemeralVolume := corev1.Volume{
			Name: DataMountVolume,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
		*volumes = append(*volumes, ephemeralVolume)
		return nil
	}

	var pvSize resource.Quantity
	if i.IsDataGrid() && i.StorageSize() != "" {
		pvSize, _ = resource.ParseQuantity(i.StorageSize())
	} else {
		_, memLimit, _ := i.Spec.Container.GetMemoryResources()
		if consts.DefaultPVSize.Cmp(memLimit) < 0 {
			pvSize = memLimit
		} else {
			pvSize = consts.DefaultPVSize
		}
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DataMountVolume,
			Namespace: i.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: pvSize,
				},
			},
		},
	}
	if err := ctx.Resources().SetControllerReference(pvc); err != nil {
		return err
	}
	pvc.OwnerReferences[0].BlockOwnerDeletion = pointer.BoolPtr(false)
	// Set a storage class if specified
	if storageClassName := i.StorageClassName(); storageClassName != "" {
		if err := ctx.Resources().LoadGlobal(storageClassName, &storagev1.StorageClass{}); err != nil {
			return fmt.Errorf("unable to load StorageClass %s: %w", storageClassName, err)
		}
		pvc.Spec.StorageClassName = &storageClassName
	}
	statefulset.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{*pvc}

	AddVolumeChmodInitContainer("data-chmod-pv", DataMountVolume, DataMountPath, &statefulset.Spec.Template.Spec)
	return nil
}

func addUserConfigVolumes(ctx pipeline.Context, i *ispnv1.Infinispan, statefulset *appsv1.StatefulSet) {
	if !i.UserConfigDefined() {
		return
	}

	statefulset.Annotations["checksum/overlayConfig"] = hash.HashString(ctx.ConfigFiles().UserConfig.ServerConfig)
	volumes := &statefulset.Spec.Template.Spec.Volumes
	*volumes = append(*volumes, corev1.Volume{
		Name: UserConfVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: i.Spec.ConfigMapName},
			},
		}})

	container := kube.GetContainer(InfinispanContainer, &statefulset.Spec.Template.Spec)
	volumeMounts := &container.VolumeMounts
	*volumeMounts = append(*volumeMounts, corev1.VolumeMount{
		Name:      UserConfVolumeName,
		MountPath: OverlayConfigMountPath,
	})
}

func BuildServerContainerArgs(userConfig pipeline.UserConfig, fips bool) []string {
	var args strings.Builder

	// Preallocate a buffer to speed up string building (saves code from growing the memory dynamically)
	args.Grow(110)

	// Check if the user defined a custom log4j config
	args.WriteString(" -l ")
	if userConfig.Log4j != "" {
		args.WriteString("user/log4j.xml")
	} else {
		args.WriteString(OperatorConfMountPath)
		args.WriteString("/log4j.xml")
	}

	// Apply Operator user config
	args.WriteString(" -c operator/infinispan-base.xml")
	// Apply user custom config
	if userConfig.ServerConfig != "" {
		args.WriteString(" -c user/")
		args.WriteString(userConfig.ServerConfigFileName)
	}
	// Apply Operator Admin config
	args.WriteString(" -c operator/infinispan-admin.xml")

	// If running in FIPS mode disable OpenSSL as it's incompatible with the NSS PKCS#11 store
	if fips {
		args.WriteString(" -Dorg.infinispan.openssl=false")
	}
	return strings.Fields(args.String())
}

func addTLS(ctx pipeline.Context, i *ispnv1.Infinispan, statefulSet *appsv1.StatefulSet) {
	if i.IsEncryptionEnabled() {
		AddVolumesForEncryption(i, &statefulSet.Spec.Template.Spec)
		configFiles := ctx.ConfigFiles()
		ispnContainer := kube.GetContainer(InfinispanContainer, &statefulSet.Spec.Template.Spec)
		ispnContainer.Env = append(ispnContainer.Env,
			corev1.EnvVar{
				Name: "KEYSTORE_HASH",
				// Compute the hash using both the Pem and P12 file for simplicity. Only one field should be set at anyone time
				Value: hash.HashByte(configFiles.Keystore.PemFile) + hash.HashByte(configFiles.Keystore.File),
			})

		if ctx.FIPS() {
			ks := ctx.ConfigFiles().Keystore
			// The FIPS scripts only requires the directory containing the keystore file(s)
			ksPath := filepath.Dir(ks.Path)
			addFipsInitContainer("keystore-nss-database", ksPath, ks.Password, EncryptKeystoreVolumeName, consts.ServerEncryptKeystoreRoot, i, statefulSet)
		}

		if i.IsClientCertEnabled() {
			ispnContainer.Env = append(ispnContainer.Env,
				corev1.EnvVar{
					Name:  "TRUSTSTORE_HASH",
					Value: hash.HashByte(configFiles.Truststore.File),
				})

			if ctx.FIPS() {
				ts := ctx.ConfigFiles().Truststore
				addFipsInitContainer("truststore-nss-database", consts.ServerEncryptTruststoreRoot, ts.Password, EncryptTruststoreVolumeName, consts.ServerEncryptTruststoreRoot, i, statefulSet)
			}
		}
	}
}

func addFipsInitContainer(name, ksPath, ksSecret, mountName, mountPath string, i *ispnv1.Infinispan, statefulSet *appsv1.StatefulSet) {
	initContainers := &statefulSet.Spec.Template.Spec.InitContainers
	*initContainers = append(*initContainers, corev1.Container{
		Name:    name,
		Image:   i.ImageName(),
		Command: []string{"/opt/infinispan/bin/init_fips_keystore.sh"},
		Args:    []string{"-p", ksSecret, ksPath},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      mountName,
			MountPath: mountPath,
		}},
	})
}

func addXSiteTLS(ctx pipeline.Context, i *ispnv1.Infinispan, statefulSet *appsv1.StatefulSet) {
	if i.IsSiteTLSEnabled() {
		spec := &statefulSet.Spec.Template.Spec
		AddSecretVolume(i.GetSiteTransportSecretName(), SiteTransportKeystoreVolumeName, consts.SiteTransportKeyStoreRoot, spec, InfinispanContainer)

		if ctx.FIPS() {
			ks := ctx.ConfigFiles().Transport.Keystore
			addFipsInitContainer("transport-keystore-nss-database", consts.SiteTransportKeyStoreRoot, ks.Password, SiteTransportKeystoreVolumeName, consts.SiteTransportKeyStoreRoot, i, statefulSet)
		}

		if ctx.ConfigFiles().Transport.Truststore != nil {
			AddSecretVolume(i.GetSiteTrustoreSecretName(), SiteTruststoreVolumeName, consts.SiteTrustStoreRoot, spec, InfinispanContainer)

			if ctx.FIPS() {
				ts := ctx.ConfigFiles().Transport.Truststore
				addFipsInitContainer("transport-truststore-nss-database", consts.SiteTrustStoreRoot, ts.Password, SiteTruststoreVolumeName, consts.SiteTrustStoreRoot, i, statefulSet)
			}
		}
	}
}
