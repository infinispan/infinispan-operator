package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	cconsts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources/config"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
var serviceAccountKube = tutils.NewTestKubernetes("")

var log = logf.Log.WithName("main_test")

var DefaultSpec = ispnv1.Infinispan{
	TypeMeta: tutils.InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: tutils.DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Type: ispnv1.ServiceTypeDataGrid,
		},
		Container: ispnv1.InfinispanContainerSpec{
			CPU:    tutils.CPU,
			Memory: tutils.Memory,
		},
		Replicas: 1,
		Expose:   exposeServiceSpec(),
		Image:    pointer.StringPtr("quay.io/infinispan/server:12.0.0.CR1"),
	},
}

var MinimalSpec = ispnv1.Infinispan{
	TypeMeta: tutils.InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: tutils.DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Replicas: 2,
	},
}

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

// Test operator and cluster version upgrade flow
func TestOperatorUpgrade(t *testing.T) {
	spec := DefaultSpec.DeepCopy()
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Namespace = tutils.Namespace
	switch tutils.OperatorUpgradeStage {
	case tutils.OperatorUpgradeStageFrom:
		testKube.CreateInfinispan(spec, tutils.Namespace)
		testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	case tutils.OperatorUpgradeStageTo:
		if tutils.CleanupInfinispan == "TRUE" {
			defer testKube.DeleteInfinispan(spec, tutils.SinglePodTimeout)
		}
		for _, state := range tutils.OperatorUpgradeStateFlow {
			testKube.WaitForInfinispanCondition(strcase.ToKebab(t.Name()), tutils.Namespace, state)
		}

		// Validates that all pods are running with desired image
		pods := &corev1.PodList{}
		err := testKube.Kubernetes.ResourcesList(tutils.Namespace, ispnctrl.PodLabels(spec.Name), pods)
		tutils.ExpectNoError(err)
		for _, pod := range pods.Items {
			if pod.Spec.Containers[0].Image != tutils.ExpectedImage {
				tutils.ExpectNoError(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, tutils.ExpectedImage))
			}
		}
	default:
		t.Skipf("Operator upgrade stage '%s' is unsupported or disabled", tutils.OperatorUpgradeStage)
	}
}

// Test if single node working correctly
func TestNodeStartup(t *testing.T) {
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	spec.Annotations = make(map[string]string)
	spec.Annotations[v1.TargetLabels] = "my-svc-label"
	spec.Labels = make(map[string]string)
	spec.Labels["my-svc-label"] = "my-svc-value"
	os.Setenv(v1.OperatorTargetLabelsEnvVarName, "{\"operator-svc-label\":\"operator-svc-value\"}")
	defer os.Unsetenv(v1.OperatorTargetLabelsEnvVarName)
	spec.Annotations[v1.PodTargetLabels] = "my-pod-label"
	spec.Labels["my-svc-label"] = "my-svc-value"
	spec.Labels["my-pod-label"] = "my-pod-value"
	os.Setenv(v1.OperatorPodTargetLabelsEnvVarName, "{\"operator-pod-label\":\"operator-pod-value\"}")
	defer os.Unsetenv(v1.OperatorPodTargetLabelsEnvVarName)
	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(spec, tutils.SinglePodTimeout)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := ispnv1.Infinispan{}
	pod := corev1.Pod{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: spec.Name, Namespace: tutils.Namespace}, &ispn))
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: spec.Name + "-0", Namespace: tutils.Namespace}, &pod))
	if pod.Labels["my-pod-label"] != ispn.Labels["my-pod-label"] ||
		pod.Labels["operator-pod-label"] != "operator-pod-value" ||
		ispn.Labels["operator-pod-label"] != "operator-pod-value" {
		panic("Labels haven't been propagated to pods")
	}
	svcList := &corev1.ServiceList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(ispn.ObjectMeta.Namespace, labels.Set{}, svcList))
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

// Run some functions for testing rights not covered by integration tests
func TestRolesSynthetic(t *testing.T) {
	_, _, err := config.GetNodePortServiceHostPort(0, serviceAccountKube.Kubernetes, log)
	tutils.ExpectNoError(err)

	_, err = kube.FindStorageClass("not-present-storage-class", serviceAccountKube.Kubernetes.Client)
	if !errors.IsNotFound(err) {
		tutils.ExpectNoError(err)
	}
}

// Test if single node with n ephemeral storage
func TestNodeWithEphemeralStorage(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Service.Container = &ispnv1.InfinispanServiceContainerSpec{EphemeralStorage: true}
	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(spec, tutils.SinglePodTimeout)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	// Making sure no PVCs were created
	pvcs := &corev1.PersistentVolumeClaimList{}
	err := testKube.Kubernetes.ResourcesList(spec.Namespace, ispnctrl.PodLabels(spec.Name), pvcs)
	tutils.ExpectNoError(err)
	if len(pvcs.Items) > 0 {
		tutils.ExpectNoError(fmt.Errorf("persistent volume claims were found (count = %d) but not expected for ephemeral storage configuration", len(pvcs.Items)))
	}
}

// Test if the cluster is working correctly
func TestClusterFormation(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Replicas = 2
	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(spec, tutils.SinglePodTimeout)
	testKube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

}

// Test if the cluster is working correctly
func TestClusterFormationWithTLS(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Replicas = 2
	spec.Spec.Security = ispnv1.InfinispanSecurity{
		EndpointEncryption: &ispnv1.EndpointEncryption{
			Type:           "secret",
			CertSecretName: "secret-certs",
		},
	}
	// Create secret
	testKube.CreateSecret(&encryptionSecret, tutils.Namespace)
	defer testKube.DeleteSecret(&encryptionSecret)
	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(spec, tutils.SinglePodTimeout)
	testKube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
}

// Test if spec.container.cpu update is handled
func TestContainerCPUUpdateWithTwoReplicas(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.CPU = "550m"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		limit := resource.MustParse("550m")
		request := resource.MustParse("275m")
		if limit.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"]) != 0 ||
			request.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"]) != 0 {
			panic("CPU field not updated")
		}
	}
	spec := MinimalSpec.DeepCopy()
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

// Test if spec.container.memory update is handled
func TestContainerMemoryUpdate(t *testing.T) {
	t.Parallel()
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.Memory = "256Mi"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		if resource.MustParse("256Mi") != ss.Spec.Template.Spec.Containers[0].Resources.Requests["memory"] {
			panic("Memory field not updated")
		}
	}
	spec := DefaultSpec.DeepCopy()
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestContainerJavaOptsUpdate(t *testing.T) {
	t.Parallel()
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
	spec := DefaultSpec.DeepCopy()
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

// Test if single node working correctly
func genericTestForContainerUpdated(ispn ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*appsv1.StatefulSet)) {
	testKube.CreateInfinispan(&ispn, tutils.Namespace)
	defer testKube.DeleteInfinispan(&ispn, tutils.SinglePodTimeout)
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	// Get the latest version of the Infinispan resource
	err := testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ispn)
	if err != nil {
		panic(err.Error())
	}

	// Get the associate statefulset
	ss := appsv1.StatefulSet{}

	// Get the current generation
	err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ss)
	if err != nil {
		panic(err.Error())
	}
	generation := ss.Status.ObservedGeneration

	// Change the Infinispan spec
	modifier(&ispn)
	// Workaround for OpenShift local test (clear GVK on decode in the client)
	ispn.TypeMeta = tutils.InfinispanTypeMeta
	err = testKube.Kubernetes.Client.Update(context.TODO(), &ispn)
	if err != nil {
		panic(err.Error())
	}

	// Wait for a new generation to appear
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ss)
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	if err != nil {
		panic(err.Error())
	}

	// Wait that current and update revisions match
	// this ensure that the rolling upgrade completes
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ss)
		return ss.Status.CurrentRevision == ss.Status.UpdateRevision, nil
	})
	if err != nil {
		panic(err.Error())
	}

	// Check that the update has been propagated
	verifier(&ss)
}

func TestCacheService(t *testing.T) {
	t.Parallel()
	testCacheService(t.Name(), nil)
}

func TestCacheServiceNativeImage(t *testing.T) {
	t.Parallel()
	testCacheService(t.Name(), pointer.StringPtr(tutils.NativeImageName))
}

func testCacheService(testName string, imageName *string) {
	spec := DefaultSpec.DeepCopy()
	spec.Name = strcase.ToKebab(testName)
	spec.Spec.Image = imageName
	spec.Spec.Service.Type = ispnv1.ServiceTypeCache
	spec.Spec.Expose = exposeServiceSpec()

	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(spec, tutils.SinglePodTimeout)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	user := cconsts.DefaultDeveloperUser
	password, err := users.PasswordFromSecret(user, spec.GetSecretName(), tutils.Namespace, testKube.Kubernetes)
	tutils.ExpectNoError(err)

	ispn := ispnv1.Infinispan{}
	testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)

	protocol := testKube.GetSchemaForRest(&ispn)
	routeName := fmt.Sprintf("%s-external", spec.Name)
	client := tutils.NewHTTPClient(user, password, protocol)
	hostAddr := testKube.WaitForExternalService(routeName, tutils.RouteTimeout, client, tutils.Namespace)

	cacheName := "default"

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client)
	actual := getViaRoute(keyURL, client)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

// TestPermanentCache creates a permanent cache the stop/start
// the cluster and checks that the cache is still there
func TestPermanentCache(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	usr := cconsts.DefaultDeveloperUser
	cacheName := "test"
	// Define function for the generic stop/start test procedure
	var createPermanentCache = func(ispn *ispnv1.Infinispan) {
		protocol := testKube.GetSchemaForRest(ispn)
		pass, err := users.PasswordFromSecret(usr, ispn.GetSecretName(), tutils.Namespace, testKube.Kubernetes)
		tutils.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)

		client := tutils.NewHTTPClient(usr, pass, protocol)
		hostAddr := testKube.WaitForExternalService(routeName, tutils.RouteTimeout, client, tutils.Namespace)
		createCache(cacheName, hostAddr, "PERMANENT", client)
	}

	var usePermanentCache = func(ispn *ispnv1.Infinispan) {
		pass, err := users.PasswordFromSecret(usr, ispn.GetSecretName(), tutils.Namespace, testKube.Kubernetes)
		tutils.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		protocol := testKube.GetSchemaForRest(ispn)

		client := tutils.NewHTTPClient(usr, pass, protocol)
		hostAddr := testKube.WaitForExternalService(routeName, tutils.RouteTimeout, client, tutils.Namespace)
		key := "test"
		value := "test-operator"
		keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
		putViaRoute(keyURL, value, client)
		actual := getViaRoute(keyURL, client)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		deleteCache(cacheName, hostAddr, client)
	}

	genericTestForGracefulShutdown(name, createPermanentCache, usePermanentCache)
}

// TestCheckDataSurviveToShutdown creates a cache with file-store the stop/start
// the cluster and checks that the cache and the data are still there
func TestCheckDataSurviveToShutdown(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	usr := cconsts.DefaultDeveloperUser
	cacheName := "test"
	template := `<infinispan><cache-container><distributed-cache name ="` + cacheName +
		`"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>`
	key := "test"
	value := "test-operator"

	// Define function for the generic stop/start test procedure
	var createCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		pass, err := users.PasswordFromSecret(usr, ispn.GetSecretName(), tutils.Namespace, testKube.Kubernetes)
		tutils.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		protocol := testKube.GetSchemaForRest(ispn)

		client := tutils.NewHTTPClient(usr, pass, protocol)
		hostAddr := testKube.WaitForExternalService(routeName, tutils.RouteTimeout, client, tutils.Namespace)
		createCacheWithXMLTemplate(cacheName, hostAddr, template, client)
		keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
		putViaRoute(keyURL, value, client)
	}

	var useCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		pass, err := users.PasswordFromSecret(usr, ispn.GetSecretName(), tutils.Namespace, testKube.Kubernetes)
		tutils.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		schema := testKube.GetSchemaForRest(ispn)
		client := tutils.NewHTTPClient(usr, pass, schema)
		hostAddr := testKube.WaitForExternalService(routeName, tutils.RouteTimeout, client, tutils.Namespace)
		keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
		actual := getViaRoute(keyURL, client)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		deleteCache(cacheName, hostAddr, client)
	}

	genericTestForGracefulShutdown(name, createCacheWithFileStore, useCacheWithFileStore)
}

func genericTestForGracefulShutdown(clusterName string, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan)) {
	// Create a resource without passing any config
	// Register it
	spec := DefaultSpec.DeepCopy()
	spec.Name = clusterName
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(spec, tutils.SinglePodTimeout)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := ispnv1.Infinispan{}
	testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)
	// Do something that needs to be permanent
	modifier(&ispn)

	// Delete the cluster
	testKube.GracefulShutdownInfinispan(spec, tutils.SinglePodTimeout)
	testKube.GracefulRestartInfinispan(spec, 1, tutils.SinglePodTimeout)

	// Do something that checks that permanent changes are there again
	verifier(&ispn)
}

func TestExternalService(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	usr := cconsts.DefaultDeveloperUser

	// Create a resource without passing any config
	spec := ispnv1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    tutils.CPU,
				Memory: tutils.Memory,
			},
			Replicas: 1,
			Expose:   exposeServiceSpec(),
		},
	}

	// Register it
	testKube.CreateInfinispan(&spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(&spec, tutils.SinglePodTimeout)

	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	pass, err := users.PasswordFromSecret(usr, spec.GetSecretName(), tutils.Namespace, testKube.Kubernetes)
	tutils.ExpectNoError(err)

	routeName := fmt.Sprintf("%s-external", name)
	ispn := ispnv1.Infinispan{}
	testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)
	schema := testKube.GetSchemaForRest(&ispn)

	client := tutils.NewHTTPClient(usr, pass, schema)
	hostAddr := testKube.WaitForExternalService(routeName, tutils.RouteTimeout, client, tutils.Namespace)

	cacheName := "test"
	createCache(cacheName, hostAddr, "", client)
	defer deleteCache(cacheName, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client)
	actual := getViaRoute(keyURL, client)

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
	exposeServiceType := cconsts.GetEnvWithDefault("EXPOSE_SERVICE_TYPE", string(ispnv1.ExposeTypeNodePort))
	switch exposeServiceType {
	case string(ispnv1.ExposeTypeNodePort):
		return ispnv1.ExposeTypeNodePort
	case string(ispnv1.ExposeTypeLoadBalancer):
		return ispnv1.ExposeTypeLoadBalancer
	default:
		panic(fmt.Errorf("unknown service type %s", exposeServiceType))
	}
}

// TestExternalServiceWithAuth starts a cluster and checks application
// and management connection with authentication
func TestExternalServiceWithAuth(t *testing.T) {
	t.Parallel()
	usr := "connectorusr"
	pass := "connectorpass"
	newpass := "connectornewpass"
	identities := users.CreateIdentitiesFor(usr, pass)
	identitiesYaml, err := yaml.Marshal(identities)
	tutils.ExpectNoError(err)

	// Create secret with application credentials
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test"},
		Type:       "Opaque",
		StringData: map[string]string{cconsts.ServerIdentitiesFilename: string(identitiesYaml)},
	}
	testKube.CreateSecret(&secret, tutils.Namespace)
	defer testKube.DeleteSecret(&secret)

	name := strcase.ToKebab(t.Name())

	// Create Infinispan
	spec := ispnv1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Security: ispnv1.InfinispanSecurity{EndpointSecretName: "conn-secret-test"},
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    tutils.CPU,
				Memory: tutils.Memory,
			},
			Replicas: 1,
			Expose:   exposeServiceSpec(),
		},
	}
	testKube.CreateInfinispan(&spec, tutils.Namespace)
	defer testKube.DeleteInfinispan(&spec, tutils.SinglePodTimeout)

	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := ispnv1.Infinispan{}
	testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)

	schema := testKube.GetSchemaForRest(&ispn)
	testAuthentication(schema, name, usr, pass)
	// Update the auth credentials.
	identities = users.CreateIdentitiesFor(usr, newpass)
	identitiesYaml, err = yaml.Marshal(identities)
	tutils.ExpectNoError(err)

	// Create secret with application credentials
	secret1 := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test-1"},
		Type:       "Opaque",
		StringData: map[string]string{cconsts.ServerIdentitiesFilename: string(identitiesYaml)},
	}
	testKube.CreateSecret(&secret1, tutils.Namespace)
	defer testKube.DeleteSecret(&secret1)

	// Get the associate statefulset
	ss := appsv1.StatefulSet{}

	// Get the current generation
	err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
	if err != nil {
		panic(err.Error())
	}
	generation := ss.Status.ObservedGeneration

	// Update the resource
	err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &spec)
	if err != nil {
		panic(err.Error())
	}

	spec.Spec.Security.EndpointSecretName = "conn-secret-test-1"
	// Workaround for OpenShift local test (clear GVK on decode in the client)
	spec.TypeMeta = tutils.InfinispanTypeMeta
	err = testKube.Kubernetes.Client.Update(context.TODO(), &spec)
	if err != nil {
		panic(fmt.Errorf(err.Error()))
	}

	// Wait for a new generation to appear
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	if err != nil {
		panic(err.Error())
	}
	// Sleep for a while to be sure that the old pods are gone
	// The restart is ongoing and it would that more than 10 sec
	// so we're not introducing any delay
	time.Sleep(10 * time.Second)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testAuthentication(schema, name, usr, newpass)
}

func testAuthentication(schema, name, usr, pass string) {
	routeName := fmt.Sprintf("%s-external", name)
	badClient := tutils.NewHTTPClient("badUser", "badPass", schema)
	client := tutils.NewHTTPClient(usr, pass, schema)
	hostAddr := testKube.WaitForExternalService(routeName, tutils.RouteTimeout, client, tutils.Namespace)

	cacheName := "test"
	createCacheBadCreds(cacheName, hostAddr, badClient)
	createCache(cacheName, hostAddr, "", client)
	defer deleteCache(cacheName, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client)
	actual := getViaRoute(keyURL, client)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func cacheURL(cacheName, hostAddr string) string {
	return fmt.Sprintf("%v/rest/v2/caches/%s", hostAddr, cacheName)
}

type httpError struct {
	status int
}

func (e *httpError) Error() string {
	return fmt.Sprintf("unexpected response %v", e.status)
}

func createCache(cacheName, hostAddr string, flags string, client tutils.HTTPClient) {
	httpURL := cacheURL(cacheName, hostAddr)
	headers := map[string]string{}
	if flags != "" {
		headers["Flags"] = flags
	}
	resp, err := client.Post(httpURL, "", headers)
	tutils.ExpectNoError(err)
	if resp.StatusCode != http.StatusOK {
		panic(httpError{resp.StatusCode})
	}
}

func createCacheBadCreds(cacheName, hostAddr string, client tutils.HTTPClient) {
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
	createCache(cacheName, hostAddr, "", client)
}

func createCacheWithXMLTemplate(cacheName, hostAddr, template string, client tutils.HTTPClient) {
	httpURL := cacheURL(cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpURL)
	headers := map[string]string{
		"Content-Type": "application/xml;charset=UTF-8",
	}
	resp, err := client.Post(httpURL, template, headers)
	if err != nil {
		panic(err.Error())
	}
	// Accept all the 2xx success codes
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		throwHTTPError(resp)
	}
}

func deleteCache(cacheName, hostAddr string, client tutils.HTTPClient) {
	httpURL := cacheURL(cacheName, hostAddr)
	resp, err := client.Delete(httpURL, nil)
	tutils.ExpectNoError(err)

	if resp.StatusCode != http.StatusOK {
		panic(httpError{resp.StatusCode})
	}
}

func getViaRoute(url string, client tutils.HTTPClient) string {
	resp, err := client.Get(url, nil)
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

func putViaRoute(url, value string, client tutils.HTTPClient) {
	headers := map[string]string{
		"Content-Type": "text/plain",
	}
	resp, err := client.Post(url, value, headers)
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
