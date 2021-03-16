package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const EncryptionSecretNamePostfix = "secret-certs"

func EndpointEncryption(name string) *v1.EndpointEncryption {
	return &v1.EndpointEncryption{
		Type:           v1.CertificateSourceTypeSecret,
		CertSecretName: fmt.Sprintf("%s-%s", name, EncryptionSecretNamePostfix),
	}
}

func EncryptionSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", name, EncryptionSecretNamePostfix),
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"tls.key": tlsKey,
			"tls.crt": tlsCrt},
	}
}

const tlsCrt = `-----BEGIN CERTIFICATE-----
MIIDkzCCAnugAwIBAgIUeKgxAiU9pYocbLPcC/q1HgmNQIEwDQYJKoZIhvcNAQEL
BQAwWTELMAkGA1UEBhMCaXQxCzAJBgNVBAgMAm1pMQswCQYDVQQHDAJtaTETMBEG
A1UECgwKaW5maW5pc3BhbjEMMAoGA1UECwwDZW5nMQ0wCwYDVQQDDARpc3BuMB4X
DTE5MDkxMjEyMDEyMVoXDTI5MDkwOTEyMDEyMVowWTELMAkGA1UEBhMCaXQxCzAJ
BgNVBAgMAm1pMQswCQYDVQQHDAJtaTETMBEGA1UECgwKaW5maW5pc3BhbjEMMAoG
A1UECwwDZW5nMQ0wCwYDVQQDDARpc3BuMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAxq8jfTo1/zaUPS+ONhHAvJ3AdjgUJY4Py82FzIFzfo9b0edRvJcp
VUJ+/l8E3XesXV7NpADJxLuXCDhnMe6lDX2mCoJFkFNQXsxiXXTl+p6JFShTCaE7
unq15Zt5ZyH0b+61JDn48aLzP4y8/8NSap363uU637gh1rxocwoahwGM4ezQAs86
iLAOuce1SeLNyewjVDW/DRSH1nG7k3RolEmWD9+o1ZOe78qDq2yUhZatOGhLKaMQ
LMcaD8b0q359jUmU1Q3S8GngRemr9o5SUEPWt2r7b8JYbMw4IvcFQC210/MzvReS
M98gA2TaSX7TyHSw/IFbymXvKtIvoNKhGQIDAQABo1MwUTAdBgNVHQ4EFgQUwwsw
M2r671VcGTy/O7ZIeergMEAwHwYDVR0jBBgwFoAUwwswM2r671VcGTy/O7ZIeerg
MEAwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAhoor1miOXPgU
f02PDor7YWvXB59epJqO9PTCe+IxrjlT4NXFGoUh97PHUActyrpRrs5QdY6w1mar
V7QgaHVLhkWXTeoMocXX8DURFoDrhKL+qhlRPh56Ut4KTGBKU2+JyS3lVIPsrx2y
QvE8qX5hNT7ESbLsyzhshQwRn9PErxoshxhpPI2JtHxSzce9WUo2GzMIXwup12pM
ILVhZavwLswTWo0XziZUTMildC+4SH1fdSoS9hokvYY8JIsZ+OToa1XFf7/92K+M
vwooI+AlMd/5zB5opyR527eaT2hOoCK8wR2/EM68v97ZpuUXnrJHsb+rdCHAWUuy
ONaPRRR3rw==
-----END CERTIFICATE-----`

const tlsKey = `-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDGryN9OjX/NpQ9
L442EcC8ncB2OBQljg/LzYXMgXN+j1vR51G8lylVQn7+XwTdd6xdXs2kAMnEu5cI
OGcx7qUNfaYKgkWQU1BezGJddOX6nokVKFMJoTu6erXlm3lnIfRv7rUkOfjxovM/
jLz/w1Jqnfre5TrfuCHWvGhzChqHAYzh7NACzzqIsA65x7VJ4s3J7CNUNb8NFIfW
cbuTdGiUSZYP36jVk57vyoOrbJSFlq04aEspoxAsxxoPxvSrfn2NSZTVDdLwaeBF
6av2jlJQQ9a3avtvwlhszDgi9wVALbXT8zO9F5Iz3yADZNpJftPIdLD8gVvKZe8q
0i+g0qEZAgMBAAECggEAHOdRpGAZht0rx5Lpf1gpz8arPwd9dtEp3x4w/sU+RgUY
+HpMW8Ep1CtuShcMoCNOwe6Ov/MVZzdbC2kZKhxrioDi7NhywkI8iO32yV2+Ly1t
B9Tr75SzGbfMSnDJwoUgCECTvYdpfc2U0YPp4tNJZBVDb7WtUOp6kcCq+UFZBpaj
U5QbaYD1Q5/xgDMuqfK25bmxWab0nNpqDn5fLzphK2sKpSd8X5Jhd4Btempf+mZ5
c6/24pRB7usOBAiAYnb6sTT7DxSyL9bmaNJFhnbTRRPBBZySqSh2rjho+AEz268/
Lhz/Rk5VsZsctV94RpuG1ebXBAmDUCm7JCbFsZkVmQKBgQD0BeChgMly/xuggIfg
a5Myz0lsFmOJ2JfYnGKLaGb1bna8+Ig+5q7xOjcV5lsp26S7biVuWEySl83ua4qg
Pd3BTdVpTKjIk5eJlZax9iiVyF0pPkjSaI0U0LAG7VmRT766+OghXvORKdCBvYup
u+Sx2vN+lxKzOxo6du7vjsJpbwKBgQDQb5VrM8SeHfsv3wK/oCPeaulpxC05/VxU
xHBzvJkeklnNALqrNQIv4ywUK9vPmpLy3BspOMq+J44CQumrnX5xKq4AI7WBrNzp
Td58iFzh9tKyl4o6fRvQPQcAYff/wbyAzpRaFE9lTbNeg9D7i/FOhCYle+tfiHKN
Lkcx4hYJ9wKBgQDatRHZbkYfTUoDlm8x0vjBB0v1FjPsbjXaLH+eFtqAiprdT5s9
VR/ikJyigi2e3H9OhbACsB0hHfGyCKzcZdaE1C+8CrsT2kRtSacgpVFGvafRuUMn
YhFgYJIEA2LNfD2j8kaK8kE3D9UTE0FDxWV5ipXGFbzq6sPdNo98IeVY/QKBgBEz
9HAZoLOwI8gqrs5kCDHWPxeEonrzx0gTwng666RTTegWlFGHGXwcUsoDaKv0xQYY
VoGLd2hEWXskTKbmY8YxUJUgXV2rh7wVujQrCQd5WKB202jKZJ5GOyqz60UHl2hG
JIZewMLKq/A0Du6D+VGSpJdZZ+7FkzbFyAh88Xa3AoGAB7MNR0PtxzVAOQbxgmCe
1Pbe7PR5oq93tbvw4eg5xkYnfihnzdzsXlM44gS2cd/Evgsu0Gk8G20id6mbdD6f
84MsEvv3r/jU9bbYQxWaQvacJ9K7TuCgtXEnBAZg6CGzEPorHiqIGlW+LkhmAGUg
KbdDDTEAHRXtTh9n1TIOXlE=
-----END PRIVATE KEY-----`

var MinimalSpec = v1.Infinispan{
	TypeMeta: InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: DefaultClusterName,
	},
	Spec: v1.InfinispanSpec{
		Replicas: 2,
	},
}

func DefaultSpec(testKube *TestKubernetes) *v1.Infinispan {
	return &v1.Infinispan{
		TypeMeta: InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultClusterName,
			Namespace: Namespace,
		},
		Spec: v1.InfinispanSpec{
			Service: v1.InfinispanServiceSpec{
				Type: v1.ServiceTypeDataGrid,
			},
			Container: v1.InfinispanContainerSpec{
				CPU:    CPU,
				Memory: Memory,
			},
			Replicas: 1,
			Expose:   ExposeServiceSpec(testKube),
		},
	}
}

func ExposeServiceSpec(testKube *TestKubernetes) *v1.ExposeSpec {
	return &v1.ExposeSpec{
		Type: exposeServiceType(testKube),
	}
}

func exposeServiceType(testKube *TestKubernetes) v1.ExposeType {
	exposeServiceType := constants.GetEnvWithDefault("EXPOSE_SERVICE_TYPE", string(v1.ExposeTypeNodePort))
	switch exposeServiceType {
	case string(v1.ExposeTypeNodePort):
		return v1.ExposeTypeNodePort
	case string(v1.ExposeTypeLoadBalancer):
		return v1.ExposeTypeLoadBalancer
	case string(v1.ExposeTypeRoute):
		okRoute, err := testKube.Kubernetes.IsGroupVersionSupported(routev1.GroupVersion.String(), "Route")
		if err == nil && okRoute {
			return v1.ExposeTypeRoute
		}
		panic(fmt.Errorf("expose type Route is not supported on the platform: %w", err))
	default:
		panic(fmt.Errorf("unknown service type %s", exposeServiceType))
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

func clientForCluster(i *v1.Infinispan, kube *TestKubernetes) HTTPClient {
	protocol := kube.GetSchemaForRest(i)

	if !i.IsAuthenticationEnabled() {
		return NewHTTPClientNoAuth(protocol)
	}

	user := constants.DefaultDeveloperUser
	pass, err := users.UserPassword(user, i.GetSecretName(), i.Namespace, kube.Kubernetes)
	ExpectNoError(err)
	return NewHTTPClient(user, pass, protocol)
}

func HTTPClientAndHost(i *v1.Infinispan, kube *TestKubernetes) (string, HTTPClient) {
	client := clientForCluster(i, kube)
	hostAddr := kube.WaitForExternalService(i.GetServiceExternalName(), i.Namespace, i.GetExposeType(), RouteTimeout, client)
	return hostAddr, client
}
