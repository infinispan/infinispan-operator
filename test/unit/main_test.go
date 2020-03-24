package unit

import (
	"bytes"
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	tstutil "github.com/infinispan/infinispan-operator/test/unit/util"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	fakerest "k8s.io/client-go/rest/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
	operatorNewCpuReq        = "50m"
	operatorNewCpuLim        = "100m"
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
	Type: corev1.SecretTypeOpaque,
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
		"alias":        []byte("server-alias"),
		"password":     []byte("password-password"),
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

var basicRestMock = func(r *http.Request) (*http.Response, error) {
	notFoundResp := &http.Response{
		Status:        string(http.StatusNotFound),
		StatusCode:    http.StatusNotFound,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Body:          ioutil.NopCloser(bytes.NewBufferString("")),
		ContentLength: 0,
		Request:       r,
		Header:        make(http.Header, 0),
	}

	if r.Method == http.MethodGet && r.URL.Path == "/apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions/servicecas.operator.openshift.io" {
		return notFoundResp, nil
	}
	return nil, nil
}

func TestBasicConfiguration(t *testing.T) {
	infinispan := basicInfinispan
	r := testDefaultConfiguration(t, infinispan, basicRestMock)
	sset := tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName})

	assert.Equal(t, corev1.URIScheme("HTTP"), sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Scheme, "Liveness probe Scheme not defined as expected")
	assert.Equal(t, corev1.URIScheme("HTTP"), sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Scheme, "Readiness probe Scheme not defined as expected")

	checkIdentitiesSecret(t, r.GetClient(), *sset, operatorName+"-generated-secret")
}

func TestBasicConfigurationWithService(t *testing.T) {
	r := testDefaultConfiguration(t, basicInfinispanWithService, basicRestMock)
	checkIdentitiesSecret(t, r.GetClient(), *tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName}), operatorName+"-generated-secret")
}

// Test if spec.container.cpu update is handled
func TestContainerCpuUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.CPU = operatorNewCpuLim
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		cpuLim := ss.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"]
		cpuReq := ss.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"]
		assert.Equalf(t, 0, cpuLim.Cmp(resource.MustParse(operatorNewCpuLim)), "must be (%v) new CPU units", operatorNewCpuLim)
		assert.Equalf(t, 0, cpuReq.Cmp(resource.MustParse(operatorNewCpuReq)), "must be (%v) new CPU units", operatorNewCpuReq)
	}
	infinispan := basicInfinispan
	genericTestForContainerUpdated(t, &infinispan, basicRestMock, modifier, verifier)
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
	genericTestForContainerUpdated(t, &infinispan, basicRestMock, modifier, verifier)
}

// Test if spec.container.extrajvmopts update is handled
func TestContainerJavaOptsUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Service.Type = ispnv1.ServiceTypeDataGrid
		ispn.Spec.Container.ExtraJvmOpts = strings.Join(extraJavaOptions, " ")
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(tstutil.FindEnv(ss.Spec.Template.Spec.Containers[0].Env, "JAVA_OPTIONS").Value))
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(tstutil.FindEnv(ss.Spec.Template.Spec.Containers[0].Env, "EXTRA_JAVA_OPTIONS").Value))
	}
	infinispan := basicInfinispan
	genericTestForContainerUpdated(t, &infinispan, basicRestMock, modifier, verifier)
}

func TestEndpointEncryptionSecret(t *testing.T) {
	r := testDefaultConfiguration(t, infinispanEndpointEncryption, basicRestMock, endpointEncryptionSecret)
	sset := tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName })

	checkEndpointEncryption(t, *sset)
	checkIdentitiesSecret(t, r.GetClient(), *sset, operatorName+"-generated-secret")

	config := tstutil.GetConfigMap(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName + "-configuration"})
	configuration := tstutil.GetInfinispanConfiguration(*config)

	assert.Equal(t, "/etc/encrypt/keystore.p12", configuration.Keystore.Path)
	assert.Equal(t, "password-password", configuration.Keystore.Password)
	assert.Equal(t, "server-alias", configuration.Keystore.Alias)
}

func TestEndpointEncryptionSecretTls(t *testing.T) {
	secret := endpointEncryptionSecret
	secret.Data = endpointEncryptionSecretTlsData

	r := testDefaultConfiguration(t, infinispanEndpointEncryption, basicRestMock, secret)
	sset := tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName })

	checkEndpointEncryption(t, *sset)
	checkIdentitiesSecret(t, r.GetClient(), *sset, operatorName+"-generated-secret")

	config := tstutil.GetConfigMap(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName + "-configuration"})
	configuration := tstutil.GetInfinispanConfiguration(*config)

	checkEndpointEncryptionConfigServiceOrSecretTls(t, *configuration)
}

func TestEndpointEncryptionService(t *testing.T) {
	infinispan := infinispanEndpointEncryption
	infinispan.Spec.Security = endpointEncryptionWithService

	r := testDefaultConfiguration(t, infinispan, basicRestMock)

	config := tstutil.GetConfigMap(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName + "-configuration"})
	configuration := tstutil.GetInfinispanConfiguration(*config)

	checkEndpointEncryptionConfigServiceOrSecretTls(t, *configuration)
	checkIdentitiesSecret(t, r.GetClient(), *tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName}), operatorName+"-generated-secret")
}

func TestExposeService(t *testing.T) {
	infinispan := basicInfinispanWithService

	for _, serviceType := range serviceTypes {
		infinispan.Spec.Expose.Type = serviceType
		infinispan.Spec.Expose.Ports = append(infinispan.Spec.Expose.Ports, corev1.ServicePort{NodePort: defaultNodePort})
		r := testDefaultConfiguration(t, infinispan, basicRestMock)
		checkIdentitiesSecret(t, r.GetClient(), *tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName}), operatorName+"-generated-secret")

		externalService := tstutil.GetService(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName + "-external"})

		if serviceType == corev1.ServiceTypeLoadBalancer || serviceType == corev1.ServiceTypeNodePort {
			// Only ServiceTypeLoadBalancer and ServiceTypeNodePort are supported
			assert.NotNilf(t, externalService, "External service with type (%v) not found", serviceType)
			assert.Equalf(t, serviceType, externalService.Spec.Type, "Service type: (%v)", serviceType)
			assert.Equalf(t, defaultHotrodPort, externalService.Spec.Ports[0].Port, "Service type: (%v)", serviceType)
			assert.Equalf(t, defaultHotrodPort, externalService.Spec.Ports[0].TargetPort.IntVal, "Service type: (%v)", serviceType)
			if serviceType == corev1.ServiceTypeLoadBalancer {
				assert.Equalf(t, int32(0), externalService.Spec.Ports[0].NodePort, "Service type: (%v)", serviceType)
			} else {
				assert.Equalf(t, defaultNodePort, externalService.Spec.Ports[0].NodePort, "Service type: (%v)", serviceType)
			}
		} else {
			assert.Nil(t, externalService)
		}
	}
}

func TestEndpointSecretUpdate(t *testing.T) {
	infinispan := infinispanWithCustomSecret

	identities := ispnutil.CreateIdentitiesFor("connectorusr", "connectorpass")
	identitiesYaml, err := yaml.Marshal(identities)

	assert.NoError(t, err)

	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointCustomSecretName + "1",
			Namespace: operatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"identities.yaml": string(identitiesYaml),
		},
	}

	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      endpointCustomSecretName + "2",
			Namespace: operatorNamespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"identities.yaml": string(identitiesYaml),
		},
	}

	r := testDefaultConfiguration(t, infinispan, basicRestMock, secret1, secret2)
	checkIdentitiesSecret(t, r.GetClient(), *tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName}), secret1.Name)

	ispn := tstutil.GetInfinispan(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName})
	ispn.Spec.Security.EndpointSecretName = endpointCustomSecretName + "2"
	r.GetClient().Update(context.TODO(), ispn)

	res := reconcile.Result{true, 0}
	for res.Requeue {
		res, err = r.Reconcile(req)
		assert.NoError(t, err)
	}

	ispn = tstutil.GetInfinispan(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName})
	checkIdentitiesSecret(t, r.GetClient(), *tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName}), secret2.Name)
	assert.Nil(t, tstutil.GetSecret(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName + "-generated-secret"}))
}

func TestBasicConfigurationWithPod(t *testing.T) {
	infinispan := basicInfinispan

	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "infinispan-pod-1",
			Namespace: operatorNamespace,
			Labels:    map[string]string{"app": "infinispan-pod", "infinispan_cr": operatorName, "clusterName": operatorName},
		},

		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{corev1.PodCondition{Type: corev1.ContainersReady, Status: corev1.ConditionTrue}},
			PodIP:      "172.0.0.10",
		},
	}

	r := testDefaultConfiguration(t, infinispan, basicRestMock, pod)
	checkIdentitiesSecret(t, r.GetClient(), *tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName}), operatorName+"-generated-secret")

	res := reconcile.Result{true, 0}
	err := errors.New("")
	for res.Requeue {
		res, err = r.Reconcile(req)
		assert.NoError(t, err)
	}

	ispn := tstutil.GetInfinispan(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName})

	assert.Equal(t, "True", ispn.Status.Conditions[0].Status)
	assert.Equal(t, "wellFormed", ispn.Status.Conditions[0].Type)
}

func testDefaultConfiguration(t *testing.T, ispn ispnv1.Infinispan, restMock func(*http.Request) (*http.Response, error), objs ...runtime.Object) infinispan.ReconcileInfinispan {
	res, r := reconcileInfinispan(t, &ispn, restMock, objs)
	assert.True(t, res.Requeue, "reconcile did not requeue request as expected")

	sset := tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName})
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

	assert.NotNil(t, tstutil.FindConfigMap(sset.Spec.Template.Spec.Volumes, "config-volume"), "Config Volume must be defined")
	assert.Equal(t, operatorName+"-configuration", tstutil.FindConfigMap(sset.Spec.Template.Spec.Volumes, "config-volume").Name)

	assert.Equal(t, defaultPingPort, tstutil.FindPort(sset.Spec.Template.Spec.Containers[0].Ports, "ping").ContainerPort, "ping port not defined")
	assert.Equal(t, defaultHotrodPort, tstutil.FindPort(sset.Spec.Template.Spec.Containers[0].Ports, "hotrod").ContainerPort, "hotrod port not defined")

	assert.Equal(t, "/etc/config", tstutil.FindVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "config-volume").MountPath, "config volume not defined as expected")
	assert.Equal(t, "/etc/security", tstutil.FindVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "identities-volume").MountPath, "identities volume not defined as expected")
	assert.Equal(t, "/opt/infinispan/server/data", tstutil.FindVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, operatorName).MountPath, "data volume not defined as expected")

	pvc := tstutil.FindPersistentVolumeClaim(sset.Spec.VolumeClaimTemplates, operatorName)
	assert.NotNil(t, pvc, "persistent volume claim not defined as expected")
	assert.Equal(t, resource.MustParse(operatorStorageDefault), pvc.Spec.Resources.Requests["storage"], "persistent volume size not defined as expected")

	assert.Equal(t, "/etc/config/infinispan.yaml", tstutil.FindEnv(sset.Spec.Template.Spec.Containers[0].Env, "CONFIG_PATH").Value, "CONFIG_PATH not defined as expected")
	assert.Equal(t, "/etc/security/identities.yaml", tstutil.FindEnv(sset.Spec.Template.Spec.Containers[0].Env, "IDENTITIES_PATH").Value, "IDENTITIES_PATH not defined as expected")
	checkJavaOptionsForService(t, ispn.Spec.Service.Type, sset.Spec.Template.Spec.Containers[0].Env)

	assert.Equal(t, defaultHotrodPort, sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Port.IntVal, "Liveness probe HTTP port not definedas expected")
	assert.Equal(t, operatorProbeURI, sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Path, "Liveness probe HTTP path not defined as expected")

	assert.Equal(t, defaultHotrodPort, sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Port.IntVal, "Readiness probe HTTP port not defined as expected")
	assert.Equal(t, operatorProbeURI, sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path, "Readiness probe HTTP path not defined as expected")

	config := tstutil.GetConfigMap(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName + "-configuration"})
	assert.NotNil(t, config, "Configuration ConfigMap not found")

	configuration := tstutil.GetInfinispanConfiguration(*config)

	assert.Equal(t, operatorName, configuration.ClusterName)
	checkOwnerReference(t, config.OwnerReferences[0], "Incorrect Owner Reference for Configuration ConfigMap")

	service := tstutil.GetService(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName})
	serviceForPing := tstutil.GetService(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName + "-ping"})

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

func genericTestForContainerUpdated(t *testing.T, ispn *ispnv1.Infinispan, restMock func(*http.Request) (*http.Response, error), modifier func(*ispnv1.Infinispan), verifier func(*appsv1.StatefulSet)) {
	res, r := reconcileInfinispan(t, ispn, restMock, nil)
	assert.True(t, res.Requeue, "reconcile did not requeue request as expected")

	modifier(ispn)
	r.GetClient().Update(context.TODO(), ispn)

	resp, err := r.Reconcile(req)
	assert.True(t, resp.Requeue, "reconcile did not requeue request as expected")
	assert.NoError(t, err)

	sset := tstutil.GetStatefulSet(r.GetClient(), types.NamespacedName{operatorNamespace, operatorName})
	assert.NotNil(t, sset, "unable to get StatefulSet")

	verifier(sset)
}

func checkOwnerReference(t *testing.T, ref metav1.OwnerReference, msgAndArgs ...interface{}) {
	assert.Equal(t, operatorOwnerReference, metav1.OwnerReference{APIVersion: ref.APIVersion, Kind: ref.Kind, Name: ref.Name}, msgAndArgs)
}

func checkEndpointEncryption(t *testing.T, sset appsv1.StatefulSet) {
	assert.Equal(t, corev1.URIScheme("HTTPS"), sset.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Scheme, "Liveness probe Scheme not defined as expected")
	assert.Equal(t, corev1.URIScheme("HTTPS"), sset.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Scheme, "Readiness probe Scheme not defined as expected")

	assert.NotNil(t, tstutil.FindSecret(sset.Spec.Template.Spec.Volumes, "encrypt-volume"), "Encrypt Volume must be defined")
	assert.Equal(t, operatorTlsSecretName, tstutil.FindSecret(sset.Spec.Template.Spec.Volumes, "encrypt-volume").SecretName)
	assert.Equal(t, "/etc/encrypt", tstutil.FindVolumeMount(sset.Spec.Template.Spec.Containers[0].VolumeMounts, "encrypt-volume").MountPath, "encrypt volume not defined as expected")
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
		assert.ElementsMatch(t, defaultJavaOptions, strings.Fields(tstutil.FindEnv(envs, "JAVA_OPTIONS").Value))
		assert.Empty(t, tstutil.FindEnv(envs, "EXTRA_JAVA_OPTIONS").Value)
	} else if serviceType == ispnv1.ServiceTypeDataGrid {
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(tstutil.FindEnv(envs, "JAVA_OPTIONS").Value))
		assert.ElementsMatch(t, extraJavaOptions, strings.Fields(tstutil.FindEnv(envs, "EXTRA_JAVA_OPTIONS").Value))
	}
}

func checkIdentitiesSecret(t *testing.T, client client.Client, sset appsv1.StatefulSet, name string) {
	assert.NotNil(t, tstutil.FindSecret(sset.Spec.Template.Spec.Volumes, "identities-volume"), "Identities Volume must be defined")
	assert.Equal(t, name, tstutil.FindSecret(sset.Spec.Template.Spec.Volumes, "identities-volume").SecretName)

	identitiesSecret := tstutil.GetSecret(client, types.NamespacedName{operatorNamespace, name})
	identities := ispnutil.Identities{}

	assert.NotNil(t, identitiesSecret, "Identities Secret not fount")
	secretData := identitiesSecret.StringData["identities.yaml"]
	assert.NotNil(t, secretData)

	yaml.Unmarshal([]byte(secretData), &identities)
	assert.Equal(t, 2, len(identities.Credentials))

	infinispan := tstutil.GetInfinispan(client, types.NamespacedName{operatorNamespace, operatorName})
	assert.Equal(t, name, infinispan.Spec.Security.EndpointSecretName)
	assert.Equal(t, name, infinispan.Status.Security.EndpointSecretName)
}

func reconcileInfinispan(t *testing.T, ispnv *ispnv1.Infinispan, restMock func(*http.Request) (*http.Response, error), objs []runtime.Object) (reconcile.Result, infinispan.ReconcileInfinispan) {
	objects := []runtime.Object{ispnv}

	scheme := kscheme.Scheme
	restReq := &http.Request{}
	restResp := &http.Response{}

	scheme.AddKnownTypes(ispnv1.SchemeGroupVersion, ispnv)
	client := fake.NewFakeClient(objects...)
	httpClient := fakerest.CreateHTTPClient(restMock)

	fakeRest := &fakerest.RESTClient{
		NegotiatedSerializer: kscheme.Codecs.WithoutConversion(),
		GroupVersion:         corev1.SchemeGroupVersion,
		VersionedAPIPath:     "/api",
		Req:                  restReq,
		Resp:                 restResp,
		Client:               httpClient,
	}

	fakeKubernetes := &ispnutil.Kubernetes{Client: client, RestClient: fakeRest}
	fakeCluster := FakeCluster{fakeKubernetes}
	r := infinispan.NewFakeReconciler(client, scheme, fakeKubernetes, fakeCluster)

	// create dependents Kubernetes runtime objects
	for _, obj := range objs {
		err := r.GetClient().Create(context.TODO(), obj)
		assert.NoErrorf(t, err, "Create (%v) object error: (%v)", obj.GetObjectKind(), err)
	}

	res, err := r.Reconcile(req)
	tstutil.ProcessSecretsData(r.GetClient(), operatorNamespace)
	assert.Nilf(t, err, "reconcile: (%v)", err)

	return res, r
}
