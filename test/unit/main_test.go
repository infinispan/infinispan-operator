package unit

import (
	"context"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"testing"
)

const (
	operatorName             = "default-infinispan"
	operatorAPIVersion       = "infinispan.org/v1"
	operatorKind             = "Infinispan"
	operatorAppName          = "infinispan-pod"
	operatorPodName          = "infinispan"
	operatorImageName        = "infinispan/server:latest"
	operatorNamespace        = "myproject"
	operatorCpuDefault       = "500m"
	operatorCpuHalfDefault   = "250m"
	operatorMemoryDefault    = "512Mi"
	operatorStorageDefault   = "1Gi"
	operatorNewCpu           = "100m"
	operatorNewMemory        = "1Gi"
	operatorProbeURI         = "rest/v2/cache-managers/default/health/status"
	operatorTlsSecretName    = "tls-secret"
	operatorTlsServiceName   = "service.beta.openshift.io"
	endpointCustomSecretName = "connect-secret"

	operatorReplicas  int32 = 1
	defaultPingPort   int32 = 8888
	defaultHotrodPort int32 = 11222
	defaultNodePort   int32 = 30222
)

var defaultJavaOptions = []string{"-Xmx200M", "-Xms200M", "-XX:MaxRAM=420M", "-Dsun.zip.disableMemoryMapping=true", "-XX:+UseSerialGC", "-XX:MinHeapFreeRatio=5", "-XX:MaxHeapFreeRatio=10"}
var extraJavaOptions = []string{"-XX:NativeMemoryTracking=summary"}
var serviceTypes = []corev1.ServiceType{corev1.ServiceTypeClusterIP, corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer, corev1.ServiceTypeExternalName}

var operatorOwnerReference = metav1.OwnerReference{
	APIVersion: operatorAPIVersion,
	Kind:       operatorKind,
	Name:       operatorName,
}

var basicInfinispan = ispnv1.Infinispan{
	TypeMeta: metav1.TypeMeta{
		APIVersion: operatorAPIVersion,
		Kind:       operatorKind,
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
		APIVersion: operatorAPIVersion,
		Kind:       operatorKind,
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
			ExtraJvmOpts: strings.Join(extraJavaOptions, " "),
		},
		Replicas: operatorReplicas,
	},
}

var infinispanEndpointEncryption = ispnv1.Infinispan{
	TypeMeta: metav1.TypeMeta{
		APIVersion: operatorAPIVersion,
		Kind:       operatorKind,
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      operatorName,
		Namespace: operatorNamespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Replicas: operatorReplicas,
		Security: ispnv1.InfinispanSecurity{
			EndpointEncryption: ispnv1.EndpointEncryption{
				Type:           "secret",
				CertSecretName: operatorTlsSecretName,
			},
		},
	},
}

var infinispanWithCustomSecret = ispnv1.Infinispan{
	TypeMeta: metav1.TypeMeta{
		APIVersion: operatorAPIVersion,
		Kind:       operatorKind,
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      operatorName,
		Namespace: operatorNamespace,
	},
	Spec: ispnv1.InfinispanSpec{
		Replicas: operatorReplicas,
		Security: ispnv1.InfinispanSecurity{
			EndpointSecretName: endpointCustomSecretName + "1",
		},
	},
}

var endpointEncryptionWithService = ispnv1.InfinispanSecurity{
	EndpointEncryption: ispnv1.EndpointEncryption{
		Type:            "service",
		CertServiceName: operatorTlsServiceName,
	},
}

var endpointEncryptionSecret = &corev1.Secret{
	Type: corev1.SecretType(corev1.SecretTypeOpaque),
	ObjectMeta: metav1.ObjectMeta{
		Name:      operatorTlsSecretName,
		Namespace: operatorNamespace,
	},
	StringData: map[string]string{
		"alias":    "server-alias",
		"password": "password-password",
	},
	Data: map[string][]byte{
		"keystore.p12": []byte("KEYSTORE.P12"),
	},
}

var endpointEncryptionSecretTlsData = map[string][]byte{
	"tls.key": []byte("TLS.KEY"),
	"tls.crt": []byte("TLS.CRT"),
}

var req = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Name:      operatorName,
		Namespace: operatorNamespace,
	},
}

func TestBasicConfiguration(t *testing.T) {
	infinispan := basicInfinispan
	r := testDefaultConfiguration(t, infinispan)
	sset := getStatefulSet(r.Client, operatorName)

	assert.Equal(t, corev1.URIScheme("HTTP"), sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Scheme, "Liveness probe Scheme not defined as expected")
	assert.Equal(t, corev1.URIScheme("HTTP"), sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Scheme, "Readiness probe Scheme not defined as expected")

	checkIdentitiesSecret(t, r.Client, *sset, operatorName+"-generated-secret")
}

func TestBasicConfigurationWithService(t *testing.T) {
	r := testDefaultConfiguration(t, basicInfinispanWithService)
	checkIdentitiesSecret(t, r.Client, *getStatefulSet(r.Client, operatorName), operatorName+"-generated-secret")
}

// Test if spec.container.cpu update is handled
func TestContainerCpuUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.CPU = operatorNewCpu
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		assert.Equalf(t, resource.MustParse(operatorNewCpu), ss.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"], "must be (%v) new CPU units", operatorNewCpu)
	}
	infinispan := basicInfinispan
	genericTestForContainerUpdated(t, &infinispan, modifier, verifier)
}

// Test if spec.container.memory update is handled
func TestContainerMemoryUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.Memory = operatorNewMemory
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		assert.Equalf(t, resource.MustParse(operatorNewMemory), ss.Spec.Template.Spec.Containers[0].Resources.Requests["memory"], "must be (%v) new memory size", operatorNewMemory)
	}
	infinispan := basicInfinispan
	genericTestForContainerUpdated(t, &infinispan, modifier, verifier)
}

// Test if spec.container.extrajvmopts update is handled
func TestContainerJavaOptsUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Service.Type = ispnv1.ServiceTypeDataGrid
		ispn.Spec.Container.ExtraJvmOpts = strings.Join(extraJavaOptions, " ")
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(findEnv(ss.Spec.Template.Spec.Containers[0].Env, "JAVA_OPTIONS").Value))
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(findEnv(ss.Spec.Template.Spec.Containers[0].Env, "EXTRA_JAVA_OPTIONS").Value))
	}
	infinispan := basicInfinispan
	genericTestForContainerUpdated(t, &infinispan, modifier, verifier)
}

func TestEndpointEncryptionSecret(t *testing.T) {
	r := testDefaultConfiguration(t, infinispanEndpointEncryption, endpointEncryptionSecret)
	sset := getStatefulSet(r.Client, operatorName)

	checkEndpointEncryption(t, *sset)
	checkIdentitiesSecret(t, r.Client, *sset, operatorName+"-generated-secret")

	config := getConfigMap(r.Client, operatorName+"-configuration")
	configuration := getInfinispanConfiguration(*config)

	assert.Equal(t, "/etc/encrypt/keystore.p12", configuration.Keystore.Path)
	assert.Equal(t, "password-password", configuration.Keystore.Password)
	assert.Equal(t, "server-alias", configuration.Keystore.Alias)
}

func TestEndpointEncryptionSecretTls(t *testing.T) {
	secret := endpointEncryptionSecret
	secret.Data = endpointEncryptionSecretTlsData

	r := testDefaultConfiguration(t, infinispanEndpointEncryption, secret)
	sset := getStatefulSet(r.Client, operatorName)

	checkEndpointEncryption(t, *sset)
	checkIdentitiesSecret(t, r.Client, *sset, operatorName+"-generated-secret")

	config := getConfigMap(r.Client, operatorName+"-configuration")
	configuration := getInfinispanConfiguration(*config)

	checkEndpointEncryptionConfigServiceOrSecretTls(t, *configuration)
}

func TestEndpointEncryptionService(t *testing.T) {
	infinispan := infinispanEndpointEncryption
	infinispan.Spec.Security = endpointEncryptionWithService

	r := testDefaultConfiguration(t, infinispan)

	config := getConfigMap(r.Client, operatorName+"-configuration")
	configuration := getInfinispanConfiguration(*config)

	checkEndpointEncryptionConfigServiceOrSecretTls(t, *configuration)
	checkIdentitiesSecret(t, r.Client, *getStatefulSet(r.Client, operatorName), operatorName+"-generated-secret")
}

func TestExposeService(t *testing.T) {
	infinispan := basicInfinispanWithService

	for _, serviceType := range serviceTypes {
		infinispan.Spec.Expose.Type = serviceType
		r := testDefaultConfiguration(t, infinispan)
		checkIdentitiesSecret(t, r.Client, *getStatefulSet(r.Client, operatorName), operatorName+"-generated-secret")

		externalService := getService(r.Client, operatorName+"-external")

		assert.NotNilf(t, externalService, "External service with type (%v) not found", serviceType)
		assert.Equalf(t, serviceType, externalService.Spec.Type, "Service type: (%v)", serviceType)
		assert.Equalf(t, defaultHotrodPort, externalService.Spec.Ports[0].Port, "Service type: (%v)", serviceType)
		assert.Equalf(t, defaultHotrodPort, externalService.Spec.Ports[0].TargetPort.IntVal, "Service type: (%v)", serviceType)
		assert.Equalf(t, defaultNodePort, externalService.Spec.Ports[0].NodePort, "Service type: (%v)", serviceType)
	}
}

func TestEndpointSecretUpdate(t *testing.T) {
	infinispan := infinispanWithCustomSecret

	identities := ispnutil.CreateIdentitiesFor("connectorusr", "connectorpass")
	identitiesYaml, err := yaml.Marshal(identities)

	assert.NoError(t, err)

	secret1 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointCustomSecretName + "1",
			Namespace: operatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"identities.yaml": string(identitiesYaml),
		},
	}

	secret2 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointCustomSecretName + "2",
			Namespace: operatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"identities.yaml": string(identitiesYaml),
		},
	}

	r := testDefaultConfiguration(t, infinispan, &secret1, &secret2)
	checkIdentitiesSecret(t, r.Client, *getStatefulSet(r.Client, operatorName), secret1.Name)

	ispn := getInfinispan(r.Client, operatorName)
	ispn.Spec.Security.EndpointSecretName = endpointCustomSecretName + "2"
	r.Client.Update(context.TODO(), ispn)

	res := reconcile.Result{true, 0}
	for res.Requeue {
		res, err = r.Reconcile(req)
		assert.NoError(t, err)
	}

	ispn = getInfinispan(r.Client, operatorName)
	checkIdentitiesSecret(t, r.Client, *getStatefulSet(r.Client, operatorName), secret2.Name)
	assert.Nil(t, getSecret(r.Client, operatorName+"-generated-secret"))
}

func testDefaultConfiguration(t *testing.T, ispn ispnv1.Infinispan, objs ...runtime.Object) infinispan.ReconcileInfinispan {
	res, r := reconcileInfinispan(t, &ispn, objs)
	assert.True(t, res.Requeue, "reconcile did not requeue request as expected")

	sset := getStatefulSet(r.Client, operatorName)
	assert.NotNil(t, sset, "unable to get StatefulSet")

	assert.Equalf(t, ispn.Spec.Replicas, int32(len(sset.Spec.Template.Spec.Containers)), "must be (%b) containers count", operatorReplicas)
	assert.Equal(t, operatorPodName, sset.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, operatorImageName, sset.Spec.Template.Spec.Containers[0].Image)
	assert.Equalf(t, resource.MustParse(operatorCpuDefault), sset.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"], "must be (%v) CPU units", operatorCpuDefault)
	assert.Equalf(t, resource.MustParse(operatorMemoryDefault), sset.Spec.Template.Spec.Containers[0].Resources.Limits["memory"], "must be (%v) memory size", operatorMemoryDefault)
	assert.Equalf(t, resource.MustParse(operatorMemoryDefault), sset.Spec.Template.Spec.Containers[0].Resources.Requests["memory"], "must be (%v) memory size", operatorMemoryDefault)
	assert.Equalf(t, resource.MustParse(operatorCpuHalfDefault), sset.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"], "must be (%v) memory size", operatorCpuHalfDefault)

	assert.Equal(t, operatorAppName, sset.Spec.Selector.MatchLabels["app"])
	assert.Equal(t, operatorName, sset.Spec.Selector.MatchLabels["clusterName"])
	assert.Equal(t, operatorName, sset.Spec.Selector.MatchLabels["infinispan_cr"])

	assert.NotNil(t, findConfigMap(sset.Spec.Template.Spec.Volumes, "config-volume"), "Config Volume must be defined")
	assert.Equal(t, operatorName+"-configuration", findConfigMap(sset.Spec.Template.Spec.Volumes, "config-volume").Name)

	assert.Equal(t, defaultPingPort, findPort(sset.Spec.Template.Spec.Containers[0].Ports, "ping").ContainerPort, "ping port not defined")
	assert.Equal(t, defaultHotrodPort, findPort(sset.Spec.Template.Spec.Containers[0].Ports, "hotrod").ContainerPort, "hotrod port not defined")

	assert.Equal(t, "/etc/config", findVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "config-volume").MountPath, "config volume not defined as expected")
	assert.Equal(t, "/etc/security", findVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "identities-volume").MountPath, "identities volume not defined as expected")
	assert.Equal(t, "/opt/infinispan/server/data", findVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, operatorName).MountPath, "data volume not defined as expected")

	pvc := findPersistentVolumeClaim(sset.Spec.VolumeClaimTemplates, operatorName)
	assert.NotNil(t, pvc, "persistent volume claim not defined as expected")
	assert.Equal(t, resource.MustParse(operatorStorageDefault), pvc.Spec.Resources.Requests["storage"], "persistent volume size not defined as expected")

	assert.Equal(t, "/etc/config/infinispan.yaml", findEnv(sset.Spec.Template.Spec.Containers[0].Env, "CONFIG_PATH").Value, "CONFIG_PATH not defined as expected")
	assert.Equal(t, "/etc/security/identities.yaml", findEnv(sset.Spec.Template.Spec.Containers[0].Env, "IDENTITIES_PATH").Value, "IDENTITIES_PATH not defined as expected")
	checkJavaOptionsForService(t, ispn.Spec.Service.Type, sset.Spec.Template.Spec.Containers[0].Env)

	assert.Equal(t, defaultHotrodPort, sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Port.IntVal, "Liveness probe HTTP port not definedas expected")
	assert.Equal(t, operatorProbeURI, sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Path, "Liveness probe HTTP path not defined as expected")

	assert.Equal(t, defaultHotrodPort, sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Port.IntVal, "Readiness probe HTTP port not defined as expected")
	assert.Equal(t, operatorProbeURI, sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path, "Readiness probe HTTP path not defined as expected")

	config := getConfigMap(r.Client, operatorName+"-configuration")
	assert.NotNil(t, config, "Configuration ConfigMap not found")

	configuration := getInfinispanConfiguration(*config)

	assert.Equal(t, operatorName, configuration.ClusterName)
	checkOwnerReference(t, config.OwnerReferences[0], "Incorrect Owner Reference for Configuration ConfigMap")

	service := getService(r.Client, operatorName)
	serviceForPing := getService(r.Client, operatorName+"-ping")

	assert.NotNil(t, service)
	assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	assert.Equal(t, 1, len(service.Spec.Ports))
	assert.Equal(t, defaultHotrodPort, service.Spec.Ports[0].Port)
	checkOwnerReference(t, service.OwnerReferences[0], "Incorrect Owner Reference for Service")

	assert.NotNil(t, serviceForPing)
	assert.Equal(t, corev1.ServiceTypeClusterIP, serviceForPing.Spec.Type)
	assert.Equal(t, "None", serviceForPing.Spec.ClusterIP)
	assert.Equal(t, 1, len(serviceForPing.Spec.Ports))
	assert.Equal(t, defaultPingPort, serviceForPing.Spec.Ports[0].Port)
	assert.Equal(t, "ping", serviceForPing.Spec.Ports[0].Name)
	checkOwnerReference(t, serviceForPing.OwnerReferences[0], "Incorrect Owner Reference for Ping Service")

	return r
}

func genericTestForContainerUpdated(t *testing.T, ispn *ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*appsv1.StatefulSet)) {
	res, r := reconcileInfinispan(t, ispn, nil)
	assert.True(t, res.Requeue, "reconcile did not requeue request as expected")

	modifier(ispn)
	r.Client.Update(context.TODO(), ispn)

	resp, err := r.Reconcile(req)
	assert.True(t, resp.Requeue, "reconcile did not requeue request as expected")
	assert.NoError(t, err)

	sset := getStatefulSet(r.Client, operatorName)
	assert.NotNil(t, sset, "unable to get StatefulSet")

	verifier(sset)
}

func checkOwnerReference(t *testing.T, ref metav1.OwnerReference, msgAndArgs ...interface{}) {
	assert.Equal(t, operatorOwnerReference, metav1.OwnerReference{APIVersion: ref.APIVersion, Kind: ref.Kind, Name: ref.Name}, msgAndArgs)
}

func checkEndpointEncryption(t *testing.T, sset appsv1.StatefulSet) {
	assert.Equal(t, corev1.URIScheme("HTTPS"), sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Scheme, "Liveness probe Scheme not defined as expected")
	assert.Equal(t, corev1.URIScheme("HTTPS"), sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Scheme, "Readiness probe Scheme not defined as expected")

	assert.NotNil(t, findSecret(sset.Spec.Template.Spec.Volumes, "encrypt-volume"), "Encrypt Volume must be defined")
	assert.Equal(t, operatorTlsSecretName, findSecret(sset.Spec.Template.Spec.Volumes, "encrypt-volume").SecretName)
	assert.Equal(t, "/etc/encrypt", findVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "encrypt-volume").MountPath, "encrypt volume not defined as expected")
}

func checkEndpointEncryptionConfigServiceOrSecretTls(t *testing.T, config ispnutil.InfinispanConfiguration) {
	assert.Equal(t, "/etc/encrypt", config.Keystore.CrtPath)
	assert.Equal(t, "/opt/infinispan/server/conf/keystore", config.Keystore.Path)
	assert.Equal(t, "/opt/infinispan/server/conf/keystore", config.Keystore.Path)
	assert.Equal(t, "password", config.Keystore.Password)
	assert.Equal(t, "server", config.Keystore.Alias)
}

func checkJavaOptionsForService(t *testing.T, serviceType ispnv1.ServiceType, envs []corev1.EnvVar) {
	if serviceType == ispnv1.ServiceTypeCache {
		assert.ElementsMatch(t, defaultJavaOptions, strings.Fields(findEnv(envs, "JAVA_OPTIONS").Value))
		assert.Empty(t, findEnv(envs, "EXTRA_JAVA_OPTIONS").Value)
	} else if serviceType == ispnv1.ServiceTypeDataGrid {
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(findEnv(envs, "JAVA_OPTIONS").Value))
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(findEnv(envs, "EXTRA_JAVA_OPTIONS").Value))
	}
}

func checkIdentitiesSecret(t *testing.T, client client.Client, sset appsv1.StatefulSet, name string) {
	assert.NotNil(t, findSecret(sset.Spec.Template.Spec.Volumes, "identities-volume"), "Identities Volume must be defined")
	assert.Equal(t, name, findSecret(sset.Spec.Template.Spec.Volumes, "identities-volume").SecretName)

	identitiesSecret := getSecret(client, name)
	identities := ispnutil.Identities{}

	assert.NotNil(t, identitiesSecret, "Identities Secret not fount")
	secretData := identitiesSecret.StringData["identities.yaml"]
	assert.NotNil(t, secretData)

	yaml.Unmarshal([]byte(secretData), &identities)
	assert.Equal(t, 2, len(identities.Credentials))

	infinispan := getInfinispan(client, operatorName)
	assert.Equal(t, name, infinispan.Spec.Security.EndpointSecretName)
	assert.Equal(t, name, infinispan.Status.Security.EndpointSecretName)
}

func reconcileInfinispan(t *testing.T, ispnv *ispnv1.Infinispan, objs []runtime.Object) (reconcile.Result, infinispan.ReconcileInfinispan) {
	objects := []runtime.Object{ispnv}

	scheme := scheme.Scheme
	scheme.AddKnownTypes(ispnv1.SchemeGroupVersion, ispnv)
	client := fake.NewFakeClient(objects...)

	r := infinispan.NewFakeReconciler(client, scheme)

	// create dependents Kubernetes runtime objects
	for _, obj := range objs {
		err := r.Client.Create(context.TODO(), obj)
		assert.NoErrorf(t, err, "Create (%v) object error: (%v)", obj.GetObjectKind(), err)
	}

	res, err := r.Reconcile(req)
	assert.Nilf(t, err, "reconcile: (%v)", err)

	return res, r
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

func findPersistentVolumeClaim(volumeMountClaims []corev1.PersistentVolumeClaim, name string) *corev1.PersistentVolumeClaim {
	for _, volumeMountClaim := range volumeMountClaims {
		if volumeMountClaim.Name == name {
			return volumeMountClaim.DeepCopy()
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

func getStatefulSet(client client.Client, name string) *appsv1.StatefulSet {
	ns := types.NamespacedName{
		Name:      name,
		Namespace: operatorNamespace,
	}
	sset := &appsv1.StatefulSet{}
	err := client.Get(context.TODO(), ns, sset)
	if err == nil {
		return sset
	}
	return nil
}

func getService(client client.Client, name string) *corev1.Service {
	ns := types.NamespacedName{
		Name:      name,
		Namespace: operatorNamespace,
	}
	service := &corev1.Service{}
	err := client.Get(context.TODO(), ns, service)
	if err == nil {
		return service
	}
	return nil
}

func getConfigMap(client client.Client, name string) *corev1.ConfigMap {
	ns := types.NamespacedName{
		Name:      name,
		Namespace: operatorNamespace,
	}
	configMap := &corev1.ConfigMap{}
	err := client.Get(context.TODO(), ns, configMap)
	if err == nil {
		return configMap
	}
	return nil
}

func getSecret(client client.Client, name string) *corev1.Secret {
	ns := types.NamespacedName{
		Name:      name,
		Namespace: operatorNamespace,
	}
	secret := &corev1.Secret{}
	err := client.Get(context.TODO(), ns, secret)
	if err == nil {
		return secret
	}
	return nil
}

func getInfinispan(client client.Client, name string) *ispnv1.Infinispan {
	ns := types.NamespacedName{
		Name:      name,
		Namespace: operatorNamespace,
	}
	ispn := &ispnv1.Infinispan{}
	err := client.Get(context.TODO(), ns, ispn)
	if err == nil {
		return ispn
	}
	return nil
}

func getInfinispanConfiguration(configMap corev1.ConfigMap) *ispnutil.InfinispanConfiguration {
	configuration := ispnutil.InfinispanConfiguration{}
	configData := configMap.Data["infinispan.yaml"]
	yaml.Unmarshal([]byte(configData), &configuration)

	return &configuration
}
