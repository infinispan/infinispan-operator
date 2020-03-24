package util

import (
	"context"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func FindConfigMap(volumes []corev1.Volume, name string) *corev1.ConfigMapVolumeSource {
	for _, volume := range volumes {
		if volume.ConfigMap != nil && volume.Name == name {
			return volume.ConfigMap
		}
	}
	return nil
}

func FindSecret(volumes []corev1.Volume, name string) *corev1.SecretVolumeSource {
	for _, volume := range volumes {
		if volume.Secret != nil && volume.Name == name {
			return volume.Secret
		}
	}
	return nil
}

func FindPort(ports []corev1.ContainerPort, name string) *corev1.ContainerPort {
	for _, port := range ports {
		if port.Name == name {
			return port.DeepCopy()
		}
	}
	return nil
}

func FindVolumeMount(volumeMounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for _, volumeMount := range volumeMounts {
		if volumeMount.Name == name {
			return volumeMount.DeepCopy()
		}
	}
	return nil
}

func FindPersistentVolumeClaim(volumeMountClaims []corev1.PersistentVolumeClaim, name string) *corev1.PersistentVolumeClaim {
	for _, volumeMountClaim := range volumeMountClaims {
		if volumeMountClaim.Name == name {
			return volumeMountClaim.DeepCopy()
		}
	}
	return nil
}

func FindEnv(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for _, env := range envs {
		if env.Name == name {
			return env.DeepCopy()
		}
	}
	return nil
}

func GetStatefulSet(cl client.Client, ns types.NamespacedName) *appsv1.StatefulSet {
	sset := &appsv1.StatefulSet{}
	err := cl.Get(context.TODO(), ns, sset)
	if err == nil {
		return sset
	}
	return nil
}

func GetService(cl client.Client, ns types.NamespacedName) *corev1.Service {
	service := &corev1.Service{}
	err := cl.Get(context.TODO(), ns, service)
	if err == nil {
		return service
	}
	return nil
}

func GetConfigMap(cl client.Client, ns types.NamespacedName) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), ns, configMap)
	if err == nil {
		return configMap
	}
	return nil
}

func GetSecret(cl client.Client, ns types.NamespacedName) *corev1.Secret {
	secret := &corev1.Secret{}
	err := cl.Get(context.TODO(), ns, secret)
	if err == nil {
		return secret
	}
	return nil
}

func GetInfinispan(client client.Client, ns types.NamespacedName) *ispnv1.Infinispan {
	ispn := &ispnv1.Infinispan{}
	err := client.Get(context.TODO(), ns, ispn)
	if err == nil {
		return ispn
	}
	return nil
}

func GetInfinispanConfiguration(configMap corev1.ConfigMap) *ispnutil.InfinispanConfiguration {
	configuration := ispnutil.InfinispanConfiguration{}
	configData := configMap.Data["infinispan.yaml"]
	yaml.Unmarshal([]byte(configData), &configuration)

	return &configuration
}

func ProcessSecretsData(cl client.Client, namespace string) {
	secrets := &corev1.SecretList{}
	listOps := &client.ListOptions{Namespace: namespace}
	cl.List(context.TODO(), secrets, listOps)
	for _, secret := range secrets.Items {
		currData := make(map[string][]byte)
		for key, data := range secret.Data {
			currData[key] = data
		}
		for key, str := range secret.StringData {
			currData[key] = []byte(str)
		}
		secret.Data = currData
		cl.Update(context.TODO(), &secret)
	}
}
