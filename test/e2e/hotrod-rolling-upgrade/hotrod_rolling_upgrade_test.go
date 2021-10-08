package hotrod_rolling_upgrade

import (
	"bufio"
	"context"
	"fmt"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/test/e2e/utils"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
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

func cacheUrl(cacheName, hostAddr string) string {
	return fmt.Sprintf("%s/rest/v2/caches/%s", hostAddr, cacheName)
}

func cacheSizeUrl(cacheName, hostAddr string) string {
	return cacheUrl(cacheName, hostAddr) + "?action=size"
}

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
	command = exec.Command("go", "run", mainPath)

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
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, nil)
	initialize()
	// TODO This should always trigger a rolling upgrade since the operator uses aliases by default. Make it an environment variable to test upgrade from different versions
	go runOperatorNewProcess("quay.io/infinispan/server:13.0.0.Final-3")

	infinispan := &ispnv1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: tutils.Namespace,
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
	hostAddr, client := utils.HTTPClientAndHost(infinispan, testKube)

	// Create caches
	createCache("textCache", "text/plain", hostAddr, client)
	createCache("jsonCache", "application/json", hostAddr, client)
	createCache("javaCache", "application/x-java-object", hostAddr, client)
	createCache("indexedCache", "application/x-protostream", hostAddr, client)

	// Add data to some caches
	addData("textCache", hostAddr, entriesPerCache, client)
	addData("indexedCache", hostAddr, entriesPerCache, client)

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

	// Check data
	assert.Equal(t, entriesPerCache, cacheSize("textCache", hostAddr, client))
	assert.Equal(t, entriesPerCache, cacheSize("indexedCache", hostAddr, client))
	assert.Equal(t, 0, cacheSize("jsonCache", hostAddr, client))
	assert.Equal(t, 0, cacheSize("javaCache", hostAddr, client))
}

func cacheSize(cacheName, hostAddr string, client utils.HTTPClient) int {
	url := cacheSizeUrl(cacheName, hostAddr)
	resp, err := client.Get(url, nil)
	defer tutils.CloseHttpResponse(resp)
	tutils.ExpectNoError(err)
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Error getting cache size, status %d", resp.StatusCode))
	}
	body, err := ioutil.ReadAll(resp.Body)
	tutils.ExpectNoError(err)
	size, err := strconv.Atoi(string(body))
	tutils.ExpectNoError(err)
	return size
}

func createCache(cacheName, mediaType, hostAddr string, client utils.HTTPClient) {
	url := cacheUrl(cacheName, hostAddr)
	var config string
	if mediaType == "" {
		config = "{\"distributed-cache\":{\"mode\":\"SYNC\", \"remote-timeout\": 60000}}"
	} else {
		config = fmt.Sprintf("{\"distributed-cache\":{\"mode\":\"SYNC\",\"remote-timeout\": 60000,\"encoding\":{\"media-type\":\"%s\"}}}", mediaType)
	}
	resp, err := client.Post(url, config, map[string]string{"Content-Type": "application/json"})
	defer tutils.CloseHttpResponse(resp)
	tutils.ExpectNoError(err)
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Error creating cache, status %d", resp.StatusCode))
	}
}

// addData Populates a cache with bounded parallelism
func addData(cacheName, hostAddr string, entries int, client utils.HTTPClient) {
	write := func(key, value string) {
		url := cacheUrl(cacheName, hostAddr) + "/" + key
		resp, err := client.Post(url, value, nil)
		defer tutils.CloseHttpResponse(resp)
		tutils.ExpectNoError(err)
		if resp.StatusCode != http.StatusNoContent {
			panic(fmt.Sprintf("Error populating cache, status %d", resp.StatusCode))
		}
	}
	for i := 0; i < entries; i++ {
		data := strconv.Itoa(i)
		write(data, data)
	}

	fmt.Printf("Populated cache %s with %d entries", cacheName, entries)
}
