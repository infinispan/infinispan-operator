package hotrod_rolling_upgrade

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	mainPath        = "../../../main.go"
	crdPath         = "../../../config/crd/bases/"
	entriesPerCache = 100
	numPods         = 2
	clusterName     = "demo-cluster"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

// command Pointer to the spawned operator process
var command *exec.Cmd

// initialize Prepares namespaces and CRD installations
func initialize() {
	testKube.NewNamespace(tutils.Namespace)
}

// killOperator Gracefully shutdown the operator and its subprocesses
func killOperator() {
	err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	if err != nil {
		panic(err)
	}
}

func runOperatorSameProcess() context.CancelFunc {
	return testKube.RunOperator(tutils.Namespace, crdPath)
}

// runOperatorNewProcess Starts the operator in a new Process, as it's not possible to start it after stopping in the
//same process due to several internal concurrency issues like https://github.com/kubernetes-sigs/controller-runtime/issues/1346
func runOperatorNewProcess(image string) {
	command = exec.Command("go", "run", mainPath, "operator")

	// Sets the gid since "go run" spawns a subprocess
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	command.Env = os.Environ()
	command.Env = append(command.Env, "RELATED_IMAGE_OPENJDK="+image)
	command.Env = append(command.Env, "OSDK_FORCE_RUN_MODE=local")
	command.Env = append(command.Env, "WATCH_NAMESPACE="+tutils.Namespace)

	stdoutPipe, _ := command.StdoutPipe()
	command.Stderr = command.Stdout
	ch := make(chan struct{})
	scanner := bufio.NewScanner(stdoutPipe)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Println(line)
		}
		ch <- struct{}{}
	}()
	if err := command.Start(); err != nil {
		panic(err)
	}
	<-ch
	if err := command.Wait(); err != nil {
		println("Operator stopped")
	}
	<-ch
}

func TestMain(t *testing.M) {
	code := t.Run()
	os.Exit(code)
}

func TestRollingUpgrade(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	initialize()
	// TODO This should always trigger a rolling upgrade since the operator uses aliases by default. Make it an environment variable to test upgrade from different versions
	go runOperatorNewProcess("quay.io/infinispan/server")

	infinispan := &ispnv1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": tutils.TestName(t)},
		},
		Spec: ispnv1.InfinispanSpec{
			Replicas: int32(numPods),
			Service: ispnv1.InfinispanServiceSpec{
				Type: ispnv1.ServiceTypeDataGrid,
			},
			Expose: &ispnv1.ExposeSpec{
				Type:     ispnv1.ExposeTypeNodePort,
				NodePort: 30000,
			},
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    "1000m",
				Memory: "1Gi",
			},
			Upgrades: &ispnv1.InfinispanUpgradesSpec{
				Type: ispnv1.UpgradeTypeHotRodRolling,
			},
		},
	}
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(numPods, tutils.SinglePodTimeout, clusterName, tutils.Namespace)
	testKube.WaitForInfinispanCondition(clusterName, tutils.Namespace, ispnv1.ConditionWellFormed)
	client := tutils.HTTPClientForCluster(infinispan, testKube)

	// Create caches
	createCache("textCache", mime.TextPlain, client)
	createCache("jsonCache", mime.ApplicationJson, client)
	createCache("javaCache", mime.ApplicationJavaObject, client)
	createCache("indexedCache", mime.ApplicationProtostream, client)

	// Add data to some caches
	addData("textCache", entriesPerCache, client)
	addData("indexedCache", entriesPerCache, client)

	// Kill the operator
	killOperator()

	// Restart the operator
	runOperatorSameProcess()

	// Check migration
	currentStatefulSetName := clusterName
	newStatefulSetName := clusterName + "-1"

	testKube.WaitForStateFulSet(newStatefulSetName, tutils.Namespace)
	testKube.WaitForStateFulSetRemoval(currentStatefulSetName, tutils.Namespace)
	testKube.WaitForInfinispanPodsCreatedBy(numPods, tutils.SinglePodTimeout, newStatefulSetName, tutils.Namespace)
	testKube.WaitForInfinispanPodsCreatedBy(0, tutils.SinglePodTimeout, currentStatefulSetName, tutils.Namespace)

	if !tutils.CheckExternalAddress(client) {
		panic("Error contacting server")
	}

	// Check data
	assert.Equal(t, entriesPerCache, cacheSize("textCache", client))
	assert.Equal(t, entriesPerCache, cacheSize("indexedCache", client))
	assert.Equal(t, 0, cacheSize("jsonCache", client))
	assert.Equal(t, 0, cacheSize("javaCache", client))
}

func cacheSize(cacheName string, client tutils.HTTPClient) int {
	return tutils.NewCacheHelper(cacheName, client).Size()
}

func createCache(cacheName string, encoding mime.MimeType, client tutils.HTTPClient) {
	config := fmt.Sprintf("{\"distributed-cache\":{\"mode\":\"SYNC\",\"remote-timeout\": 60000,\"encoding\":{\"media-type\":\"%s\"}}}", encoding)
	tutils.NewCacheHelper(cacheName, client).Create(config, mime.ApplicationJson)
}

// addData Populates a cache with bounded parallelism
func addData(cacheName string, entries int, client tutils.HTTPClient) {
	cache := tutils.NewCacheHelper(cacheName, client)
	for i := 0; i < entries; i++ {
		data := strconv.Itoa(i)
		cache.Put(data, data, mime.TextPlain)
	}
	fmt.Printf("Populated cache %s with %d entries", cacheName, entries)
}
