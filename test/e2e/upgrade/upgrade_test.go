package upgrade

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
)

const (
	CrdPath = "../../../deploy/crds/"
)

var (
	testKube         = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
	upgradeStateFlow = []v1.ConditionType{v1.ConditionUpgrade, v1.ConditionStopping, v1.ConditionWellFormed}
)

// TODO remove and replace with DefaultSpec once Batch PR has been merged as it removes this logic from main_test
var MinimalSpec = v1.Infinispan{
	TypeMeta: tutils.InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: tutils.DefaultClusterName,
	},
	Spec: v1.InfinispanSpec{
		Replicas: 1,
	},
}

func TestGracefulShutdown(t *testing.T) {
	namespace := tutils.Namespace
	testKube.NewNamespace(namespace)
	// Utilise the sha256 of the current Infinispan image to trick the operator into thinking a upgrade is required
	oldImage := getDockerImageSha()
	tutils.ExpectNoError(os.Setenv("DEFAULT_IMAGE", oldImage))
	stopCh := testKube.InstallAndRunOperator(namespace, CrdPath, false)
	ispn := MinimalSpec.DeepCopy()
	ispn.Name = strcase.ToKebab(t.Name())
	testKube.CreateInfinispan(ispn, namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, namespace)
	assertPodImage(oldImage, ispn)
	close(stopCh)

	// Unset DEFAULT_IMAGE and install the current operator version
	os.Unsetenv("DEFAULT_IMAGE")
	stopCh = testKube.InstallAndRunOperator(namespace, CrdPath, false)
	defer close(stopCh)
	for _, state := range upgradeStateFlow {
		testKube.WaitForInfinispanCondition(ispn.Name, namespace, state)
	}
	assertPodImage(tutils.ExpectedImage, ispn)
}

func assertPodImage(image string, ispn *v1.Infinispan) {
	pods := &corev1.PodList{}
	err := testKube.Kubernetes.ResourcesList(ispn.Namespace, ispnctrl.PodLabels(ispn.Name), pods)
	tutils.ExpectNoError(err)
	for _, pod := range pods.Items {
		if pod.Spec.Containers[0].Image != image {
			tutils.ExpectNoError(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, image))
		}
	}
}

func getDockerImageSha() string {
	name := "sha-pod"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tutils.Namespace,
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: pointer.Int64Ptr(1000600000),
			},
			Containers: []corev1.Container{{
				Image: constants.DefaultOperandImageOpenJDK,
				Name:  name,
			}},
		},
	}
	testKube.Create(pod)
	defer testKube.Delete(pod)

	err := wait.Poll(tutils.DefaultPollPeriod, tutils.MaxWaitTimeout, func() (done bool, err error) {
		err = testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: tutils.Namespace}, pod)
		if err != nil && errors.IsNotFound(err) {
			return false, nil
		}
		if len(pod.Status.ContainerStatuses) > 0 {
			return pod.Status.ContainerStatuses[0].ImageID != "", nil
		}
		return false, nil
	})
	tutils.ExpectNoError(err)
	return strings.TrimPrefix(pod.Status.ContainerStatuses[0].ImageID, "docker-pullable://")
}
