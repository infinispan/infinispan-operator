package utils

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const (
	keystoreSecretSuffix   = "keystore"
	truststoreSecretSuffix = "truststore"
)

func EndpointEncryption(name string) *ispnv1.EndpointEncryption {
	return &ispnv1.EndpointEncryption{
		Type:           v1.CertificateSourceTypeSecret,
		CertSecretName: fmt.Sprintf("%s-%s", name, keystoreSecretSuffix),
	}
}

func EndpointEncryptionClientCert(name string, clientCert v1.ClientCertType) *v1.EndpointEncryption {
	return &v1.EndpointEncryption{
		Type:                 v1.CertificateSourceTypeSecret,
		CertSecretName:       fmt.Sprintf("%s-%s", name, keystoreSecretSuffix),
		ClientCert:           clientCert,
		ClientCertSecretName: fmt.Sprintf("%s-%s", name, truststoreSecretSuffix),
	}
}

func EncryptionSecret(name, namespace string, privKey, cert []byte) *corev1.Secret {
	s := keystoreSecret(name, namespace)
	s.Data = map[string][]byte{
		corev1.TLSCertKey:       cert,
		corev1.TLSPrivateKeyKey: privKey,
	}
	return s
}

func EncryptionSecretKeystore(name, namespace string, keystore []byte) *corev1.Secret {
	s := keystoreSecret(name, namespace)
	s.StringData["alias"] = "server"
	s.StringData["password"] = KeystorePassword
	s.Data["keystore.p12"] = keystore
	return s
}

func EncryptionSecretClientCert(name, namespace string, caCert, clientCert []byte) *corev1.Secret {
	s := truststoreSecret(name, namespace)
	s.Data["trust.ca"] = caCert
	if clientCert != nil {
		s.Data["trust.cert.client"] = clientCert
	}
	return s
}

func EncryptionSecretClientTrustore(name, namespace string, truststore []byte) *corev1.Secret {
	s := truststoreSecret(name, namespace)
	s.StringData["truststore-password"] = TruststorePassword
	s.Data["truststore.p12"] = truststore
	return s
}

func keystoreSecret(name, namespace string) *corev1.Secret {
	secretName := fmt.Sprintf("%s-%s", name, keystoreSecretSuffix)
	return encryptionSecret(secretName, namespace)
}

func truststoreSecret(name, namespace string) *corev1.Secret {
	secretName := fmt.Sprintf("%s-%s", name, truststoreSecretSuffix)
	return encryptionSecret(secretName, namespace)
}

func encryptionSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{},
		StringData: map[string]string{},
	}
}

var MinimalSpec = ispnv1.Infinispan{
	TypeMeta: InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Replicas: 2,
	},
}

func DefaultSpec(testKube *TestKubernetes) *ispnv1.Infinispan {
	return &ispnv1.Infinispan{
		TypeMeta: InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultClusterName,
			Namespace: Namespace,
		},
		Spec: ispnv1.InfinispanSpec{
			Service: ispnv1.InfinispanServiceSpec{
				Type: ispnv1.ServiceTypeDataGrid,
			},
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    CPU,
				Memory: Memory,
			},
			Replicas: 1,
			Expose:   ExposeServiceSpec(testKube),
		},
	}
}

func WebServerPod(name, namespace, configName, mountPath, imageName string) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "web-server",
				Image: imageName,
				Ports: []corev1.ContainerPort{{
					ContainerPort: int32(WebServerPortNumber),
					Name:          "web-port",
					Protocol:      corev1.ProtocolTCP,
				}},
				VolumeMounts: []corev1.VolumeMount{{
					MountPath: mountPath,
					Name:      "data",
				}},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Scheme: corev1.URISchemeHTTP,
							Path:   "/index.html",
							Port:   intstr.FromInt(WebServerPortNumber),
						},
					},
					InitialDelaySeconds: 5,
					TimeoutSeconds:      60,
					PeriodSeconds:       1,
					SuccessThreshold:    1,
					FailureThreshold:    5,
				},
			}},
			Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: configName,
						},
					},
				},
			}},
		},
	}
}

func WebServerService(name, namespace string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{{
				Protocol: corev1.ProtocolTCP,
				Port:     int32(WebServerPortNumber),
			}},
		},
	}
}

func ExposeServiceSpec(testKube *TestKubernetes) *ispnv1.ExposeSpec {
	return &ispnv1.ExposeSpec{
		Type: exposeServiceType(testKube),
	}
}

func exposeServiceType(testKube *TestKubernetes) ispnv1.ExposeType {
	switch ispnv1.ExposeType(ExposeServiceType) {
	case ispnv1.ExposeTypeNodePort, ispnv1.ExposeTypeLoadBalancer:
		return ispnv1.ExposeType(ExposeServiceType)
	case ispnv1.ExposeTypeRoute:
		okRoute, err := testKube.Kubernetes.IsGroupVersionSupported(routev1.GroupVersion.String(), "Route")
		ExpectNoError(err)
		if okRoute {
			return ispnv1.ExposeTypeRoute
		}
		panic(fmt.Errorf("expose type Route is not supported on the platform"))
	default:
		panic(fmt.Errorf("unknown service type %s", ExposeServiceType))
	}
}

func GetYamlReaderFromFile(filename string) (*yaml.YAMLReader, error) {
	absFileName := getAbsolutePath(filename)
	f, err := os.Open(absFileName)
	if err != nil {
		return nil, err
	}
	return yaml.NewYAMLReader(bufio.NewReader(f)), nil
}

// Obtain the file absolute path given a relative path
func getAbsolutePath(relativeFilePath string) string {
	if !strings.HasPrefix(relativeFilePath, ".") {
		return relativeFilePath
	}
	dir, _ := os.Getwd()
	absPath, _ := filepath.Abs(dir + "/" + relativeFilePath)
	return absPath
}

func clientForCluster(i *ispnv1.Infinispan, kube *TestKubernetes) HTTPClient {
	protocol := kube.GetSchemaForRest(i)

	if !i.IsAuthenticationEnabled() {
		return NewHTTPClientNoAuth(protocol)
	}

	user := constants.DefaultDeveloperUser
	pass, err := users.UserPassword(user, i.GetSecretName(), i.Namespace, kube.Kubernetes)
	ExpectNoError(err)
	return NewHTTPClient(user, pass, protocol)
}

func HTTPClientAndHost(i *ispnv1.Infinispan, kube *TestKubernetes) (string, HTTPClient) {
	client := clientForCluster(i, kube)
	hostAddr := kube.WaitForExternalService(i, RouteTimeout, client)
	return hostAddr, client
}

func HTTPSClientAndHost(i *v1.Infinispan, tlsConfig *tls.Config, kube *TestKubernetes) (string, HTTPClient) {
	var client HTTPClient
	clientCert := i.Spec.Security.EndpointEncryption.ClientCert
	if clientCert != "" && clientCert != ispnv1.ClientCertNone {
		client = NewHTTPSClientCert(tlsConfig)
	} else {
		if i.IsAuthenticationEnabled() {
			user := constants.DefaultDeveloperUser
			pass, err := users.UserPassword(user, i.GetSecretName(), i.Namespace, kube.Kubernetes)
			ExpectNoError(err)
			client = NewHTTPSClient(user, pass, tlsConfig)
		} else {
			client = NewHTTPSClientNoAuth(tlsConfig)
		}
	}

	hostAddr := kube.WaitForExternalService(i, RouteTimeout, client)
	return hostAddr, client
}
