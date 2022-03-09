package xsite

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

type TLSMode string

const (
	// no TLS configured
	NoTLS = "none"
	// enables TLS but uses the default value
	DefaultTLS = "default"
	// enables TLS and uses the same keystore for Gossip Router and Infinispan servers
	SingleKeyStoreTLS = "single"
)

type crossSiteKubernetes struct {
	kube      *tutils.TestKubernetes
	crossSite ispnv1.Infinispan
	namespace string
	context   string
	apiServer string
}

func crossSiteSpec(name string, replicas int32, primarySite, backupSite, siteNamespace, xsiteSecretName string, exposeType ispnv1.CrossSiteExposeType, exposePort int32) *ispnv1.Infinispan {
	return &ispnv1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", name, primarySite),
		},
		Spec: ispnv1.InfinispanSpec{
			Replicas: replicas,
			Service: ispnv1.InfinispanServiceSpec{
				Type: ispnv1.ServiceTypeDataGrid,
				Sites: &ispnv1.InfinispanSitesSpec{
					Local: ispnv1.InfinispanSitesLocalSpec{
						Name: primarySite,
						Expose: ispnv1.CrossSiteExposeSpec{
							Type: exposeType,
							Port: exposePort,
						},
						MaxRelayNodes: 2,
					},
					Locations: []ispnv1.InfinispanSiteLocationSpec{
						{
							Name:        backupSite,
							Namespace:   siteNamespace,
							SecretName:  xsiteSecretName,
							ClusterName: fmt.Sprintf("%s-%s", name, backupSite),
						},
					},
				},
				Container: &ispnv1.InfinispanServiceContainerSpec{
					EphemeralStorage: true,
				},
			},
			Container: ispnv1.InfinispanContainerSpec{
				Memory: tutils.Memory,
			},
			ConfigListener: &ispnv1.ConfigListenerSpec{
				// Disable the ConfigListener to reduce the total number of resources required
				Enabled: false,
			},
		},
	}
}

func crossSiteCertificateSecret(secretName, namespace string, clientConfig *api.Config, ctx string) *corev1.Secret {
	clusterKey := clientConfig.Contexts[ctx].Cluster
	authInfoKey := clientConfig.Contexts[ctx].AuthInfo
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"certificate-authority": clientConfig.Clusters[clusterKey].CertificateAuthorityData,
			"client-certificate":    clientConfig.AuthInfos[authInfoKey].ClientCertificateData,
			"client-key":            clientConfig.AuthInfos[authInfoKey].ClientKeyData,
		},
	}
}

func crossSiteTokenSecret(secretName, namespace string, token []byte) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"token": token,
		},
	}
}

func createTLSKeysStoreSecret(keyStore []byte, secretName, namespace, password string, filename *string) *corev1.Secret {
	if filename == nil {
		defaultFilename := constants.DefaultSiteKeyStoreFileName
		filename = &defaultFilename
	}
	return createGenericTLSSecret(keyStore, secretName, namespace, password, *filename)
}

func createTLSTrustStoreSecret(trustStore []byte, secretName, namespace, password string, filename *string) *corev1.Secret {
	if filename == nil {
		defaultFilename := constants.DefaultSiteTrustStoreFileName
		filename = &defaultFilename
	}
	return createGenericTLSSecret(trustStore, secretName, namespace, password, *filename)
}

func createGenericTLSSecret(data []byte, secretName, namespace, password, filename string) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"password": password,
		},
		Data: map[string][]byte{
			filename: data,
		},
	}
	return secret
}

func TestCrossSiteViewInternal(t *testing.T) {
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, NoTLS, nil)
}

// TestDefaultTLSInternal tests if the TLS connection works for internal cross-site communication
func TestDefaultTLSInternal(t *testing.T) {
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, DefaultTLS, nil)
}

// TestDefaultTLSInternalVersion3 tests if the TLSv1.3 connection works for internal cross-site communication
func TestDefaultTLSInternalVersion3(t *testing.T) {
	protocol := ispnv1.TLSVersion13
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, DefaultTLS, &protocol)
}

// TestSingleTLSInternal tests if the TLS connection works for internal cross-site communication and custom keystore and truststore
func TestSingleTLSInternal(t *testing.T) {
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, SingleKeyStoreTLS, nil)
}

// TestSingleTLSInternalVersion3 tests if the TLSv1.3 connection works for internal cross-site communication and custom keystore and truststore
func TestSingleTLSInternalVersion3(t *testing.T) {
	protocol := ispnv1.TLSVersion13
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, SingleKeyStoreTLS, &protocol)
}

func TestCrossSiteViewInternalMultiPod(t *testing.T) {
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 2, NoTLS, nil)
}

// TestDefaultTLSInternalMultiPod tests if the TLS connection works for internal cross-site communication and multi pod clusters
func TestDefaultTLSInternalMultiPod(t *testing.T) {
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 2, DefaultTLS, nil)
}

func TestCrossSiteViewKubernetesNodePort(t *testing.T) {
	// Cross-Site between clusters will need to setup two instances of the Kind for Travis CI
	// Not be able to test on the separate OCP/OKD instance (probably with AWS/Azure LoadBalancer support only)
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeNodePort, 0, 1, NoTLS, nil)
}

// TestDefaultTLSKubernetesNodePort tests if the TLS connection works with NodePort.
func TestDefaultTLSKubernetesNodePort(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeNodePort, 0, 1, DefaultTLS, nil)
}

func TestCrossSiteViewOpenshiftNodePort(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeNodePort, 0, 1, NoTLS, nil)
}

func TestCrossSiteViewKubernetesLoadBalancer(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 1, NoTLS, nil)
}

// TestDefaultTLSKubernetesLoadBalancer tests if the TLS connection works with LoadBalancer.
func TestDefaultTLSKubernetesLoadBalancer(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 1, DefaultTLS, nil)
}

func TestCrossSiteViewOpenshiftLoadBalancer(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 1, NoTLS, nil)
}

func TestCrossSiteViewLoadBalancerWithPort(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 1443, 1, NoTLS, nil)
}

// TestDefaultTLSLoadBalancerWithPort tests if the TLS connection works with LoadBalancer and a custom port
func TestDefaultTLSLoadBalancerWithPort(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 1443, 1, DefaultTLS, nil)
}

func TestDefaultTLSOpenshiftRoute(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeRoute, 0, 1, DefaultTLS, nil)
}

var multiSite = map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
var singleSite = map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}

func TestMain(m *testing.M) {
	setUpSingleSite()

	for _, s := range setUpMultipleSites() {
		defer multiSite["xsite1"].kube.DeleteSecret(s)
		defer multiSite["xsite2"].kube.DeleteSecret(s)
		defer singleSite["xsite1"].kube.DeleteSecret(s)
		defer singleSite["xsite2"].kube.DeleteSecret(s)
	}

	for _, s := range setUpDefaultTLSSecrets() {
		defer multiSite["xsite1"].kube.DeleteSecret(s)
		defer multiSite["xsite2"].kube.DeleteSecret(s)
		defer singleSite["xsite1"].kube.DeleteSecret(s)
		defer singleSite["xsite2"].kube.DeleteSecret(s)
	}

	for _, s := range setUpSingleTLSKeystoreSecrets() {
		defer multiSite["xsite1"].kube.DeleteSecret(s)
		defer multiSite["xsite2"].kube.DeleteSecret(s)
		defer singleSite["xsite1"].kube.DeleteSecret(s)
		defer singleSite["xsite2"].kube.DeleteSecret(s)
	}

	// exec test and this returns an exit code to pass to os
	os.Exit(m.Run())
}

func setUpMultipleSites() []*corev1.Secret {
	clientConfig := clientcmd.GetConfigFromFileOrDie(kube.FindKubeConfig())
	for instance, testKube := range multiSite {
		testKube.context = fmt.Sprintf("kind-%s", instance)
		testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, instance)
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
		clusterContextName := clientConfig.Contexts[testKube.context].Cluster
		apiServerUrl, err := url.Parse(clientConfig.Clusters[clusterContextName].Server)
		tutils.ExpectNoError(err)
		testKube.apiServer = apiServerUrl.Host
	}

	// Kubernetes secrets
	kubeSite1Secret := crossSiteCertificateSecret("secret-kube-xsite2", multiSite["xsite1"].namespace, clientConfig, multiSite["xsite2"].context)
	kubeSite2Secret := crossSiteCertificateSecret("secret-kube-xsite1", multiSite["xsite2"].namespace, clientConfig, multiSite["xsite1"].context)
	multiSite["xsite1"].kube.CreateSecret(kubeSite1Secret)
	multiSite["xsite2"].kube.CreateSecret(kubeSite2Secret)

	// Openshift secrets
	serviceAccount := tutils.OperatorSAName
	operatorNamespaceSite1 := constants.GetWithDefault(tutils.OperatorNamespace, multiSite["xsite1"].namespace)
	tokenSecretXsite1, err := kube.LookupServiceAccountTokenSecret(serviceAccount, operatorNamespaceSite1, multiSite["xsite1"].kube.Kubernetes.Client, context.TODO())
	tutils.ExpectNoError(err)

	operatorNamespaceSite2 := constants.GetWithDefault(tutils.OperatorNamespace, multiSite["xsite2"].namespace)
	tokenSecretXsite2, err := kube.LookupServiceAccountTokenSecret(serviceAccount, operatorNamespaceSite2, multiSite["xsite2"].kube.Kubernetes.Client, context.TODO())
	tutils.ExpectNoError(err)

	osSite1Secret := crossSiteTokenSecret("secret-openshift-xsite2", multiSite["xsite1"].namespace, tokenSecretXsite2.Data["token"])
	osSite2Secret := crossSiteTokenSecret("secret-openshift-xsite1", multiSite["xsite2"].namespace, tokenSecretXsite1.Data["token"])
	multiSite["xsite1"].kube.CreateSecret(osSite1Secret)
	multiSite["xsite2"].kube.CreateSecret(osSite2Secret)

	return []*corev1.Secret{kubeSite1Secret, kubeSite2Secret, osSite1Secret, osSite2Secret}
}

func setUpSingleSite() {
	clientConfig := clientcmd.GetConfigFromFileOrDie(kube.FindKubeConfig())
	for _, testKube := range singleSite {
		testKube.context = clientConfig.CurrentContext
		testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, "xsite2")
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
	}
}

func setUpDefaultTLSSecrets() []*corev1.Secret {
	transport, router, trust := tutils.CreateDefaultCrossSiteKeyAndTrustStore()
	secrets := make([]*corev1.Secret, 0)
	for site := range multiSite {
		transportSecretName := fmt.Sprintf("%s-transport-tls-secret", site)
		routerSecretName := fmt.Sprintf("%s-router-tls-secret", site)
		trustSecretName := fmt.Sprintf("%s-trust-tls-secret", site)

		namespace := multiSite[site].namespace

		transportSecret := createTLSKeysStoreSecret(transport, transportSecretName, namespace, tutils.KeystorePassword, nil)
		routerSecret := createTLSKeysStoreSecret(router, routerSecretName, namespace, tutils.KeystorePassword, nil)
		trustSecret := createTLSTrustStoreSecret(trust, trustSecretName, namespace, tutils.TruststorePassword, nil)

		multiSite[site].kube.CreateSecret(transportSecret)
		multiSite[site].kube.CreateSecret(routerSecret)
		multiSite[site].kube.CreateSecret(trustSecret)

		secrets = append(secrets, transportSecret, routerSecret, trustSecret)

		// single site tests use cross-site in the same namespace (xsite2) so we need to create the xsite1 secrets in xsite2 namespace
		if site == "xsite1" {
			transportSecret = createTLSKeysStoreSecret(transport, transportSecretName, singleSite["xsite2"].namespace, tutils.KeystorePassword, nil)
			routerSecret = createTLSKeysStoreSecret(router, routerSecretName, singleSite["xsite2"].namespace, tutils.KeystorePassword, nil)
			trustSecret = createTLSTrustStoreSecret(trust, trustSecretName, singleSite["xsite2"].namespace, tutils.TruststorePassword, nil)

			singleSite["xsite2"].kube.CreateSecret(transportSecret)
			singleSite["xsite2"].kube.CreateSecret(routerSecret)
			singleSite["xsite2"].kube.CreateSecret(trustSecret)

			secrets = append(secrets, transportSecret, routerSecret, trustSecret)
		}
	}
	return secrets
}

func setUpSingleTLSKeystoreSecrets() []*corev1.Secret {
	keystore, truststore := tutils.CreateCrossSiteSingleKeyStoreAndTrustStore()
	secrets := make([]*corev1.Secret, 0)
	for site := range multiSite {
		namespace := multiSite[site].namespace
		keyStoreFileName := "my-keystore.p12"
		keyStoreSecretName := fmt.Sprintf("%s-my-keystore-secret", site)
		trustStoreFileName := "my-truststore.p12"
		trustStoreSecretName := fmt.Sprintf("%s-my-truststore-secret", site)

		transportSecret := createTLSKeysStoreSecret(keystore, keyStoreSecretName, namespace, tutils.KeystorePassword, &keyStoreFileName)
		trustSecret := createTLSTrustStoreSecret(truststore, trustStoreSecretName, namespace, tutils.KeystorePassword, &trustStoreFileName)

		multiSite[site].kube.CreateSecret(transportSecret)
		multiSite[site].kube.CreateSecret(trustSecret)

		secrets = append(secrets, transportSecret, trustSecret)

		// single site tests use cross-site in the same namespace (xsite2) so we need to create the xsite1 secrets in xsite2 namespace
		if site == "xsite1" {
			transportSecret = createTLSKeysStoreSecret(keystore, keyStoreSecretName, singleSite["xsite2"].namespace, tutils.KeystorePassword, &keyStoreFileName)
			trustSecret = createTLSTrustStoreSecret(truststore, trustStoreSecretName, singleSite["xsite2"].namespace, tutils.KeystorePassword, &trustStoreFileName)

			singleSite["xsite2"].kube.CreateSecret(transportSecret)
			singleSite["xsite2"].kube.CreateSecret(trustSecret)

			secrets = append(secrets, transportSecret, trustSecret)
		}
	}

	return secrets
}

func testCrossSiteView(t *testing.T, isMultiCluster bool, schemeType ispnv1.CrossSiteSchemeType, exposeType ispnv1.CrossSiteExposeType, exposePort, podsPerSite int32, tlsMode TLSMode, tlsProtocol *ispnv1.TLSProtocol) {
	t.Parallel()
	var testKubes map[string]*crossSiteKubernetes

	if isMultiCluster {
		var site1SecretName, site2SecretName string
		testKubes = multiSite
		if schemeType == ispnv1.CrossSiteSchemeTypeKubernetes {
			site1SecretName = "secret-kube-xsite1"
			site2SecretName = "secret-kube-xsite2"
		} else if schemeType == ispnv1.CrossSiteSchemeTypeOpenShift {
			site1SecretName = "secret-openshift-xsite1"
			site2SecretName = "secret-openshift-xsite2"
		}
		testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), podsPerSite, "xsite1", "xsite2", testKubes["xsite2"].namespace, site2SecretName, exposeType, exposePort)
		testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), podsPerSite, "xsite2", "xsite1", testKubes["xsite1"].namespace, site1SecretName, exposeType, exposePort)

		testKubes["xsite1"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, testKubes["xsite2"].apiServer)
		testKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, testKubes["xsite1"].apiServer)
	} else {
		testKubes = singleSite
		testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), podsPerSite, "xsite1", "xsite2", "", "", exposeType, exposePort)
		testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), podsPerSite, "xsite2", "xsite1", "", "", exposeType, exposePort)

	}

	defer testKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite1"].namespace)
	defer testKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite2"].namespace)

	// Check if Route is available
	if exposeType == ispnv1.CrossSiteExposeTypeRoute {
		okRoute, err := testKubes["xsite1"].kube.Kubernetes.IsGroupVersionSupported(routev1.SchemeGroupVersion.String(), "Route")
		tutils.ExpectNoError(err)
		if !okRoute {
			t.Skip("Route not available. Skipping test")
		}
		okRoute, err = testKubes["xsite2"].kube.Kubernetes.IsGroupVersionSupported(routev1.SchemeGroupVersion.String(), "Route")
		tutils.ExpectNoError(err)
		if !okRoute {
			t.Skip("Route not available. Skipping test")
		}
	}

	if tlsMode == DefaultTLS {
		for site := range testKubes {
			transportSecretName := fmt.Sprintf("%s-transport-tls-secret", site)
			routerSecretName := fmt.Sprintf("%s-router-tls-secret", site)
			trustSecretName := fmt.Sprintf("%s-trust-tls-secret", site)
			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption = &ispnv1.EncryptionSiteSpec{}
			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TransportKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: transportSecretName,
			}
			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.RouterKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: routerSecretName,
			}
			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TrustStore = &ispnv1.CrossSiteTrustStore{
				SecretName: trustSecretName,
			}
			if tlsProtocol != nil {
				testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.Protocol = *tlsProtocol
			}
		}
	} else if tlsMode == SingleKeyStoreTLS {
		for site := range testKubes {
			keyStoreFileName := "my-keystore.p12"
			keyStoreSecretName := fmt.Sprintf("%s-my-keystore-secret", site)
			trustStoreFileName := "my-truststore.p12"
			trustStoreSecretName := fmt.Sprintf("%s-my-truststore-secret", site)

			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption = &ispnv1.EncryptionSiteSpec{}
			if tlsProtocol != nil {
				testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.Protocol = *tlsProtocol
			}
			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TransportKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: keyStoreSecretName,
				Filename:   keyStoreFileName,
				Alias:      "same",
			}
			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.RouterKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: keyStoreSecretName,
				Filename:   keyStoreFileName,
				Alias:      "same",
			}
			testKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TrustStore = &ispnv1.CrossSiteTrustStore{
				SecretName: trustStoreSecretName,
				Filename:   trustStoreFileName,
			}
		}
	}

	testKubes["xsite1"].crossSite.Labels = map[string]string{"test-name": t.Name()}
	testKubes["xsite2"].crossSite.Labels = map[string]string{"test-name": t.Name()}

	testKubes["xsite1"].kube.CreateInfinispan(&testKubes["xsite1"].crossSite, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.CreateInfinispan(&testKubes["xsite2"].crossSite, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanPods(int(podsPerSite), tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.WaitForInfinispanPods(int(podsPerSite), tutils.SinglePodTimeout, testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	ispnXSite1 := testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
}
