package xsite

import (
	"context"
	"fmt"
	"net/url"
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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func crossSiteSpec(name string, replicas int32, primarySite, backupSite, siteNamespace string, exposeType ispnv1.CrossSiteExposeType, exposePort int32) *ispnv1.Infinispan {
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
							SecretName:  secretSiteName(backupSite),
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

func crossSiteCertificateSecret(siteName, namespace string, clientConfig *api.Config, ctx string) *corev1.Secret {
	clusterKey := clientConfig.Contexts[ctx].Cluster
	authInfoKey := clientConfig.Contexts[ctx].AuthInfo
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretSiteName(siteName),
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

func crossSiteTokenSecret(siteName, namespace string, token []byte) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretSiteName(siteName),
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"token": token,
		},
	}
}

func secretSiteName(siteName string) string {
	return fmt.Sprintf("secret-%s", siteName)
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

func TestCrossSiteGracefulShutdown(t *testing.T) {
	testName := tutils.TestName(t)
	tesKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(kube.FindKubeConfig())

	tesKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 2, "xsite1", "xsite2", "", ispnv1.CrossSiteExposeTypeClusterIP, 0)
	tesKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 2, "xsite2", "xsite1", "", ispnv1.CrossSiteExposeTypeClusterIP, 0)
	for _, testKube := range tesKubes {
		testKube.context = clientConfig.CurrentContext
		testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, "xsite2")
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
	}

	tesKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	tesKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

	defer tesKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, tesKubes["xsite1"].namespace)
	defer tesKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.CreateInfinispan(&tesKubes["xsite1"].crossSite, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.CreateInfinispan(&tesKubes["xsite2"].crossSite, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.WaitForInfinispanPods(int(2), tutils.SinglePodTimeout, tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.WaitForInfinispanPods(int(2), tutils.SinglePodTimeout, tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	var ispnXSite1 *ispnv1.Infinispan

	ispnXSite1 = tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")

	tutils.ExpectNoError(
		tesKubes["xsite1"].kube.UpdateInfinispan(&tesKubes["xsite1"].crossSite, func() {
			tesKubes["xsite1"].crossSite.Spec.Replicas = 0
		}),
	)

	tesKubes["xsite1"].kube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace)
	ispnXSite1 = tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionGracefulShutdown)
	assert.Equal(t, metav1.ConditionTrue, ispnXSite1.GetCondition(ispnv1.ConditionGracefulShutdown).Status)
	assert.Equal(t, metav1.ConditionFalse, ispnXSite1.GetCondition(ispnv1.ConditionStopping).Status)

	tutils.ExpectNoError(
		tesKubes["xsite1"].kube.UpdateInfinispan(&tesKubes["xsite1"].crossSite, func() {
			tesKubes["xsite1"].crossSite.Spec.Replicas = 2
		}),
	)
	tesKubes["xsite1"].kube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace)
	tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	ispnXSite1 = tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
}

func TestSingleGossipRouter(t *testing.T) {
	testName := tutils.TestName(t)
	tesKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(kube.FindKubeConfig())

	// setup instances
	for instance, testKube := range tesKubes {
		testKube.context = fmt.Sprintf("kind-%s", instance)
		testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, instance)
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
		clusterContextName := clientConfig.Contexts[testKube.context].Cluster
		apiServerUrl, err := url.Parse(clientConfig.Clusters[clusterContextName].Server)
		tutils.ExpectNoError(err)
		testKube.apiServer = apiServerUrl.Host
	}

	// create secrets
	tesKubes["xsite1"].kube.CreateSecret(crossSiteCertificateSecret("xsite2", tesKubes["xsite1"].namespace, clientConfig, tesKubes["xsite2"].context))
	tesKubes["xsite2"].kube.CreateSecret(crossSiteCertificateSecret("xsite1", tesKubes["xsite2"].namespace, clientConfig, tesKubes["xsite1"].context))

	defer tesKubes["xsite1"].kube.DeleteSecret(crossSiteCertificateSecret("xsite2", tesKubes["xsite1"].namespace, clientConfig, tesKubes["xsite2"].context))
	defer tesKubes["xsite2"].kube.DeleteSecret(crossSiteCertificateSecret("xsite1", tesKubes["xsite2"].namespace, clientConfig, tesKubes["xsite1"].context))

	tesKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite1", "xsite2", tesKubes["xsite2"].namespace, ispnv1.CrossSiteExposeTypeNodePort, 0)
	tesKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite2", "xsite1", tesKubes["xsite1"].namespace, ispnv1.CrossSiteExposeTypeNodePort, 0)

	tesKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	tesKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

	// Disable Gossip Router on site1
	tesKubes["xsite1"].crossSite.Spec.Service.Sites.Local.Discovery = &ispnv1.DiscoverySiteSpec{
		LaunchGossipRouter: pointer.Bool(false),
	}
	tesKubes["xsite1"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", ispnv1.CrossSiteSchemeTypeKubernetes, tesKubes["xsite2"].apiServer)

	// Prevent site2 from trying to fetch the Gossip Router from site1
	tesKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].Namespace = ""
	tesKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].ClusterName = ""
	tesKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].SecretName = ""

	defer tesKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, tesKubes["xsite1"].namespace)
	defer tesKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.CreateInfinispan(&tesKubes["xsite1"].crossSite, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.CreateInfinispan(&tesKubes["xsite2"].crossSite, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	ispnXSite1 := tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")

	assertGossipRouterPodCount(t, tesKubes["xsite1"], 0)
	expectNoCrossSiteService(tesKubes["xsite1"])

	assertGossipRouterPodCount(t, tesKubes["xsite2"], 1)
	expectsCrossSiteService(tesKubes["xsite2"])

}

func testCrossSiteView(t *testing.T, isMultiCluster bool, schemeType ispnv1.CrossSiteSchemeType, exposeType ispnv1.CrossSiteExposeType, exposePort, podsPerSite int32, tlsMode TLSMode, tlsProtocol *ispnv1.TLSProtocol) {
	testName := tutils.TestName(t)
	tesKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(kube.FindKubeConfig())

	if isMultiCluster {
		for instance, testKube := range tesKubes {
			testKube.context = fmt.Sprintf("kind-%s", instance)
			testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, instance)
			testKube.kube = tutils.NewTestKubernetes(testKube.context)
			clusterContextName := clientConfig.Contexts[testKube.context].Cluster
			apiServerUrl, err := url.Parse(clientConfig.Clusters[clusterContextName].Server)
			tutils.ExpectNoError(err)
			testKube.apiServer = apiServerUrl.Host
		}
		if schemeType == ispnv1.CrossSiteSchemeTypeKubernetes {
			tesKubes["xsite1"].kube.CreateSecret(crossSiteCertificateSecret("xsite2", tesKubes["xsite1"].namespace, clientConfig, tesKubes["xsite2"].context))
			tesKubes["xsite2"].kube.CreateSecret(crossSiteCertificateSecret("xsite1", tesKubes["xsite2"].namespace, clientConfig, tesKubes["xsite1"].context))

			defer tesKubes["xsite1"].kube.DeleteSecret(crossSiteCertificateSecret("xsite2", tesKubes["xsite1"].namespace, clientConfig, tesKubes["xsite2"].context))
			defer tesKubes["xsite2"].kube.DeleteSecret(crossSiteCertificateSecret("xsite1", tesKubes["xsite2"].namespace, clientConfig, tesKubes["xsite1"].context))
		} else if schemeType == ispnv1.CrossSiteSchemeTypeOpenShift {
			serviceAccount := tutils.OperatorSAName
			operatorNamespaceSite1 := constants.GetWithDefault(tutils.OperatorNamespace, tesKubes["xsite1"].namespace)
			tokenSecretXsite1, err := kube.LookupServiceAccountTokenSecret(serviceAccount, operatorNamespaceSite1, tesKubes["xsite1"].kube.Kubernetes.Client, context.TODO())
			tutils.ExpectNoError(err)
			operatorNamespaceSite2 := constants.GetWithDefault(tutils.OperatorNamespace, tesKubes["xsite2"].namespace)
			tokenSecretXsite2, err := kube.LookupServiceAccountTokenSecret(serviceAccount, operatorNamespaceSite2, tesKubes["xsite2"].kube.Kubernetes.Client, context.TODO())
			tutils.ExpectNoError(err)

			tesKubes["xsite1"].kube.CreateSecret(crossSiteTokenSecret("xsite2", tesKubes["xsite1"].namespace, tokenSecretXsite2.Data["token"]))
			tesKubes["xsite2"].kube.CreateSecret(crossSiteTokenSecret("xsite1", tesKubes["xsite2"].namespace, tokenSecretXsite1.Data["token"]))

			defer tesKubes["xsite1"].kube.DeleteSecret(crossSiteTokenSecret("xsite2", tesKubes["xsite1"].namespace, []byte("")))
			defer tesKubes["xsite2"].kube.DeleteSecret(crossSiteTokenSecret("xsite1", tesKubes["xsite2"].namespace, []byte("")))
		}
		tesKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite1", "xsite2", tesKubes["xsite2"].namespace, exposeType, exposePort)
		tesKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite2", "xsite1", tesKubes["xsite1"].namespace, exposeType, exposePort)

		tesKubes["xsite1"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, tesKubes["xsite2"].apiServer)
		tesKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, tesKubes["xsite1"].apiServer)
	} else {
		tesKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite1", "xsite2", "", exposeType, exposePort)
		tesKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite2", "xsite1", "", exposeType, exposePort)
		for _, testKube := range tesKubes {
			testKube.context = clientConfig.CurrentContext
			testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, "xsite2")
			testKube.kube = tutils.NewTestKubernetes(testKube.context)
		}
	}

	defer tesKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, tesKubes["xsite1"].namespace)
	defer tesKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, tesKubes["xsite2"].namespace)

	// Check if Route is available
	if exposeType == ispnv1.CrossSiteExposeTypeRoute {
		okRoute, err := tesKubes["xsite1"].kube.Kubernetes.IsGroupVersionSupported(routev1.SchemeGroupVersion.String(), "Route")
		tutils.ExpectNoError(err)
		if !okRoute {
			t.Skip("Route not available. Skipping test")
		}
		okRoute, err = tesKubes["xsite2"].kube.Kubernetes.IsGroupVersionSupported(routev1.SchemeGroupVersion.String(), "Route")
		tutils.ExpectNoError(err)
		if !okRoute {
			t.Skip("Route not available. Skipping test")
		}
	}

	if tlsMode == DefaultTLS {
		transport, router, trust := tutils.CreateDefaultCrossSiteKeyAndTrustStore()

		for site := range tesKubes {
			transportSecretName := fmt.Sprintf("%s-transport-tls-secret", site)
			routerSecretName := fmt.Sprintf("%s-router-tls-secret", site)
			trustSecretName := fmt.Sprintf("%s-trust-tls-secret", site)

			namespace := tesKubes[site].namespace

			transportSecret := createTLSKeysStoreSecret(transport, transportSecretName, namespace, tutils.KeystorePassword, nil)
			routerSecret := createTLSKeysStoreSecret(router, routerSecretName, namespace, tutils.KeystorePassword, nil)
			trustSecret := createTLSTrustStoreSecret(trust, trustSecretName, namespace, tutils.TruststorePassword, nil)

			tesKubes[site].kube.CreateSecret(transportSecret)
			tesKubes[site].kube.CreateSecret(routerSecret)
			tesKubes[site].kube.CreateSecret(trustSecret)

			if tutils.CleanupXSiteOnFinish {
				defer tesKubes[site].kube.DeleteSecret(transportSecret)
				defer tesKubes[site].kube.DeleteSecret(routerSecret)
				defer tesKubes[site].kube.DeleteSecret(trustSecret)
			}
			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption = &ispnv1.EncryptionSiteSpec{}
			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TransportKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: transportSecretName,
			}
			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.RouterKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: routerSecretName,
			}
			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TrustStore = &ispnv1.CrossSiteTrustStore{
				SecretName: trustSecretName,
			}
			if tlsProtocol != nil {
				tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.Protocol = *tlsProtocol
			}
		}
	} else if tlsMode == SingleKeyStoreTLS {
		keystore, truststore := tutils.CreateCrossSiteSingleKeyStoreAndTrustStore()
		for site := range tesKubes {
			namespace := tesKubes[site].namespace
			keyStoreFileName := "my-keystore.p12"
			keyStoreSecretName := fmt.Sprintf("%s-my-keystore-secret", site)
			trustStoreFileName := "my-truststore.p12"
			trustStoreSecretName := fmt.Sprintf("%s-my-truststore-secret", site)

			transportSecret := createTLSKeysStoreSecret(keystore, keyStoreSecretName, namespace, tutils.KeystorePassword, &keyStoreFileName)
			trustSecret := createTLSTrustStoreSecret(truststore, trustStoreSecretName, namespace, tutils.KeystorePassword, &trustStoreFileName)

			tesKubes[site].kube.CreateSecret(transportSecret)
			tesKubes[site].kube.CreateSecret(trustSecret)

			if tutils.CleanupXSiteOnFinish {
				defer tesKubes[site].kube.DeleteSecret(transportSecret)
				defer tesKubes[site].kube.DeleteSecret(trustSecret)
			}

			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption = &ispnv1.EncryptionSiteSpec{}
			if tlsProtocol != nil {
				tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.Protocol = *tlsProtocol
			}
			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TransportKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: keyStoreSecretName,
				Filename:   keyStoreFileName,
				Alias:      "same",
			}
			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.RouterKeyStore = ispnv1.CrossSiteKeyStore{
				SecretName: keyStoreSecretName,
				Filename:   keyStoreFileName,
				Alias:      "same",
			}
			tesKubes[site].crossSite.Spec.Service.Sites.Local.Encryption.TrustStore = &ispnv1.CrossSiteTrustStore{
				SecretName: trustStoreSecretName,
				Filename:   trustStoreFileName,
			}
		}
	}

	tesKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	tesKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

	tesKubes["xsite1"].kube.CreateInfinispan(&tesKubes["xsite1"].crossSite, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.CreateInfinispan(&tesKubes["xsite2"].crossSite, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.WaitForInfinispanPods(int(podsPerSite), tutils.SinglePodTimeout, tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.WaitForInfinispanPods(int(podsPerSite), tutils.SinglePodTimeout, tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	ispnXSite1 := tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
}

func assertGossipRouterPodCount(t *testing.T, testkube *crossSiteKubernetes, expected int) {
	pods := &corev1.PodList{}
	err := testkube.kube.Kubernetes.Client.List(context.TODO(), pods, &client.ListOptions{
		Namespace:     testkube.namespace,
		LabelSelector: labels.SelectorFromSet(testkube.crossSite.GossipRouterPodLabels()),
	})
	tutils.ExpectNoError(err)
	assert.Equal(t, expected, len(pods.Items))
}

func expectNoCrossSiteService(testkube *crossSiteKubernetes) {
	err := testkube.kube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{
		Name:      testkube.crossSite.GetSiteServiceName(),
		Namespace: testkube.crossSite.Namespace,
	}, &corev1.Service{})
	tutils.ExpectNotFound(err)
}

func expectsCrossSiteService(testkube *crossSiteKubernetes) {
	err := testkube.kube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{
		Name:      testkube.crossSite.GetSiteServiceName(),
		Namespace: testkube.crossSite.Namespace,
	}, &corev1.Service{})
	tutils.ExpectNoError(err)
}
