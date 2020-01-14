package unit

import (
	"context"
	"strings"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	operatorName                = "default-infinispan"
	operatorPodName             = "infinispan-pod"
	operatorNamespace           = "myproject"
	operatorCpuDefault          = "500m"
	operatorMemoryDefault       = "512Mi"
	operatorCpu                 = "100m"
	operatorMemory              = "1024Mi"
	operatorReplicas      int32 = 1

	defaultPingPort   int32 = 8888
	defaultHotrodPort int32 = 11222
)

var defaultJavaOptions = []string{"-Xmx200M", "-Xms200M", "-XX:MaxRAM=420M", "-Dsun.zip.disableMemoryMapping=true", "-XX:+UseSerialGC", "-XX:MinHeapFreeRatio=5", "-XX:MaxHeapFreeRatio=10"}

var basicInfinispan = ispnv1.Infinispan{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "infinispan.org/v1",
		Kind:       "Infinispan",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      operatorName,
		Namespace: operatorNamespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Replicas: operatorReplicas,
	},
}

var basicInfinispanWithService = ispnv1.Infinispan{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "infinispan.org/v1",
		Kind:       "Infinispan",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      operatorName,
		Namespace: operatorNamespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Type: ispnv1.ServiceTypeDataGrid,
		},
		Container: ispnv1.InfinispanContainerSpec{
			CPU:    operatorCpu,
			Memory: operatorMemory,
		},
		Replicas: operatorReplicas,
	},
}

var req = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Name:      operatorName,
		Namespace: operatorNamespace,
	},
}

func TestBasicConfiguration(t *testing.T) {
	res, r := reconcileInfinispan(basicInfinispan, t)

	assert.True(t, res.Requeue, "reconcile did not requeue request as expected")

	sset := &appsv1.StatefulSet{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, sset)

	assert.Nilf(t, err, "get StatefulSet: (%v)", err)

	assert.Equalf(t, basicInfinispan.Spec.Replicas, int32(len(sset.Spec.Template.Spec.Containers)), "must be (%b) containers count", operatorReplicas)
	assert.Equal(t, "infinispan", sset.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, "infinispan/server:latest", sset.Spec.Template.Spec.Containers[0].Image)
	assert.Equalf(t, operatorCpuDefault, sset.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String(), "must be (%v) CPU units", operatorCpuDefault)
	assert.Equalf(t, operatorMemoryDefault, sset.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String(), "must be (%v) memory size", operatorMemoryDefault)

	assert.Equal(t, operatorPodName, sset.Spec.Selector.MatchLabels["app"])
	assert.Equal(t, operatorName, sset.Spec.Selector.MatchLabels["clusterName"])
	assert.Equal(t, operatorName, sset.Spec.Selector.MatchLabels["infinispan_cr"])

	assert.NotNil(t, findConfigMap(sset.Spec.Template.Spec.Volumes, "config-volume"), "Config Volume must be defined")
	assert.Equal(t, operatorName+"-configuration", findConfigMap(sset.Spec.Template.Spec.Volumes, "config-volume").Name)
	assert.NotNil(t, findSecret(sset.Spec.Template.Spec.Volumes, "identities-volume"), "Identities Volume must be defined")
	assert.Equal(t, operatorName+"-generated-secret", findSecret(sset.Spec.Template.Spec.Volumes, "identities-volume").SecretName)

	assert.Equal(t, defaultPingPort, findPort(sset.Spec.Template.Spec.Containers[0].Ports, "ping").ContainerPort, "ping port not defined")
	assert.Equal(t, defaultHotrodPort, findPort(sset.Spec.Template.Spec.Containers[0].Ports, "hotrod").ContainerPort, "hotrod port not defined")

	assert.Equal(t, "/etc/config", findVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "config-volume").MountPath, "config volume not defined as expected")
	assert.Equal(t, "/etc/security", findVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "identities-volume").MountPath, "identities volume not defined as expected")
	assert.Equal(t, "/opt/infinispan/server/data", findVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, operatorName).MountPath, "data volume not defined as expected")

	assert.Equal(t, "/etc/config/infinispan.yaml", findEnv(sset.Spec.Template.Spec.Containers[0].Env, "CONFIG_PATH").Value, "CONFIG_PATH not defined as expected")
	assert.Equal(t, "/etc/security/identities.yaml", findEnv(sset.Spec.Template.Spec.Containers[0].Env, "IDENTITIES_PATH").Value, "IDENTITIES_PATH not defined as expected")
	assert.ElementsMatch(t, defaultJavaOptions, strings.Fields(findEnv(sset.Spec.Template.Spec.Containers[0].Env, "JAVA_OPTIONS").Value))
	assert.Empty(t, findEnv(sset.Spec.Template.Spec.Containers[0].Env, "EXTRA_JAVA_OPTIONS").Value)

	assert.Equal(t, defaultHotrodPort, sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Port.IntVal, "Liveness probe HTTP port not defined")
	assert.Equal(t, "/rest/v2/cache-managers/default/health/status", sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Path, "Liveness probe HTTP path not defined")
	assert.Equal(t, corev1.URIScheme("HTTP"), sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Scheme, "Liveness probe Scheme not defined")

	assert.Equal(t, defaultHotrodPort, sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Port.IntVal, "Readiness probe HTTP port not defined")
	assert.Equal(t, "/rest/v2/cache-managers/default/health/status", sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path, "Readiness probe HTTP path not defined")
	assert.Equal(t, corev1.URIScheme("HTTP"), sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Scheme, "Readiness probe Scheme not defined")

	configName := types.NamespacedName{
		Name:      operatorName + "-configuration",
		Namespace: operatorNamespace,
	}
	config := &corev1.ConfigMap{}
	configuration := ispnutil.InfinispanConfiguration{}
	err = r.Client.Get(context.TODO(), configName, config)

	assert.Nilf(t, err, "get ConfigMap: (%v)", err)
	configData := config.Data["infinispan.yaml"]
	assert.NotNil(t, configData)

	yaml.Unmarshal([]byte(configData), &configuration)
	assert.Equal(t, operatorName, configuration.ClusterName)
	assert.Equal(t, basicInfinispan.APIVersion, config.OwnerReferences[0].APIVersion)
	assert.Equal(t, basicInfinispan.Kind, config.OwnerReferences[0].Kind)
	assert.Equal(t, operatorName, config.OwnerReferences[0].Name)

	secretName := types.NamespacedName{
		Name:      operatorName + "-generated-secret",
		Namespace: operatorNamespace,
	}
	secret := &corev1.Secret{}
	identities := ispnutil.Identities{}
	err = r.Client.Get(context.TODO(), secretName, secret)

	assert.Nil(t, err, "get Identities Secret: (%v)", err)
	secretData := secret.StringData["identities.yaml"]
	assert.NotNil(t, secretData)

	yaml.Unmarshal([]byte(secretData), &identities)
	assert.Equal(t, 2, len(identities.Credentials))
	assert.Equal(t, basicInfinispan.APIVersion, secret.OwnerReferences[0].APIVersion)
	assert.Equal(t, basicInfinispan.Kind, secret.OwnerReferences[0].Kind)
	assert.Equal(t, operatorName, secret.OwnerReferences[0].Name)

}

func reconcileInfinispan(ispnv ispnv1.Infinispan, t *testing.T) (reconcile.Result, infinispan.ReconcileInfinispan) {
	objects := []runtime.Object{&ispnv}

	scheme := scheme.Scheme
	scheme.AddKnownTypes(ispnv1.SchemeGroupVersion, &ispnv)
	client := fake.NewFakeClient(objects...)
	r := &infinispan.ReconcileInfinispan{Client: client, Scheme: scheme}

	res, err := r.Reconcile(req)
	assert.Nilf(t, err, "reconcile: (%v)", err)

	return res, *r
}

func findConfigMap(volumes []corev1.Volume, name string) *corev1.ConfigMapVolumeSource {
	for _, volume := range volumes {
		if volume.ConfigMap != nil && volume.Name == name {
			return volume.ConfigMap
		}
	}
	return nil
}

func findSecret(volumes []corev1.Volume, name string) *corev1.SecretVolumeSource {
	for _, volume := range volumes {
		if volume.Secret != nil && volume.Name == name {
			return volume.Secret
		}
	}
	return nil
}

func findPort(ports []corev1.ContainerPort, name string) *corev1.ContainerPort {
	for _, port := range ports {
		if port.Name == name {
			return port.DeepCopy()
		}
	}
	return nil
}

func findVolumeMount(volumeMounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
	for _, volumeMount := range volumeMounts {
		if volumeMount.Name == name {
			return volumeMount.DeepCopy()
		}
	}
	return nil
}

func findEnv(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for _, env := range envs {
		if env.Name == name {
			return env.DeepCopy()
		}
	}
	return nil
}
