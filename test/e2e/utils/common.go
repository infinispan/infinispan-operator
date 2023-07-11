package utils

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/pointer"
)

const (
	keystoreSecretSuffix   = "keystore"
	truststoreSecretSuffix = "truststore"
)

var VersionManager = func() *version.Manager {
	if os.Getenv(ispnv1.OperatorOperandVersionEnvVarName) == "" {
		// Operand versions must be updated whenever the supported Operands change as the Operands are executed
		// during the integration tests.
		// The final release must be a cve release in order for TestOperandCVEUpgrade to pass. This does not have to be
		// a real release, but the image tag must differ from the oldest Operand release so that we can ensure the
		// Pod Image was correctly updated.
		_ = os.Setenv(ispnv1.OperatorOperandVersionEnvVarName, `[{
			"downstream-version": "0.1.0",
			"upstream-version": "13.0.10",
			"image": "quay.io/infinispan/server:13.0.10.Final"
		},{
			"downstream-version": "0.2.0-1",
			"upstream-version": "14.0.1",
			"image": "quay.io/infinispan/server:14.0.1.Final"
		},{
			"downstream-version": "0.2.1-1",
			"upstream-version": "14.0.6",
			"image": "quay.io/infinispan/server:14.0.6.Final"
		},{
			"downstream-version": "0.2.2-1",
			"upstream-version": "14.0.9",
			"image": "quay.io/infinispan/server:14.0.9.Final",
			"cve": true
		},{
			"downstream-version": "0.2.3-1",
			"upstream-version": "14.0.12",
			"image": "quay.io/infinispan/server:14.0.12.Final",
			"cve": true
		},{
			"downstream-version": "0.2.3-2",
			"upstream-version": "14.0.12",
			"image": "quay.io/infinispan/server:14.0",
			"cve": true
		}]`)
	}
	if manager, err := version.ManagerFromEnv(ispnv1.OperatorOperandVersionEnvVarName); err != nil {
		panic(err)
	} else {
		return manager
	}
}

var LatestOperand = VersionManager().Latest()

func EndpointEncryption(name string) *ispnv1.EndpointEncryption {
	return &ispnv1.EndpointEncryption{
		Type:           ispnv1.CertificateSourceTypeSecret,
		CertSecretName: fmt.Sprintf("%s-%s", name, keystoreSecretSuffix),
	}
}

func EndpointEncryptionClientCert(name string, clientCert ispnv1.ClientCertType) *ispnv1.EndpointEncryption {
	return &ispnv1.EndpointEncryption{
		Type:                 ispnv1.CertificateSourceTypeSecret,
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

func TestName(t *testing.T) string {
	return regexp.MustCompile(".*/").ReplaceAllString(t.Name(), "")
}

// DefaultSpec creates a default Infinispan spec for tests. Consumers should utilise the initializer function to modify
// the spec before the resource is created. This allows for the webhook defaulting behaviour to be applied without
// deploying the webhooks
func DefaultSpec(t *testing.T, testKube *TestKubernetes, initializer func(*ispnv1.Infinispan)) *ispnv1.Infinispan {
	testName := TestName(t)
	infinispan := &ispnv1.Infinispan{
		TypeMeta: InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      strcase.ToKebab(testName),
			Namespace: Namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: ispnv1.InfinispanSpec{
			Service: ispnv1.InfinispanServiceSpec{
				Type: ispnv1.ServiceTypeDataGrid,
				Container: &ispnv1.InfinispanServiceContainerSpec{
					EphemeralStorage: true,
				},
			},
			Container: ispnv1.InfinispanContainerSpec{
				Memory: Memory,
			},
			Replicas: 1,
			Expose:   ExposeServiceSpec(testKube),
			ConfigListener: &ispnv1.ConfigListenerSpec{
				Enabled: false,
				Logging: &ispnv1.ConfigListenerLoggingSpec{
					Level: ispnv1.ConfigListenerLoggingDebug,
				},
			},
			Logging: &ispnv1.InfinispanLoggingSpec{
				Categories: map[string]ispnv1.LoggingLevelType{
					"org.infinispan.SERVER": ispnv1.LoggingLevelDebug,
				},
			},
		},
	}

	// Explicitly set the ServingCertsMode so that Openshift automatic encryption configuration is applied when running
	// tests locally
	if RunLocalOperator == "TRUE" {
		infinispan.Spec.Version = VersionManager().Latest().Ref()
		ispnv1.ServingCertsMode = testKube.Kubernetes.GetServingCertsMode(context.TODO())
	}

	if OperandVersion != "" {
		infinispan.Spec.Version = OperandVersion
	}

	if CPU != "" {
		infinispan.Spec.Container.CPU = CPU
	}

	if initializer != nil {
		initializer(infinispan)
	}

	// Apply the defaults last
	if RunLocalOperator == "TRUE" {
		infinispan.Default()
	}

	return infinispan
}

func WebServerDeployment(name, namespace, configName, mountPath, imageName string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": name},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: map[string]string{"app": name},
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
			},
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
		okRoute, err := testKube.Kubernetes.IsGroupVersionSupported(routev1.SchemeGroupVersion.String(), "Route")
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
	pass, err := users.UserPassword(user, i.GetSecretName(), i.Namespace, kube.Kubernetes, context.TODO())
	ExpectNoError(err)
	return NewHTTPClient(user, pass, protocol)
}

func HTTPClientForCluster(i *ispnv1.Infinispan, kube *TestKubernetes) HTTPClient {
	return kube.WaitForExternalService(i, RouteTimeout, clientForCluster(i, kube))
}

func HTTPSClientForCluster(i *ispnv1.Infinispan, tlsConfig *tls.Config, kube *TestKubernetes) HTTPClient {

	userAndPassword := func() (*string, *string) {
		user := constants.DefaultDeveloperUser
		pass, err := users.UserPassword(user, i.GetSecretName(), i.Namespace, kube.Kubernetes, context.TODO())
		ExpectNoError(err)
		return &user, &pass
	}

	var client HTTPClient
	clientCert := i.Spec.Security.EndpointEncryption.ClientCert
	if clientCert != "" && clientCert != ispnv1.ClientCertNone {
		if clientCert == ispnv1.ClientCertAuthenticate || !i.IsAuthenticationEnabled() {
			client = NewClient(authCert, nil, nil, "https", tlsConfig)
		} else {
			user, pass := userAndPassword()
			client = NewClient(authDigest, user, pass, "https", tlsConfig)
		}
	} else {
		if i.IsAuthenticationEnabled() {
			user, pass := userAndPassword()
			client = NewClient(authDigest, user, pass, "https", tlsConfig)
		} else {
			client = NewClient(authNone, nil, nil, "https", tlsConfig)
		}
	}
	return kube.WaitForExternalService(i, RouteTimeout, client)
}
