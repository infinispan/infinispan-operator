package xsite

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
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
			},
			Container: ispnv1.InfinispanContainerSpec{
				Memory: tutils.Memory,
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

func TestCrossSiteViewInternal(t *testing.T) {
	testCrossSiteView(t, false, "", ispnv1.CrossSiteExposeTypeClusterIP, 0)
}

func TestCrossSiteViewKubernetesNodePort(t *testing.T) {
	// Cross-Site between clusters will need to setup two instances of the Kind for Travis CI
	// Not be able to test on the separate OCP/OKD instance (probably with AWS/Azure LoadBalancer support only)
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeNodePort, 0)
}

func TestCrossSiteViewOpenshiftNodePort(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeNodePort, 0)
}

func TestCrossSiteViewKubernetesLoadBalancer(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeKubernetes, ispnv1.CrossSiteExposeTypeLoadBalancer, 0)
}

func TestCrossSiteViewOpenshiftLoadBalancer(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 0)
}

func TestCrossSiteViewLoadBalancerWithPort(t *testing.T) {
	testCrossSiteView(t, true, ispnv1.CrossSiteSchemeTypeOpenShift, ispnv1.CrossSiteExposeTypeLoadBalancer, 1443)
}

func testCrossSiteView(t *testing.T, isMultiCluster bool, schemeType ispnv1.CrossSiteSchemeType, exposeType ispnv1.CrossSiteExposeType, exposePort int32) {
	tesKubes := map[string]*crossSiteKubernetes{"xsite1": {}, "xsite2": {}}
	clientConfig := clientcmd.GetConfigFromFileOrDie(kube.FindKubeConfig())

	if isMultiCluster {
		for instance, testKube := range tesKubes {
			testKube.context = fmt.Sprintf("kind-%s", instance)
			testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, instance)
			testKube.kube = tutils.NewTestKubernetes(testKube.context)
			apiServerUrl, err := url.Parse(clientConfig.Clusters[testKube.context].Server)
			tutils.ExpectNoError(err)
			testKube.apiServer = apiServerUrl.Host
		}
		if schemeType == ispnv1.CrossSiteSchemeTypeKubernetes {
			tesKubes["xsite1"].kube.CreateSecret(crossSiteCertificateSecret("xsite2", tesKubes["xsite1"].namespace, clientConfig, tesKubes["xsite2"].context))
			tesKubes["xsite2"].kube.CreateSecret(crossSiteCertificateSecret("xsite1", tesKubes["xsite2"].namespace, clientConfig, tesKubes["xsite1"].context))

			defer tesKubes["xsite1"].kube.DeleteSecret(crossSiteCertificateSecret("xsite2", tesKubes["xsite1"].namespace, clientConfig, tesKubes["xsite2"].context))
			defer tesKubes["xsite2"].kube.DeleteSecret(crossSiteCertificateSecret("xsite1", tesKubes["xsite2"].namespace, clientConfig, tesKubes["xsite1"].context))
		} else if schemeType == ispnv1.CrossSiteSchemeTypeOpenShift {
			tokenSecretXsite1, err := kube.LookupServiceAccountTokenSecret("xsite1", tesKubes["xsite1"].namespace, tesKubes["xsite1"].kube.Kubernetes.Client, context.TODO())
			tutils.ExpectNoError(err)
			tokenSecretXsite2, err := kube.LookupServiceAccountTokenSecret("xsite2", tesKubes["xsite2"].namespace, tesKubes["xsite2"].kube.Kubernetes.Client, context.TODO())
			tutils.ExpectNoError(err)

			tesKubes["xsite1"].kube.CreateSecret(crossSiteTokenSecret("xsite2", tesKubes["xsite1"].namespace, tokenSecretXsite2.Data["token"]))
			tesKubes["xsite2"].kube.CreateSecret(crossSiteTokenSecret("xsite1", tesKubes["xsite2"].namespace, tokenSecretXsite1.Data["token"]))

			defer tesKubes["xsite1"].kube.DeleteSecret(crossSiteTokenSecret("xsite2", tesKubes["xsite1"].namespace, []byte("")))
			defer tesKubes["xsite2"].kube.DeleteSecret(crossSiteTokenSecret("xsite1", tesKubes["xsite2"].namespace, []byte("")))
		}
		tesKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), 2, "xsite1", "xsite2", tesKubes["xsite2"].namespace, exposeType, exposePort)
		tesKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), 1, "xsite2", "xsite1", tesKubes["xsite1"].namespace, exposeType, exposePort)

		tesKubes["xsite1"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, tesKubes["xsite2"].apiServer)
		tesKubes["xsite2"].crossSite.Spec.Service.Sites.Locations[0].URL = fmt.Sprintf("%s://%s", schemeType, tesKubes["xsite1"].apiServer)
	} else {
		tesKubes["xsite1"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), 2, "xsite1", "xsite2", "", exposeType, exposePort)
		tesKubes["xsite2"].crossSite = *crossSiteSpec(strcase.ToKebab(t.Name()), 1, "xsite2", "xsite1", "", exposeType, exposePort)
		for _, testKube := range tesKubes {
			testKube.context = clientConfig.CurrentContext
			testKube.namespace = fmt.Sprintf("%s-%s", tutils.Namespace, "xsite2")
			testKube.kube = tutils.NewTestKubernetes(testKube.context)
		}
	}

	tesKubes["xsite1"].crossSite.Labels = map[string]string{"test-name": t.Name()}
	tesKubes["xsite2"].crossSite.Labels = map[string]string{"test-name": t.Name()}

	tesKubes["xsite1"].kube.CreateInfinispan(&tesKubes["xsite1"].crossSite, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.CreateInfinispan(&tesKubes["xsite2"].crossSite, tesKubes["xsite2"].namespace)

	defer tesKubes["xsite1"].kube.CleanNamespaceAndLogOnPanic(tutils.Namespace, tesKubes["xsite1"].crossSite.Labels)
	defer tesKubes["xsite2"].kube.CleanNamespaceAndLogOnPanic(tutils.Namespace, tesKubes["xsite2"].crossSite.Labels)

	tesKubes["xsite1"].kube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace)
	tesKubes["xsite2"].kube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace)

	tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionWellFormed)
	tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionWellFormed)

	ispnXSite1 := tesKubes["xsite1"].kube.WaitForInfinispanCondition(tesKubes["xsite1"].crossSite.Name, tesKubes["xsite1"].namespace, ispnv1.ConditionCrossSiteViewFormed)
	ispnXSite2 := tesKubes["xsite2"].kube.WaitForInfinispanCondition(tesKubes["xsite2"].crossSite.Name, tesKubes["xsite2"].namespace, ispnv1.ConditionCrossSiteViewFormed)

	assert.Contains(t, ispnXSite1.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
	assert.Contains(t, ispnXSite2.GetCondition(ispnv1.ConditionCrossSiteViewFormed).Message, "xsite1,xsite2")
}
