package e2e

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
		kubernetes.InstallRBAC(namespace, "../../deploy/")
		kubernetes.InstallCRD("../../deploy/crds/")
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
	name := "test-node-startup"
	spec.ObjectMeta.Name = name
	// Register it
	kubernetes.CreateInfinispan(spec, tconst.Namespace)
	defer kubernetes.DeleteInfinispan(name, tconst.Namespace, tconst.SinglePodTimeout)
	waitForPodsOrFail(spec, 1)
}

// Test operator and cluster version upgrade flow
func TestOperatorUpgrade(t *testing.T) {
	name := "test-operator-upgrade"
	spec := DefaultSpec.DeepCopy()
	spec.ObjectMeta.Name = name
	switch tconst.OperatorUpgradeStage {
	case tconst.OperatorUpgradeStageFrom:
		kubernetes.CreateInfinispan(spec, tconst.Namespace)
		waitForPodsOrFail(spec, 1)
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
	waitForPodsOrFail(spec, 2)
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
	waitForPodsOrFail(spec, 2)
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
	waitForPodsOrFail(spec, int(ispn.Spec.Replicas))
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
	waitForPodsOrFail(spec, 1)

	user := cconsts.DefaultDeveloperUser
	password, err := cluster.Kubernetes.GetPassword(user, spec.GetSecretName(), tconst.Namespace)
	testutil.ExpectNoError(err)

	ispn := ispnv1.Infinispan{}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)

	protocol := getSchemaForRest(&ispn)
	routeName := fmt.Sprintf("%s-external", name)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	hostAddr := kubernetes.WaitForExternalService(routeName, tconst.RouteTimeout, client, user, password, protocol, tconst.Namespace)

	cacheName := "default"

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
		routeName := fmt.Sprintf("%s-external", name)
		protocol := getSchemaForRest(ispn)

		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(routeName, tconst.RouteTimeout, client, usr, pass, protocol, tconst.Namespace)
		createCache(protocol, cacheName, usr, pass, hostAddr, "PERMANENT", client)
	}

	var usePermanentCache = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.Kubernetes.GetPassword(usr, ispn.GetSecretName(), tconst.Namespace)
		testutil.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		protocol := getSchemaForRest(ispn)

		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(routeName, tconst.RouteTimeout, client, usr, pass, protocol, tconst.Namespace)
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
		routeName := fmt.Sprintf("%s-external", name)
		protocol := getSchemaForRest(ispn)

		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(routeName, tconst.RouteTimeout, client, usr, pass, protocol, tconst.Namespace)
		createCacheWithXMLTemplate(getSchemaForRest(ispn), cacheName, usr, pass, hostAddr, template, client)
		keyURL := fmt.Sprintf("%v/%v", cacheURL(protocol, cacheName, hostAddr), key)
		putViaRoute(keyURL, value, client, usr, pass)
	}

	var useCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.Kubernetes.GetPassword(usr, ispn.GetSecretName(), tconst.Namespace)
		testutil.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		schema := getSchemaForRest(ispn)
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client := &http.Client{Transport: tr}
		hostAddr := kubernetes.WaitForExternalService(routeName, tconst.RouteTimeout, client, usr, pass, schema, tconst.Namespace)
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
	waitForPodsOrFail(spec, 1)

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

func waitForPodsOrFail(spec *ispnv1.Infinispan, num int) {
	// Wait that "num" pods are up
	kubernetes.WaitForPods("app=infinispan-pod", num, tconst.SinglePodTimeout, tconst.Namespace)
	ispn := ispnv1.Infinispan{}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)
	protocol := getSchemaForRest(&ispn)
	pods := kubernetes.GetPods("app=infinispan-pod", tconst.Namespace)
	podName := pods[0].Name

	pass, err := cluster.Kubernetes.GetPassword(cconsts.DefaultOperatorUser, spec.GetSecretName(), tconst.Namespace)
	testutil.ExpectNoError(err)

	expectedClusterSize := num
	// Check that the cluster size is num querying the first pod
	var lastErr error
	err = wait.Poll(time.Second, tconst.TestTimeout, func() (done bool, err error) {
		value, err := cluster.GetClusterSize(cconsts.DefaultOperatorUser, pass, podName, tconst.Namespace, protocol)
		if err != nil {
			lastErr = err
			return false, nil
		}
		if value > expectedClusterSize {
			return true, fmt.Errorf("more than expected nodes in cluster (expected=%v, actual=%v)", expectedClusterSize, value)
		}

		return value == num, nil
	})

	if err == wait.ErrWaitTimeout && lastErr != nil {
		err = fmt.Errorf("timed out waiting for the condition, last error: %v", lastErr)
	}

	testutil.ExpectNoError(err)
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

	routeName := fmt.Sprintf("%s-external", name)
	ispn := ispnv1.Infinispan{}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)
	schema := getSchemaForRest(&ispn)

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	hostAddr := kubernetes.WaitForExternalService(routeName, tconst.RouteTimeout, client, usr, pass, schema, tconst.Namespace)

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
	case "NodePort":
		return ispnv1.ExposeTypeNodePort
	case "LoadBalancer":
		return ispnv1.ExposeTypeLoadBalancer
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
	testAuthentication(schema, name, usr, pass)
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
	testAuthentication(schema, name, usr, newpass)
}

func testAuthentication(schema, name, usr, pass string) {
	routeName := fmt.Sprintf("%s-external", name)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	hostAddr := kubernetes.WaitForExternalService(routeName, tconst.RouteTimeout, client, usr, pass, schema, tconst.Namespace)

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
	if err != nil {
		panic(err.Error())
	}
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &curr)
	if curr.IsEncryptionCertSourceDefined() {
		return "https"
	}
	return "http"
}
