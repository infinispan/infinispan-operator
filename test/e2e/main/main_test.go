package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	cconsts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	comutil "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	tconst "github.com/infinispan/infinispan-operator/test/e2e/constants"
	testutil "github.com/infinispan/infinispan-operator/test/e2e/util"
	routev1 "github.com/openshift/api/route/v1"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

var kubernetes = testutil.NewTestKubernetes()
var cluster = util.NewCluster(kubernetes.Kubernetes)

var DefaultSpec = ispnv1.Infinispan{
	TypeMeta: tconst.InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: tconst.DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Type: ispnv1.ServiceTypeDataGrid,
		},
		Container: ispnv1.InfinispanContainerSpec{
			CPU:    tconst.CPU,
			Memory: tconst.Memory,
		},
		Replicas: 1,
		Expose:   exposeServiceSpec(),
	},
}

var MinimalSpec = ispnv1.Infinispan{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "infinispan.org/v1",
		Kind:       "Infinispan",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: tconst.DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Replicas: 2,
	},
}

func TestMain(m *testing.M) {
	namespace := strings.ToLower(tconst.Namespace)
	if "TRUE" == tconst.RunLocalOperator {
		if "TRUE" != tconst.RunSaOperator && tconst.OperatorUpgradeStage != tconst.OperatorUpgradeStageTo {
			kubernetes.DeleteNamespace(namespace)
			kubernetes.DeleteCRD("infinispans.infinispan.org")
			kubernetes.DeleteCRD("caches.infinispan.org")
			kubernetes.NewNamespace(namespace)
		}
		kubernetes.InstallRBAC(namespace, "../../../deploy/")
		kubernetes.InstallCRD("../../../deploy/crds/")
		stopCh := kubernetes.RunOperator(namespace)
		code := m.Run()
		close(stopCh)
		os.Exit(code)
	} else {
		code := m.Run()
		os.Exit(code)
	}
}

// Test if single node working correctly
func TestNodeStartup(t *testing.T) {
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	spec.Annotations = make(map[string]string)
	spec.Annotations[ispnv1.TargetLabels] = "my-svc-label"
	spec.Labels = make(map[string]string)
	spec.Labels["my-svc-label"] = "my-svc-value"
	os.Setenv(ispnv1.OperatorTargetLabelsEnvVarName, "{\"operator-svc-label\":\"operator-svc-value\"}")
	defer os.Unsetenv(ispnv1.OperatorTargetLabelsEnvVarName)
	spec.Annotations[ispnv1.PodTargetLabels] = "my-pod-label"
	spec.Labels["my-svc-label"] = "my-svc-value"
	spec.Labels["my-pod-label"] = "my-pod-value"
	os.Setenv(ispnv1.OperatorPodTargetLabelsEnvVarName, "{\"operator-pod-label\":\"operator-pod-value\"}")
	defer os.Unsetenv(ispnv1.OperatorPodTargetLabelsEnvVarName)
	name := "test-node-startup"
	spec.ObjectMeta.Name = name
	// Register it
	kubernetes.CreateInfinispan(spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)
	kubernetes.WaitForInfinispanPods(1, tconst.SinglePodTimeout, spec.Name, spec.Namespace)
	ispn := ispnv1.Infinispan{}
	pod := corev1.Pod{}
	testutil.ExpectNoError(kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: spec.Name, Namespace: tconst.Namespace}, &ispn))
	testutil.ExpectNoError(kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: spec.Name + "-0", Namespace: tconst.Namespace}, &pod))
	if pod.Labels["my-pod-label"] != ispn.Labels["my-pod-label"] ||
		pod.Labels["operator-pod-label"] != "operator-pod-value" ||
		ispn.Labels["operator-pod-label"] != "operator-pod-value" {
		panic("Labels haven't been propagated to pods")
	}

	svcList := &corev1.ServiceList{}
	testutil.ExpectNoError(kubernetes.Kubernetes.ResourcesList(ispn.ObjectMeta.Name, ispn.ObjectMeta.Namespace, "", svcList))
	if len(svcList.Items) == 0 {
		panic("No services found for cluster")
	}
	for _, svc := range svcList.Items {
		if svc.Labels["my-svc-label"] != ispn.Labels["my-svc-label"] ||
			svc.Labels["operator-svc-label"] != "operator-svc-value" ||
			ispn.Labels["operator-svc-label"] != "operator-svc-value" {
			panic("Labels haven't been propagated to services")
		}
	}
}

// Test operator and cluster version upgrade flow
func TestOperatorUpgrade(t *testing.T) {
	name := "test-operator-upgrade"
	spec := DefaultSpec.DeepCopy()
	spec.ObjectMeta.Name = name
	switch tconst.OperatorUpgradeStage {
	case tconst.OperatorUpgradeStageFrom:
		kubernetes.CreateInfinispan(spec, tconst.Namespace)
		kubernetes.WaitForInfinispanPods(1, tconst.SinglePodTimeout, spec.Name, spec.Namespace)
	case tconst.OperatorUpgradeStageTo:
		defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)
		for _, state := range tconst.OperatorUpgradeStateFlow {
			kubernetes.WaitForInfinispanCondition(name, tconst.Namespace, state)
		}
	default:
		t.Skipf("Operator upgrade stage '%s' is unsupported or disabled", tconst.OperatorUpgradeStage)
	}
}

// Test if the cluster is working correctly
func TestClusterFormation(t *testing.T) {
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := "test-cluster-formation"
	spec.ObjectMeta.Name = name
	spec.Spec.Replicas = 2
	// Register it
	kubernetes.CreateInfinispan(spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)
	kubernetes.WaitForInfinispanPods(2, tconst.SinglePodTimeout, spec.Name, spec.Namespace)
}

// Test if the cluster is working correctly
func TestClusterFormationWithTLS(t *testing.T) {
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := "test-cluster-formation"
	spec.ObjectMeta.Name = name
	spec.Spec.Replicas = 2
	spec.Spec.Security = ispnv1.InfinispanSecurity{
		EndpointEncryption: &ispnv1.EndpointEncryption{
			Type:           ispnv1.CertificateSourceTypeSecretLowCase,
			CertSecretName: "secret-certs",
		},
	}
	// Create secret
	kubernetes.CreateSecret(&encryptionSecret, tconst.Namespace)
	defer kubernetes.DeleteSecret(&encryptionSecret)
	// Register it
	kubernetes.CreateInfinispan(spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)
	kubernetes.WaitForInfinispanPods(2, tconst.SinglePodTimeout, spec.Name, spec.Namespace)
}

// Test if spec.container.cpu update is handled
func TestContainerCPUUpdateWithTwoReplicas(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.CPU = "250m"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		limit := resource.MustParse("250m")
		request := resource.MustParse("125m")
		if limit.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"]) != 0 ||
			request.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"]) != 0 {
			panic("CPU field not updated")
		}
	}
	genericTestForContainerUpdated(MinimalSpec, modifier, verifier)
}

// Test if spec.container.memory update is handled
func TestContainerMemoryUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.Memory = "256Mi"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		if resource.MustParse("256Mi") != ss.Spec.Template.Spec.Containers[0].Resources.Requests["memory"] {
			panic("Memory field not updated")
		}
	}
	genericTestForContainerUpdated(DefaultSpec, modifier, verifier)
}

func TestContainerJavaOptsUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.ExtraJvmOpts = "-XX:NativeMemoryTracking=summary"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		env := ss.Spec.Template.Spec.Containers[0].Env
		for _, value := range env {
			if value.Name == "JAVA_OPTIONS" {
				if value.Value != "-XX:NativeMemoryTracking=summary" {
					panic("JAVA_OPTIONS not updated")
				} else {
					return
				}
			}
		}
		panic("JAVA_OPTIONS not updated")
	}
	genericTestForContainerUpdated(DefaultSpec, modifier, verifier)
}

// Test if single node working correctly
func genericTestForContainerUpdated(ispn ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*appsv1.StatefulSet)) {
	spec := ispn.DeepCopy()
	name := "test-node-startup"
	spec.ObjectMeta.Name = name
	kubernetes.CreateInfinispan(spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)
	kubernetes.WaitForInfinispanPods(int(ispn.Spec.Replicas), tconst.SinglePodTimeout, spec.Name, spec.Namespace)
	// Get the latest version of the Infinispan resource
	err := kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, spec)
	if err != nil {
		panic(err.Error())
	}

	// Get the associate statefulset
	ss := appsv1.StatefulSet{}

	// Get the current generation
	err = kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
	if err != nil {
		panic(err.Error())
	}
	generation := ss.Status.ObservedGeneration

	// Change the Infinispan spec
	modifier(spec)
	// Workaround for OpenShift local test (clear GVK on decode in the client)
	spec.TypeMeta = tconst.InfinispanTypeMeta
	err = kubernetes.Kubernetes.Client.Update(context.TODO(), spec)
	if err != nil {
		panic(err.Error())
	}

	// Wait for a new generation to appear
	err = wait.Poll(tconst.DefaultPollPeriod, tconst.SinglePodTimeout, func() (done bool, err error) {
		kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	if err != nil {
		panic(err.Error())
	}

	// Wait that current and update revisions match
	// this ensure that the rolling upgrade completes
	err = wait.Poll(tconst.DefaultPollPeriod, tconst.SinglePodTimeout, func() (done bool, err error) {
		kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
		return ss.Status.CurrentRevision == ss.Status.UpdateRevision, nil
	})
	if err != nil {
		panic(err.Error())
	}

	// Check that the update has been propagated
	verifier(&ss)
}

func TestCacheService(t *testing.T) {
	spec := DefaultSpec.DeepCopy()
	name := "test-cache-service"

	spec.ObjectMeta.Name = name
	spec.Spec.Service.Type = ispnv1.ServiceTypeCache
	spec.Spec.Expose = exposeServiceSpec()

	kubernetes.CreateInfinispan(spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)
	kubernetes.WaitForInfinispanPods(1, tconst.SinglePodTimeout, spec.Name, spec.Namespace)

	user := cconsts.DefaultDeveloperUser
	password, err := cluster.Kubernetes.GetPassword(user, spec.GetSecretName(), tconst.Namespace)
	testutil.ExpectNoError(err)

	ispn := ispnv1.Infinispan{}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)

	protocol := getSchemaForRest(&ispn)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	hostAddr := kubernetes.WaitForExternalService(ispn.GetServiceExternalName(), tconst.Namespace, spec.GetExposeType(), tconst.RouteTimeout, client, user, password, protocol)

	cacheName := "default"
	waitForCacheToBeCreated(cacheURL(protocol, cacheName, hostAddr), client, user, password)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(protocol, cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, user, password)
	actual := getViaRoute(keyURL, client, user, password)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

// TestPermanentCache creates a permanent cache the stop/start
// the cluster and checks that the cache is still there
func TestPermanentCache(t *testing.T) {
	name := "test-permanent-cache"
	usr := cconsts.DefaultDeveloperUser
	cacheName := "test"
	// Define function for the generic stop/start test procedure
	var createPermanentCache = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.Kubernetes.GetPassword(usr, ispn.GetSecretName(), tconst.Namespace)
		testutil.ExpectNoError(err)
		protocol := getSchemaForRest(ispn)

		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(ispn.GetServiceExternalName(), tconst.Namespace, ispn.GetExposeType(), tconst.RouteTimeout, client, usr, pass, protocol)
		createCache(protocol, cacheName, usr, pass, hostAddr, "PERMANENT", client)
	}

	var usePermanentCache = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.Kubernetes.GetPassword(usr, ispn.GetSecretName(), tconst.Namespace)
		testutil.ExpectNoError(err)
		protocol := getSchemaForRest(ispn)

		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(ispn.GetServiceExternalName(), tconst.Namespace, ispn.GetExposeType(), tconst.RouteTimeout, client, usr, pass, protocol)
		key := "test"
		value := "test-operator"
		keyURL := fmt.Sprintf("%v/%v", cacheURL(protocol, cacheName, hostAddr), key)
		putViaRoute(keyURL, value, client, usr, pass)
		actual := getViaRoute(keyURL, client, usr, pass)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		deleteCache(getSchemaForRest(ispn), cacheName, usr, pass, hostAddr, client)
	}

	genericTestForGracefulShutdown(name, createPermanentCache, usePermanentCache)
}

// TestPermanentCache creates a cache with file-store the stop/start
// the cluster and checks that the cache and the data are still there
func TestCheckDataSurviveToShutdown(t *testing.T) {
	name := "test-data-shutdown-cache"
	usr := cconsts.DefaultDeveloperUser
	cacheName := "test"
	template := `<infinispan><cache-container><distributed-cache name ="` + cacheName +
		`"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>`
	key := "test"
	value := "test-operator"

	// Define function for the generic stop/start test procedure
	var createCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.Kubernetes.GetPassword(usr, ispn.GetSecretName(), tconst.Namespace)
		testutil.ExpectNoError(err)
		protocol := getSchemaForRest(ispn)

		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(ispn.GetServiceExternalName(), tconst.Namespace, ispn.GetExposeType(), tconst.RouteTimeout, client, usr, pass, protocol)
		createCacheWithXMLTemplate(getSchemaForRest(ispn), cacheName, usr, pass, hostAddr, template, client)
		keyURL := fmt.Sprintf("%v/%v", cacheURL(protocol, cacheName, hostAddr), key)
		putViaRoute(keyURL, value, client, usr, pass)
	}

	var useCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.Kubernetes.GetPassword(usr, ispn.GetSecretName(), tconst.Namespace)
		testutil.ExpectNoError(err)
		schema := getSchemaForRest(ispn)
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(ispn.GetServiceExternalName(), tconst.Namespace, ispn.GetExposeType(), tconst.RouteTimeout, client, usr, pass, schema)
		keyURL := fmt.Sprintf("%v/%v", cacheURL(schema, cacheName, hostAddr), key)
		actual := getViaRoute(keyURL, client, usr, pass)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		deleteCache(schema, cacheName, usr, pass, hostAddr, client)
	}

	genericTestForGracefulShutdown(name, createCacheWithFileStore, useCacheWithFileStore)
}

func genericTestForGracefulShutdown(clusterName string, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan)) {
	// Create a resource without passing any config
	// Register it
	spec := DefaultSpec.DeepCopy()
	spec.ObjectMeta.Name = clusterName
	kubernetes.CreateInfinispan(spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(spec.Name, tconst.Namespace, tconst.SinglePodTimeout)
	kubernetes.WaitForInfinispanPods(1, tconst.SinglePodTimeout, spec.Name, spec.Namespace)

	ispn := ispnv1.Infinispan{}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)
	// Do something that needs to be permanent
	modifier(&ispn)

	// Delete the cluster
	kubernetes.GracefulShutdownInfinispan(spec, tconst.SinglePodTimeout)
	kubernetes.GracefulRestartInfinispan(spec, 1, tconst.SinglePodTimeout)

	// Do something that checks that permanent changes are there again
	verifier(&ispn)
}

func TestExternalService(t *testing.T) {
	name := "test-external-service"
	usr := cconsts.DefaultDeveloperUser

	// Create a resource without passing any config
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    tconst.CPU,
				Memory: tconst.Memory,
			},
			Replicas: 1,
			Expose:   exposeServiceSpec(),
		},
	}

	// Register it
	kubernetes.CreateInfinispan(&spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)

	kubernetes.WaitForPods("app=infinispan-pod", 1, tconst.SinglePodTimeout, tconst.Namespace)

	pass, err := cluster.Kubernetes.GetPassword(usr, spec.GetSecretName(), tconst.Namespace)
	testutil.ExpectNoError(err)

	ispn := ispnv1.Infinispan{}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)
	schema := getSchemaForRest(&ispn)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	hostAddr := kubernetes.WaitForExternalService(ispn.GetServiceExternalName(), tconst.Namespace, ispn.GetExposeType(), tconst.RouteTimeout, client, usr, pass, schema)

	cacheName := "test"
	createCache(schema, cacheName, usr, pass, hostAddr, "", client)
	defer deleteCache(schema, cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(schema, cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, usr, pass)
	actual := getViaRoute(keyURL, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func exposeServiceSpec() *ispnv1.ExposeSpec {
	return &ispnv1.ExposeSpec{
		Type: exposeServiceType(),
	}
}

func exposeServiceType() ispnv1.ExposeType {
	exposeServiceType := comutil.GetEnvWithDefault("EXPOSE_SERVICE_TYPE", "NodePort")
	switch exposeServiceType {
	case string(ispnv1.ExposeTypeNodePort):
		return ispnv1.ExposeTypeNodePort
	case string(ispnv1.ExposeTypeLoadBalancer):
		return ispnv1.ExposeTypeLoadBalancer
	case string(ispnv1.ExposeTypeRoute):
		okRoute, err := kubernetes.Kubernetes.IsGroupVersionSupported(routev1.GroupVersion.String(), "Route")
		if err == nil && okRoute {
			return ispnv1.ExposeTypeRoute
		}
		panic(fmt.Errorf("expose type Route is not supported on the platform: %w", err))
	default:
		panic(fmt.Errorf("unknown service type %s", exposeServiceType))
	}
}

// TestExternalServiceWithAuth starts a cluster and checks application
// and management connection with authentication
func TestExternalServiceWithAuth(t *testing.T) {
	usr := "connectorusr"
	pass := "connectorpass"
	newpass := "connectornewpass"
	identities := util.CreateIdentitiesFor(usr, pass)
	identitiesYaml, err := yaml.Marshal(identities)
	testutil.ExpectNoError(err)

	// Create secret with application credentials
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test"},
		Type:       "Opaque",
		StringData: map[string]string{"identities.yaml": string(identitiesYaml)},
	}
	kubernetes.CreateSecret(&secret, tconst.Namespace)
	defer kubernetes.DeleteSecret(&secret)

	name := "text-external-service-with-auth"

	// Create Infinispan
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Security: ispnv1.InfinispanSecurity{EndpointSecretName: "conn-secret-test"},
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    tconst.CPU,
				Memory: tconst.Memory,
			},
			Replicas: 1,
			Expose:   exposeServiceSpec(),
		},
	}
	kubernetes.CreateInfinispan(&spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)

	kubernetes.WaitForPods("app=infinispan-pod", 1, tconst.SinglePodTimeout, tconst.Namespace)

	ispn := ispnv1.Infinispan{}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)

	schema := getSchemaForRest(&ispn)
	testAuthentication(schema, ispn.GetServiceExternalName(), ispn.GetExposeType(), usr, pass)
	// Update the auth credentials.
	identities = util.CreateIdentitiesFor(usr, newpass)
	identitiesYaml, err = yaml.Marshal(identities)
	testutil.ExpectNoError(err)

	// Create secret with application credentials
	secret1 := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test-1"},
		Type:       "Opaque",
		StringData: map[string]string{"identities.yaml": string(identitiesYaml)},
	}
	kubernetes.CreateSecret(&secret1, tconst.Namespace)
	defer kubernetes.DeleteSecret(&secret1)

	// Get the associate statefulset
	ss := appsv1.StatefulSet{}

	// Get the current generation
	err = kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
	if err != nil {
		panic(err.Error())
	}
	generation := ss.Status.ObservedGeneration

	// Update the resource
	err = kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &spec)
	if err != nil {
		panic(err.Error())
	}

	spec.Spec.Security.EndpointSecretName = "conn-secret-test-1"
	// Workaround for OpenShift local test (clear GVK on decode in the client)
	spec.TypeMeta = tconst.InfinispanTypeMeta
	err = kubernetes.Kubernetes.Client.Update(context.TODO(), &spec)
	if err != nil {
		panic(fmt.Errorf(err.Error()))
	}

	// Wait for a new generation to appear
	err = wait.Poll(tconst.DefaultPollPeriod, tconst.SinglePodTimeout, func() (done bool, err error) {
		kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	if err != nil {
		panic(err.Error())
	}
	// Sleep for a while to be sure that the old pods are gone
	// The restart is ongoing and it would that more than 10 sec
	// so we're not introducing any delay
	time.Sleep(10 * time.Second)
	kubernetes.WaitForPods("app=infinispan-pod", 1, tconst.SinglePodTimeout, tconst.Namespace)
	testAuthentication(schema, ispn.GetServiceExternalName(), spec.GetExposeType(), usr, newpass)
}

func testAuthentication(schema, routeName string, exposeType ispnv1.ExposeType, usr, pass string) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	hostAddr := kubernetes.WaitForExternalService(routeName, tconst.Namespace, exposeType, tconst.RouteTimeout, client, usr, pass, schema)

	cacheName := "test"
	createCacheBadCreds(schema, cacheName, "badUser", "badPass", hostAddr, client)
	createCache(schema, cacheName, usr, pass, hostAddr, "", client)
	defer deleteCache(schema, cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(schema, cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, usr, pass)
	actual := getViaRoute(keyURL, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func cacheURL(schema, cacheName, hostAddr string) string {
	return fmt.Sprintf(schema+"://%v/rest/v2/caches/%s", hostAddr, cacheName)
}

type httpError struct {
	status int
}

func (e *httpError) Error() string {
	return fmt.Sprintf("unexpected response %v", e.status)
}

func createCache(schema, cacheName, usr, pass, hostAddr string, flags string, client *http.Client) {
	httpURL := cacheURL(schema, cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpURL)
	httpEmpty(httpURL, "POST", usr, pass, flags, client)
}

func createCacheBadCreds(schema, cacheName, usr, pass, hostAddr string, client *http.Client) {
	defer func() {
		data := recover()
		if data == nil {
			panic("createCacheBadCred should fail, but it doesn't")
		}
		err := data.(httpError)
		if err.status != http.StatusUnauthorized {
			panic(err)
		}
	}()
	createCache(schema, cacheName, usr, pass, hostAddr, "", client)
}

func createCacheWithXMLTemplate(schema, cacheName, user, pass, hostAddr, template string, client *http.Client) {
	httpURL := cacheURL(schema, cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpURL)
	body := bytes.NewBuffer([]byte(template))
	req, err := http.NewRequest("POST", httpURL, body)
	req.Header.Set("Content-Type", "application/xml;charset=UTF-8")
	req.SetBasicAuth(user, pass)
	fmt.Printf("Put request via route: %v\n", req)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	// Accept all the 2xx success codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		throwHTTPError(resp)
	}
}

func deleteCache(schema, cacheName, usr, pass, hostAddr string, client *http.Client) {
	httpURL := cacheURL(schema, cacheName, hostAddr)
	fmt.Printf("Delete cache: %v\n", httpURL)
	httpEmpty(httpURL, "DELETE", usr, pass, "", client)
}

func httpEmpty(httpURL string, method string, usr string, pass string, flags string, client *http.Client) {
	req, err := http.NewRequest(method, httpURL, nil)
	if flags != "" {
		req.Header.Set("flags", flags)
	}
	testutil.ExpectNoError(err)

	req.SetBasicAuth(usr, pass)
	resp, err := client.Do(req)
	testutil.ExpectNoError(err)

	if resp.StatusCode != http.StatusOK {
		panic(httpError{resp.StatusCode})
	}
}

func getViaRoute(url string, client *http.Client, user string, pass string) string {
	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		throwHTTPError(resp)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err.Error())
	}
	return string(bodyBytes)
}

func putViaRoute(url string, value string, client *http.Client, user string, pass string) {
	body := bytes.NewBuffer([]byte(value))
	req, err := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", "text/plain")
	req.SetBasicAuth(user, pass)
	fmt.Printf("Put request via route: %v\n", req)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	if resp.StatusCode != http.StatusNoContent {
		throwHTTPError(resp)
	}
}

func waitForCacheToBeCreated(url string, client *http.Client, user string, pass string) {
	err := wait.Poll(tconst.DefaultPollPeriod, tconst.MaxWaitTimeout, func() (done bool, err error) {
		req, err := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(user, pass)
		fmt.Printf("Wait for cache to be created: %v\n", req)
		resp, err := client.Do(req)
		if err != nil {
			return false, err
		}
		return resp.StatusCode == http.StatusOK, nil
	})
	testutil.ExpectNoError(err)
}

func throwHTTPError(resp *http.Response) {
	errorBytes, _ := ioutil.ReadAll(resp.Body)
	panic(fmt.Errorf("unexpected HTTP status code (%d): %s", resp.StatusCode, string(errorBytes)))
}

func getSchemaForRest(ispn *ispnv1.Infinispan) string {
	curr := ispnv1.Infinispan{}
	// Wait for the operator to populate Infinispan CR data
	err := wait.Poll(tconst.DefaultPollPeriod, tconst.SinglePodTimeout, func() (done bool, err error) {
		kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &curr)
		return len(curr.Status.Conditions) > 0, nil
	})
	testutil.ExpectNoError(err)
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &curr)
	if curr.IsEncryptionCertSourceDefined() {
		return "https"
	}
	return "http"
}
