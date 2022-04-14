package infinispan

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
)

func TestPodDegradationAfterOOM(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	//Creating Infinispan cluster
	ispn := tutils.DefaultSpec(t, testKube)
	ispn.Spec.Replicas = 2
	ispn.Spec.Container.Memory = "256Mi"
	ispn.Spec.Service.Container.EphemeralStorage = false

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	//Creating cache
	cacheName := "failover-cache"
	template := `<replicated-cache name ="` + cacheName + `"><encoding media-type="text/plain"/></replicated-cache>`
	veryLongValue := GenerateStringWithCharset(100000)
	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client_)
	cacheHelper.Create(template, mime.ApplicationXml)

	client_.Quiet(true)
	//Generate tons of random entries
	for key := 1; key < 50000; key++ {
		strKey := strconv.Itoa(key)
		if err := cacheHelper.CacheClient.Put(strKey, veryLongValue, mime.TextPlain); err != nil {
			fmt.Printf("ERROR for key=%d, Description=%s\n", key, err)
			break
		}
	}
	client_.Quiet(false)

	//Check if all pods are running, and they are not degraded
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	//Verify whether the pod restarted for an OOM exception
	hasOOMhappened := false
	podList := &corev1.PodList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(tutils.Namespace, ispn.PodSelectorLabels(), podList, context.TODO()))

	for _, pod := range podList.Items {
		status := pod.Status.ContainerStatuses

	out:
		for _, containerStatuses := range status {
			if containerStatuses.LastTerminationState.Terminated != nil {
				terminatedPod := containerStatuses.LastTerminationState.Terminated

				if terminatedPod.Reason == "OOMKilled" {
					hasOOMhappened = true
					fmt.Printf("ExitCode='%d' Reason='%s' Message='%s'\n", terminatedPod.ExitCode, terminatedPod.Reason, terminatedPod.Message)
					break out
				}
			}
		}
	}

	if kube.AreAllPodsReady(podList) && hasOOMhappened {
		fmt.Println("All pods are ready")
	} else if kube.AreAllPodsReady(podList) && !hasOOMhappened {
		panic("Test finished without an OutOfMemory occurred")
	} else {
		panic("One of the pods is degraded")
	}
}

func GenerateStringWithCharset(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}
