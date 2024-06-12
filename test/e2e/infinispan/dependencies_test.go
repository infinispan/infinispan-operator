package infinispan

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestExternalDependenciesHttp(t *testing.T) {
	if os.Getenv("NO_NGINX") != "" {
		t.Skip("Skipping test, no Nginx available.")
	}
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	webServerConfig := prepareWebServer()
	defer testKube.DeleteResource(tutils.Namespace, labels.SelectorFromSet(map[string]string{"app": tutils.WebServerName}), webServerConfig, tutils.SinglePodTimeout)

	namespace := tutils.Namespace
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Dependencies = &ispnv1.InfinispanExternalDependencies{
			Artifacts: []ispnv1.InfinispanExternalArtifacts{
				{Url: fmt.Sprintf("http://%s:%d/task01-1.0.0.jar", tutils.WebServerName, tutils.WebServerPortNumber)},
				{Url: fmt.Sprintf("http://%s:%d/task02-1.0.0.zip", tutils.WebServerName, tutils.WebServerPortNumber)},
			},
			InitContainer: ispnv1.InitDependenciesContainerSpec{
				Memory: "512Mi:64Mi",
				CPU:    "900m:100m",
			},
		}
	})

	// Create the cluster
	testKube.CreateInfinispan(spec, tutils.Namespace)
	pod := testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, namespace).Items[0]
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Assert that the initContainer has the specified re
	container := kube.GetInitContainer(provision.ExternalArtifactsDownloadInitContainer, &pod.Spec)
	assert.Equal(t, resource.MustParse("512Mi"), *container.Resources.Limits.Memory())
	assert.Equal(t, resource.MustParse("64Mi"), *container.Resources.Requests.Memory())
	assert.Equal(t, resource.MustParse("900m"), *container.Resources.Limits.Cpu())
	assert.Equal(t, resource.MustParse("100m"), *container.Resources.Requests.Cpu())

	client_ := tutils.HTTPClientForCluster(ispn, testKube)

	validateTaskExecution := func(task, param string, status int, result string) {
		url := fmt.Sprintf("rest/v2/tasks/%s?action=exec&param.name=%s", task, param)
		resp, err := client_.Post(url, "", nil)
		tutils.ExpectNoError(err)
		defer func(Body io.ReadCloser) {
			tutils.ExpectNoError(Body.Close())
		}(resp.Body)
		if resp.StatusCode != status {
			panic(fmt.Sprintf("Unexpected response code %d for the Server Task execution", resp.StatusCode))
		}
		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			tutils.ExpectNoError(err)
			if string(body) != result {
				panic(fmt.Sprintf("Unexpected task %s response '%s' from the Server Task", task, string(body)))
			}
		}
	}

	for _, task := range []string{"01", "02"} {
		validateTaskExecution("task-"+task, "World", http.StatusOK, "Hello World")
	}

	// Ensure that updates to InitContainer resources result in a StatefulSet rollout and the appropriate values are configured
	verifyStatefulSetUpdate(
		*ispn,
		func(ispn *ispnv1.Infinispan) {
			ispn.Spec.Dependencies.InitContainer.CPU = ""
			ispn.Spec.Dependencies.InitContainer.Memory = ""
		},
		func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
			testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
			container = kube.GetInitContainer(provision.ExternalArtifactsDownloadInitContainer, &ss.Spec.Template.Spec)
			assert.True(t, container.Resources.Limits.Memory().IsZero())
			assert.True(t, container.Resources.Requests.Memory().IsZero())
			assert.True(t, container.Resources.Limits.Cpu().IsZero())
			assert.True(t, container.Resources.Requests.Cpu().IsZero())
		},
	)

	var externalLibraryAddModify = func(ispn *ispnv1.Infinispan) {
		libs := &ispn.Spec.Dependencies.Artifacts
		*libs = append(*libs, ispnv1.InfinispanExternalArtifacts{Url: fmt.Sprintf("http://%s:%d/task03-1.0.0.tar.gz", tutils.WebServerName, tutils.WebServerPortNumber)})
	}
	var externalLibraryAddVerify = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
		validateTaskExecution("task-03", "World", http.StatusOK, "Hello World")
	}
	verifyStatefulSetUpdate(*ispn, externalLibraryAddModify, externalLibraryAddVerify)

	var externalLibraryHashModify = func(ispn *ispnv1.Infinispan) {
		for taskName, taskData := range webServerConfig.BinaryData {
			for artifactIndex, artifact := range ispn.Spec.Dependencies.Artifacts {
				if strings.Contains(artifact.Url, taskName) {
					ispn.Spec.Dependencies.Artifacts[artifactIndex].Hash = fmt.Sprintf("sha1:%s", hash.HashByte(taskData))
				}
			}
		}
	}

	var externalLibraryHashVerify = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
		for _, task := range []string{"01", "02", "03"} {
			validateTaskExecution("task-"+task, "World", http.StatusOK, "Hello World")
		}
	}

	verifyStatefulSetUpdate(*ispn, externalLibraryHashModify, externalLibraryHashVerify)

	var externalLibraryFailHashModify = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Dependencies.Artifacts[1].Hash = fmt.Sprintf("sha1:%s", "failhash")
	}

	tutils.ExpectNoError(testKube.UpdateInfinispan(ispn, func() {
		externalLibraryFailHashModify(ispn)
	}))

	podList := &corev1.PodList{}
	tutils.ExpectNoError(wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		err = testKube.Kubernetes.ResourcesList(ispn.Namespace, ispn.PodSelectorLabels(), podList, context.TODO())
		if err != nil {
			return false, nil
		}
		for _, pod := range podList.Items {
			if kube.InitContainerFailed(pod.Status.InitContainerStatuses) {
				return true, nil
			}

		}
		return false, nil
	}))

	var externalLibraryRemoveModify = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Dependencies = nil
	}
	var externalLibraryRemoveVerify = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
		for _, task := range []string{"01", "02", "03"} {
			validateTaskExecution("task-"+task, "", http.StatusBadRequest, "")
		}
	}
	verifyStatefulSetUpdate(*ispn, externalLibraryRemoveModify, externalLibraryRemoveVerify)
}

func prepareWebServer() *corev1.ConfigMap {
	webServerConfig := &corev1.ConfigMap{}
	testKube.LoadResourceFromYaml("../utils/data/external-libs-config.yaml", webServerConfig)
	webServerConfig.Namespace = tutils.Namespace
	testKube.Create(webServerConfig)

	webServerDeploymentConfig := tutils.WebServerDeployment(tutils.WebServerName, tutils.Namespace, webServerConfig.Name, tutils.WebServerRootFolder, tutils.WebServerImageName)
	tutils.ExpectNoError(controllerutil.SetControllerReference(webServerConfig, webServerDeploymentConfig, tutils.Scheme))
	testKube.Create(webServerDeploymentConfig)

	webServerService := tutils.WebServerService(tutils.WebServerName, tutils.Namespace)
	tutils.ExpectNoError(controllerutil.SetControllerReference(webServerConfig, webServerService, tutils.Scheme))
	testKube.Create(webServerService)

	testKube.WaitForPods(1, tutils.SinglePodTimeout, &client.ListOptions{Namespace: tutils.Namespace, LabelSelector: labels.SelectorFromSet(map[string]string{"app": tutils.WebServerName})}, nil)
	return webServerConfig
}
