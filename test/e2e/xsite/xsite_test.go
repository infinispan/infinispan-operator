package xsite

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	ispnv2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
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

func crossSiteSpec(name string, replicas int32, primarySite, backupSite, siteNamespace string, exposeType ispnv1.CrossSiteExposeType, exposePort, nodePort int32) *ispnv1.Infinispan {
	infinispan := &ispnv1.Infinispan{
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
						Discovery: &ispnv1.DiscoverySiteSpec{
							Memory: "500Mi",
							CPU:    "500m",
						},
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

	if nodePort > 0 {
		infinispan.Spec.Service.Sites.Local.Expose.NodePort = nodePort
	}

	if tutils.OperandVersion != "" {
		infinispan.Spec.Version = tutils.OperandVersion
	}

	return infinispan
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
	testCrossSiteView(t, false, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, NoTLS, nil)
}

// TestDefaultTLSInternal tests if the TLS connection works for internal cross-site communication
func TestDefaultTLSInternal(t *testing.T) {
	testCrossSiteView(t, false, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, DefaultTLS, nil)
}

// TestDefaultTLSInternalVersion2 tests if the TLSv1.2 connection works for internal cross-site communication
func TestDefaultTLSInternalVersion2(t *testing.T) {
	protocol := ispnv1.TLSVersion12
	testCrossSiteView(t, false, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, DefaultTLS, &protocol)
}

// TestSingleTLSInternal tests if the TLS connection works for internal cross-site communication and custom keystore and truststore
func TestSingleTLSInternal(t *testing.T) {
	testCrossSiteView(t, false, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, SingleKeyStoreTLS, nil)
}

// TestSingleTLSInternalVersion2 tests if the TLSv1.2 connection works for internal cross-site communication and custom keystore and truststore
func TestSingleTLSInternalVersion2(t *testing.T) {
	protocol := ispnv1.TLSVersion12
	testCrossSiteView(t, false, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 1, SingleKeyStoreTLS, &protocol)
}

func TestCrossSiteViewInternalMultiPod(t *testing.T) {
	testCrossSiteView(t, false, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 2, NoTLS, nil)
}

// TestDefaultTLSInternalMultiPod tests if the TLS connection works for internal cross-site communication and multi pod clusters
func TestDefaultTLSInternalMultiPod(t *testing.T) {
	testCrossSiteView(t, false, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 2, DefaultTLS, nil)
}

func TestCrossSiteViewKubernetesNodePort(t *testing.T) {
	tutils.SkipForOpenShift(t, "Kubernetes scheme is unsupported by the testsuite running against the OpenShift at the moment")
	tutils.SkipForAWS(t, "Nodes hostnames and IPs are private and not accessible on AWS")
	// Cross-Site between clusters will need to setup two instances of the Kind for Travis CI
	// Not be able to test on the separate OCP/OKD instance (probably with AWS/Azure LoadBalancer support only)
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeNodePort, 0, 1, NoTLS, nil)
}

// TestCrossSiteViewKubernetesNodePortE is the same as TestCrossSiteViewKubernetesNodePort except the NodePort value is explicitly configured
func TestCrossSiteViewKubernetesNodePortE(t *testing.T) {
	tutils.SkipForOpenShift(t, "Kubernetes scheme is unsupported by the testsuite running against the OpenShift at the moment")
	tutils.SkipForAWS(t, "Nodes hostnames and IPs are private and not accessible on AWS")
	testCrossSiteView(t, true, true, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeNodePort, -1, 1, NoTLS, nil)
}

// TestDefaultTLSKubernetesNodePort tests if the TLS connection works with NodePort.
func TestDefaultTLSKubernetesNodePort(t *testing.T) {
	tutils.SkipForOpenShift(t, "Kubernetes scheme is unsupported by the testsuite running against the OpenShift at the moment")
	tutils.SkipForAWS(t, "Nodes hostnames and IPs are private and not accessible on AWS")
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeNodePort, 0, 1, DefaultTLS, nil)
}

func TestCrossSiteViewOpenshiftNodePort(t *testing.T) {
	tutils.SkipForAWS(t, "Nodes hostnames and IPs are private and not accessible on AWS")
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeNodePort, 0, 1, NoTLS, nil)
}

func TestCrossSiteViewKubernetesLoadBalancer(t *testing.T) {
	tutils.SkipForOpenShift(t, "Kubernetes scheme is unsupported by the testsuite running against the OpenShift at the moment")
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 1, NoTLS, nil)
}

// TestDefaultTLSKubernetesLoadBalancer tests if the TLS connection works with LoadBalancer.
func TestDefaultTLSKubernetesLoadBalancer(t *testing.T) {
	tutils.SkipForOpenShift(t, "Kubernetes scheme is unsupported by the testsuite running against the OpenShift at the moment")
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 1, DefaultTLS, nil)
}

func TestCrossSiteViewOpenshiftLoadBalancer(t *testing.T) {
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 1, NoTLS, nil)
}

func TestCrossSiteViewLoadBalancerWithPort(t *testing.T) {
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 1443, 1, NoTLS, nil)
}

// TestDefaultTLSLoadBalancerWithPort tests if the TLS connection works with LoadBalancer and a custom port
func TestDefaultTLSLoadBalancerWithPort(t *testing.T) {
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 1443, 1, DefaultTLS, nil)
}

func TestDefaultTLSOpenshiftRoute(t *testing.T) {
	testCrossSiteView(t, true, false, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeRoute, 0, 1, DefaultTLS, nil)
}

func TestCrossSiteGracefulShutdown(t *testing.T) {
	testName := tutils.TestName(t)
	testKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(tutils.FindKubeConfig())

	testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 2, "xsite1", "xsite2", "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 0)
	testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 2, "xsite2", "xsite1", "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 0)
	for _, testKube := range testKubes {
		testKube.context = clientConfig.CurrentContext
		testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, "xsite2")
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
	}

	testKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	testKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

	defer testKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite1"].namespace)
	defer testKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.CreateInfinispan(&testKubes["xsite1"].crossSite, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.CreateInfinispan(&testKubes["xsite2"].crossSite, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanPods(int(2), tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.WaitForInfinispanPods(int(2), tutils.SinglePodTimeout, testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	var ispnXSite1 *ispnv1.Infinispan

	ispnXSite1 = testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")

	tutils.ExpectNoError(
		testKubes["xsite1"].kube.UpdateInfinispan(&testKubes["xsite1"].crossSite, func() {
			testKubes["xsite1"].crossSite.Spec.Replicas = 0
		}),
	)

	testKubes["xsite1"].kube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
	ispnXSite1 = testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionGracefulShutdown)
	assert.Equal(t, metav1.ConditionTrue, ispnXSite1.GetCondition(ispnv1.ConditionGracefulShutdown).Status)
	assert.Equal(t, metav1.ConditionFalse, ispnXSite1.GetCondition(ispnv1.ConditionStopping).Status)

	tutils.ExpectNoError(
		testKubes["xsite1"].kube.UpdateInfinispan(&testKubes["xsite1"].crossSite, func() {
			testKubes["xsite1"].crossSite.Spec.Replicas = 2
		}),
	)
	testKubes["xsite1"].kube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
	testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	ispnXSite1 = testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
}

func TestSingleGossipRouter(t *testing.T) {
	testName := tutils.TestName(t)
	testKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(tutils.FindKubeConfig())

	// setup instances
	for instance, testKube := range testKubes {
		testKube.context = fmt.Sprintf("kind-%s", instance)
		testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, instance)
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
		clusterContextName := clientConfig.Contexts[testKube.context].Cluster
		apiServerUrl, err := url.Parse(clientConfig.Clusters[clusterContextName].Server)
		tutils.ExpectNoError(err)
		testKube.apiServer = apiServerUrl.Host
	}

	// create secrets
	operatorNamespaceSite1 := constants.GetWithDefault(tutils.OperatorNamespace, testKubes["xsite1"].namespace)
	operatorNamespaceSite2 := constants.GetWithDefault(tutils.OperatorNamespace, testKubes["xsite2"].namespace)
	xsite1Token := getServiceAccountToken(operatorNamespaceSite1, testKubes["xsite1"].kube)
	xsite2Token := getServiceAccountToken(operatorNamespaceSite2, testKubes["xsite2"].kube)

	defer testKubes["xsite1"].kube.DeleteSecret(crossSiteTokenSecret("xsite2", testKubes["xsite1"].namespace, []byte("")))
	defer testKubes["xsite2"].kube.DeleteSecret(crossSiteTokenSecret("xsite1", testKubes["xsite2"].namespace, []byte("")))

	testKubes["xsite1"].kube.CreateSecret(crossSiteTokenSecret("xsite2", testKubes["xsite1"].namespace, xsite2Token))
	testKubes["xsite2"].kube.CreateSecret(crossSiteTokenSecret("xsite1", testKubes["xsite2"].namespace, xsite1Token))

	testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite1", "xsite2", testKubes["xsite2"].namespace, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 0)
	testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite2", "xsite1", testKubes["xsite1"].namespace, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 0)

	testKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	testKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

	// Disable Gossip Router on site1
	testKubes["xsite1"].crossSite.Spec.Service.Sites.Local.Discovery = &ispnv1.DiscoverySiteSpec{
		LaunchGossipRouter: pointer.Bool(false),
	}
	testKubes["xsite1"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", ispnv1.CrossSiteSchemeTypeOpenShift, testKubes["xsite2"].apiServer)

	// Prevent site2 from trying to fetch the Gossip Router from site1
	testKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].Namespace = ""
	testKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].ClusterName = ""
	testKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].SecretName = ""

	defer testKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite1"].namespace)
	defer testKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.CreateInfinispan(&testKubes["xsite1"].crossSite, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.CreateInfinispan(&testKubes["xsite2"].crossSite, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	ispnXSite1 := testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")

	assertGossipRouterPodCount(t, testKubes["xsite1"], 0)
	expectNoCrossSiteService(testKubes["xsite1"])

	assertGossipRouterPodCount(t, testKubes["xsite2"], 1)
	expectsCrossSiteService(testKubes["xsite2"])
}

func TestSuspectAndHearbeatConfig(t *testing.T) {
	// single cluster cross-site is enough.
	// testing both configuration in the same test to save time
	// test inspects the configmap and the arguments list of Gossip Router

	testName := tutils.TestName(t)
	testKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(tutils.FindKubeConfig())

	for _, testKube := range testKubes {
		testKube.context = clientConfig.CurrentContext
		testKube.namespace = fmt.Sprintf("%s-xsite2", tutils.Namespace)
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
	}

	defer testKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite1"].namespace)
	defer testKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite2"].namespace)

	testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite1", "xsite2", "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 0)
	testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite2", "xsite1", "", ispnv1.CrossSiteExposeTypeClusterIP, 0, 0)

	testKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	testKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

	// site 1 configuration
	// enable suspect events (disabled by default) and disable hearbeats (enabled by default)
	testKubes["xsite1"].crossSite.Spec.Service.Sites.Local.Discovery.SuspectEvents = true
	testKubes["xsite1"].crossSite.Spec.Service.Sites.Local.Discovery.Heartbeats = &ispnv1.GossipRouterHeartbeatSpec{
		Enabled: pointer.Bool(false),
	}

	// site 2 configuration
	// check heartbeats values to test parsers and to check if they are in the correct attributes
	// validation already tested in the webhook tests
	hbInterval := pointer.Int64(20000)
	hbTimeout := pointer.Int64(50000)
	testKubes["xsite2"].crossSite.Spec.Service.Sites.Local.Discovery.Heartbeats = &ispnv1.GossipRouterHeartbeatSpec{
		Interval: hbInterval,
		Timeout:  hbTimeout,
	}

	testKubes["xsite1"].kube.CreateInfinispan(&testKubes["xsite1"].crossSite, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.CreateInfinispan(&testKubes["xsite2"].crossSite, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	ispnXSite1 := testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")

	expectGossipRouterSupectValue(t, testKubes["xsite1"], true)
	expectGossipRouterSupectValue(t, testKubes["xsite2"], false)

	expectHeartBeatConfiguration(t, testKubes["xsite1"], false, nil, nil)
	expectHeartBeatConfiguration(t, testKubes["xsite2"], true, hbInterval, hbTimeout)
}

func TestMultipleSiteDefinitions(t *testing.T) {
	// single cluster cross-site is enough.
	// Testing issue https://github.com/infinispan/infinispan-operator/issues/1834
	clientConfig := clientcmd.GetConfigFromFileOrDie(tutils.FindKubeConfig())
	kube := tutils.NewTestKubernetes(clientConfig.CurrentContext)
	namespace := fmt.Sprintf("%s-xsite2", tutils.Namespace)

	defer kube.CleanNamespaceAndLogOnPanic(t, namespace)

	infinispan := tutils.DefaultSpec(t, kube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = 3
		i.Spec.Service.Sites = &ispnv1.InfinispanSitesSpec{
			Local: ispnv1.InfinispanSitesLocalSpec{
				Name: "local",
				Expose: ispnv1.CrossSiteExposeSpec{
					Type: ispnv1.CrossSiteExposeTypeClusterIP,
				},
				MaxRelayNodes: 1,
				Discovery: &ispnv1.DiscoverySiteSpec{
					Memory: "500Mi",
					CPU:    "500m",
				},
			},
			Locations: []ispnv1.InfinispanSiteLocationSpec{
				{
					Name: "remote-site-1",
					URL:  "infinispan+xsite://fake-site-1.svc.local:7900",
				},
				{
					Name: "remote-site-2",
					URL:  "infinispan+xsite://fake-site-2.svc.local:7900",
				},
				{
					Name: "remote-site-3",
					URL:  "infinispan+xsite://fake-site-3.svc.local:7900",
				},
				{
					Name: "remote-site-4",
					URL:  "infinispan+xsite://fake-site-4.svc.local:7900",
				},
			},
		}
	})

	kube.CreateInfinispan(infinispan, namespace)
	kube.WaitForInfinispanPods(3, tutils.SinglePodTimeout, infinispan.Name, namespace)
	kube.WaitForInfinispanCondition(infinispan.Name, namespace, ispnv1.ConditionWellFormed)
	// cannot check for cross-site view since it won't be formed

	version := kube.GetStatefulSet(infinispan.Name, infinispan.Namespace).Status.ObservedGeneration

	time.Sleep(10 * time.Second)
	kube.WaitForInfinispanPods(3, tutils.SinglePodTimeout, infinispan.Name, namespace)
	kube.WaitForInfinispanCondition(infinispan.Name, namespace, ispnv1.ConditionWellFormed)

	otherVersion := kube.GetStatefulSet(infinispan.Name, infinispan.Namespace).Status.ObservedGeneration
	assert.Equal(t, version, otherVersion)
}

func TestBackupPodWithoutTLS(t *testing.T) {
	testBackupCrossSiteCache(t, false)
}

func TestBackupPodWithTLS(t *testing.T) {
	testBackupCrossSiteCache(t, true)
}

func testCrossSiteView(t *testing.T, isMultiCluster bool, explicitNodePort bool, schemeType ispnv1.CrossSiteSchemeType, exposeType ispnv1.CrossSiteExposeType, exposePort, podsPerSite int32, tlsMode TLSMode, tlsProtocol *ispnv1.TLSProtocol) {
	testName := tutils.TestName(t)
	testKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(tutils.FindKubeConfig())
	nodePort := int32(0)

	if isMultiCluster {
		for instance, testKube := range testKubes {
			testKube.context = fmt.Sprintf("kind-%s", instance)
			testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, instance)
			testKube.kube = tutils.NewTestKubernetes(testKube.context)
			clusterContextName := clientConfig.Contexts[testKube.context].Cluster
			apiServerUrl, err := url.Parse(clientConfig.Clusters[clusterContextName].Server)
			tutils.ExpectNoError(err)
			testKube.apiServer = apiServerUrl.Host
		}
		if schemeType == ispnv1.CrossSiteSchemeTypeKubernetes {
			defer testKubes["xsite1"].kube.DeleteSecret(crossSiteCertificateSecret("xsite2", testKubes["xsite1"].namespace, clientConfig, testKubes["xsite2"].context))
			defer testKubes["xsite2"].kube.DeleteSecret(crossSiteCertificateSecret("xsite1", testKubes["xsite2"].namespace, clientConfig, testKubes["xsite1"].context))

			testKubes["xsite1"].kube.CreateSecret(crossSiteCertificateSecret("xsite2", testKubes["xsite1"].namespace, clientConfig, testKubes["xsite2"].context))
			testKubes["xsite2"].kube.CreateSecret(crossSiteCertificateSecret("xsite1", testKubes["xsite2"].namespace, clientConfig, testKubes["xsite1"].context))
		} else if schemeType == ispnv1.CrossSiteSchemeTypeOpenShift {
			operatorNamespaceSite1 := constants.GetWithDefault(tutils.OperatorNamespace, testKubes["xsite1"].namespace)
			operatorNamespaceSite2 := constants.GetWithDefault(tutils.OperatorNamespace, testKubes["xsite2"].namespace)
			xsite1Token := getServiceAccountToken(operatorNamespaceSite1, testKubes["xsite1"].kube)
			xsite2Token := getServiceAccountToken(operatorNamespaceSite2, testKubes["xsite2"].kube)

			defer testKubes["xsite1"].kube.DeleteSecret(crossSiteTokenSecret("xsite2", testKubes["xsite1"].namespace, []byte("")))
			defer testKubes["xsite2"].kube.DeleteSecret(crossSiteTokenSecret("xsite1", testKubes["xsite2"].namespace, []byte("")))

			testKubes["xsite1"].kube.CreateSecret(crossSiteTokenSecret("xsite2", testKubes["xsite1"].namespace, xsite2Token))
			testKubes["xsite2"].kube.CreateSecret(crossSiteTokenSecret("xsite1", testKubes["xsite2"].namespace, xsite1Token))
		}

		// Find a free node port on both clusters and configure it in the xsite expose spec
		if exposeType == ispnv1.CrossSiteExposeTypeNodePort && explicitNodePort {
			usedPorts := testKubes["xsite1"].kube.GetUsedNodePorts()
			for k, v := range testKubes["xsite2"].kube.GetUsedNodePorts() {
				usedPorts[k] = v
			}
			// Obtain a NodePort within the range 30000-32767 that is not currently used by either cluster
			for i := int32(30000); i < 32768; i++ {
				if _, usedPort := usedPorts[i]; !usedPort {
					nodePort = i
					break
				}
			}
			fmt.Printf("Configuring xsite with NodePort=%d\n", nodePort)
		}

		testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite1", "xsite2", testKubes["xsite2"].namespace, exposeType, exposePort, nodePort)
		testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite2", "xsite1", testKubes["xsite1"].namespace, exposeType, exposePort, nodePort)

		testKubes["xsite1"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, testKubes["xsite2"].apiServer)
		testKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, testKubes["xsite1"].apiServer)
	} else {
		testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite1", "xsite2", "", exposeType, exposePort, nodePort)
		testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), podsPerSite, "xsite2", "xsite1", "", exposeType, exposePort, nodePort)
		for _, testKube := range testKubes {
			testKube.context = clientConfig.CurrentContext
			testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, "xsite2")
			testKube.kube = tutils.NewTestKubernetes(testKube.context)
		}
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
		transport, router, trust := tutils.CreateDefaultCrossSiteKeyAndTrustStore()

		for site := range testKubes {
			transportSecretName := fmt.Sprintf("%s-transport-tls-secret", site)
			routerSecretName := fmt.Sprintf("%s-router-tls-secret", site)
			trustSecretName := fmt.Sprintf("%s-trust-tls-secret", site)

			namespace := testKubes[site].namespace

			transportSecret := createTLSKeysStoreSecret(transport, transportSecretName, namespace, tutils.KeystorePassword, nil)
			routerSecret := createTLSKeysStoreSecret(router, routerSecretName, namespace, tutils.KeystorePassword, nil)
			trustSecret := createTLSTrustStoreSecret(trust, trustSecretName, namespace, tutils.TruststorePassword, nil)

			testKubes[site].kube.CreateSecret(transportSecret)
			testKubes[site].kube.CreateSecret(routerSecret)
			testKubes[site].kube.CreateSecret(trustSecret)

			if tutils.CleanupXSiteOnFinish {
				defer testKubes[site].kube.DeleteSecret(transportSecret)
				defer testKubes[site].kube.DeleteSecret(routerSecret)
				defer testKubes[site].kube.DeleteSecret(trustSecret)
			}
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
		keystore, truststore := tutils.CreateCrossSiteSingleKeyStoreAndTrustStore()
		for site := range testKubes {
			namespace := testKubes[site].namespace
			keyStoreFileName := "my-keystore.p12"
			keyStoreSecretName := fmt.Sprintf("%s-my-keystore-secret", site)
			trustStoreFileName := "my-truststore.p12"
			trustStoreSecretName := fmt.Sprintf("%s-my-truststore-secret", site)

			transportSecret := createTLSKeysStoreSecret(keystore, keyStoreSecretName, namespace, tutils.KeystorePassword, &keyStoreFileName)
			trustSecret := createTLSTrustStoreSecret(truststore, trustStoreSecretName, namespace, tutils.KeystorePassword, &trustStoreFileName)

			testKubes[site].kube.CreateSecret(transportSecret)
			testKubes[site].kube.CreateSecret(trustSecret)

			if tutils.CleanupXSiteOnFinish {
				defer testKubes[site].kube.DeleteSecret(transportSecret)
				defer testKubes[site].kube.DeleteSecret(trustSecret)
			}

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

	testKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	testKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

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

	if explicitNodePort {
		verifyNodePort := func(site *crossSiteKubernetes) {
			i := &site.crossSite
			service := &corev1.Service{}
			tutils.ExpectNoError(
				site.kube.Kubernetes.Client.Get(
					context.TODO(),
					types.NamespacedName{
						Namespace: i.Namespace,
						Name:      i.GetSiteServiceName(),
					},
					service),
			)
			assert.Equal(t, nodePort, service.Spec.Ports[0].NodePort)
		}
		verifyNodePort(testKubes["xsite1"])
		verifyNodePort(testKubes["xsite2"])
	}
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

func expectGossipRouterSupectValue(t *testing.T, siteKube *crossSiteKubernetes, expectValue bool) {
	deployment := siteKube.kube.WaitForDeployment(siteKube.crossSite.GetGossipRouterDeploymentName(), siteKube.namespace)
	args := strings.Join(deployment.Spec.Template.Spec.Containers[0].Args, " ")
	expect := fmt.Sprintf("-suspect %s", strconv.FormatBool(expectValue))
	assert.True(t, strings.Contains(args, expect), "Wrong '-suspect' argument. Expects=%s. Args=%s", strconv.FormatBool(expectValue), args)
}

func expectHeartBeatConfiguration(t *testing.T, siteKube *crossSiteKubernetes, enabled bool, interval, timeout *int64) {
	confgMap := siteKube.kube.GetConfigMap(siteKube.crossSite.GetConfigName(), siteKube.namespace)
	data := confgMap.Data["infinispan-admin.xml"]
	if enabled {
		assert.True(t, strings.Contains(data, fmt.Sprintf("heartbeat_interval=\"%s\"", strconv.FormatInt(*interval, 10))), "TUNNEL hearbeat configuration expected")
		assert.True(t, strings.Contains(data, fmt.Sprintf("heartbeat_timeout=\"%s\"", strconv.FormatInt(*timeout, 10))), "TUNNEL hearbeat configuration expected")
	} else {
		assert.False(t, strings.Contains(data, "heartbeat_interval"), "TUNNEL hearbeat configuration not expected")
		assert.False(t, strings.Contains(data, "heartbeat_timeout"), "TUNNEL hearbeat configuration not expected")
	}
}

func getServiceAccountToken(namespace string, k8s *tutils.TestKubernetes) []byte {
	response, err := corev1client.New(k8s.Kubernetes.RestClient).
		ServiceAccounts(namespace).
		CreateToken(
			context.TODO(),
			tutils.OperatorSAName,
			&authenticationv1.TokenRequest{},
			metav1.CreateOptions{},
		)
	tutils.ExpectNoError(err)
	if len(response.Status.Token) == 0 {
		panic(fmt.Errorf("failed to create token: no token in server response"))
	}
	return []byte(response.Status.Token)
}

func testBackupCrossSiteCache(t *testing.T, useTLS bool) {
	testName := tutils.TestName(t)
	testKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(tutils.FindKubeConfig())

	for instance, testKube := range testKubes {
		testKube.context = fmt.Sprintf("kind-%s", instance)
		testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, instance)
		testKube.kube = tutils.NewTestKubernetes(testKube.context)
		clusterContextName := clientConfig.Contexts[testKube.context].Cluster
		apiServerUrl, err := url.Parse(clientConfig.Clusters[clusterContextName].Server)
		tutils.ExpectNoError(err)
		testKube.apiServer = apiServerUrl.Host
	}

	// authentication for operator
	defer testKubes["xsite1"].kube.DeleteSecret(crossSiteCertificateSecret("xsite2", testKubes["xsite1"].namespace, clientConfig, testKubes["xsite2"].context))
	defer testKubes["xsite2"].kube.DeleteSecret(crossSiteCertificateSecret("xsite1", testKubes["xsite2"].namespace, clientConfig, testKubes["xsite1"].context))

	testKubes["xsite1"].kube.CreateSecret(crossSiteCertificateSecret("xsite2", testKubes["xsite1"].namespace, clientConfig, testKubes["xsite2"].context))
	testKubes["xsite2"].kube.CreateSecret(crossSiteCertificateSecret("xsite1", testKubes["xsite2"].namespace, clientConfig, testKubes["xsite1"].context))

	// create the cross-site spec
	testKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite1", "xsite2", testKubes["xsite2"].namespace, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 0)
	testKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(testName), 1, "xsite2", "xsite1", testKubes["xsite1"].namespace, ispnv1.CrossSiteExposeTypeLoadBalancer, 0, 0)

	testKubes["xsite1"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", ispnv1.CrossSiteSchemeTypeKubernetes, testKubes["xsite2"].apiServer)
	testKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", ispnv1.CrossSiteSchemeTypeKubernetes, testKubes["xsite1"].apiServer)

	defer testKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite1"].namespace)
	defer testKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(t, testKubes["xsite2"].namespace)

	testKubes["xsite1"].crossSite.Spec.Expose = &ispnv1.ExposeSpec{
		Type: ispnv1.ExposeTypeNodePort,
	}
	testKubes["xsite2"].crossSite.Spec.Expose = &ispnv1.ExposeSpec{
		Type: ispnv1.ExposeTypeNodePort,
	}

	if useTLS {
		transport, router, trust := tutils.CreateDefaultCrossSiteKeyAndTrustStore()

		for site := range testKubes {
			transportSecretName := fmt.Sprintf("%s-transport-tls-secret", site)
			routerSecretName := fmt.Sprintf("%s-router-tls-secret", site)
			trustSecretName := fmt.Sprintf("%s-trust-tls-secret", site)

			namespace := testKubes[site].namespace

			transportSecret := createTLSKeysStoreSecret(transport, transportSecretName, namespace, tutils.KeystorePassword, nil)
			routerSecret := createTLSKeysStoreSecret(router, routerSecretName, namespace, tutils.KeystorePassword, nil)
			trustSecret := createTLSTrustStoreSecret(trust, trustSecretName, namespace, tutils.TruststorePassword, nil)

			testKubes[site].kube.CreateSecret(transportSecret)
			testKubes[site].kube.CreateSecret(routerSecret)
			testKubes[site].kube.CreateSecret(trustSecret)

			if tutils.CleanupXSiteOnFinish {
				defer testKubes[site].kube.DeleteSecret(transportSecret)
				defer testKubes[site].kube.DeleteSecret(routerSecret)
				defer testKubes[site].kube.DeleteSecret(trustSecret)
			}
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
		}
	}

	// create and wait for cross-site connection
	testKubes["xsite1"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}
	testKubes["xsite2"].crossSite.ObjectMeta.Labels = map[string]string{"test-name": testName}

	testKubes["xsite1"].kube.CreateInfinispan(&testKubes["xsite1"].crossSite, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.CreateInfinispan(&testKubes["xsite2"].crossSite, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
	testKubes["xsite2"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace)

	testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	ispnXSite1 := testKubes["xsite1"].kube.WaitForInfinispanCondition(testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := testKubes["xsite2"].kube.WaitForInfinispanCondition(testKubes["xsite2"].crossSite.Name, testKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")

	//Populate the cluster with some data to backup
	versionManager := getVersionManager(t, testKubes["xsite1"].kube)
	client := tutils.HTTPClientForClusterWithVersionManager(&testKubes["xsite1"].crossSite, testKubes["xsite1"].kube, versionManager)
	cacheName := "xsiteCache"

	cache := tutils.NewCacheHelper(cacheName, client)
	config := "{\"distributed-cache\":{\"mode\":\"SYNC\",\"backups\":{\"xsite2\":{\"backup\":{\"strategy\":\"SYNC\"}}}}}"
	cache.Create(config, mime.ApplicationJson)
	cache.Populate(10)
	cache.AssertSize(10)

	// Test backup the cluster's content
	backupName := "backup"
	backupSpec := backupSpec(testName, backupName, testKubes["xsite1"].namespace, testKubes["xsite1"].crossSite.Name)
	testKubes["xsite1"].kube.Create(backupSpec)

	// Ensure the backup pod has joined the cluster
	testKubes["xsite1"].kube.WaitForValidBackupPhase(backupName, testKubes["xsite1"].namespace, ispnv2.BackupSucceeded)

	// Ensure that the backup pod has left the cluster, by checking a cluster pod's size
	testKubes["xsite1"].kube.WaitForInfinispanPods(int(1), tutils.SinglePodTimeout, testKubes["xsite1"].crossSite.Name, testKubes["xsite1"].namespace)
}

func backupSpec(testName, name, namespace, cluster string) *ispnv2.Backup {
	spec := &ispnv2.Backup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Backup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"test-name": testName},
		},
		Spec: ispnv2.BackupSpec{
			Cluster: cluster,
		},
	}
	spec.Default()
	return spec
}

func getVersionManager(t *testing.T, kube *tutils.TestKubernetes) *version.Manager {
	podList := kube.WaitForPods(1, tutils.SinglePodTimeout, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"control-plane": "controller-manager",
		})},
		nil,
	)
	assert.Equal(t, 1, len(podList.Items))
	envVars := podList.Items[0].Spec.Containers[0].Env
	index := kubernetes.GetEnvVarIndex(ispnv1.OperatorOperandVersionEnvVarName, &envVars)
	assert.GreaterOrEqual(t, index, 0)
	versionManager, err := version.ManagerFromJson(envVars[index].Value)
	tutils.ExpectNoError(err)
	return versionManager
}
