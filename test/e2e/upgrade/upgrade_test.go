package upgrade

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	sha := tutils.ImageSha
	if sha != "" {
		return sha
	}
	// If the INFINISPAN_IMAGE_SHA env variable has not been set, attempt to retreive
	cmd := exec.Command("docker", "inspect", "'--format='{{index .RepoDigests 0}}'", constants.DefaultOperandImageOpenJDK)
	stdout, err := cmd.Output()

	if err != nil {
		panic(fmt.Errorf("INFINISPAN_IMAGE_SHA='%s'. Unable to get Docker image sha: : %w", os.Getenv("INFINISPAN_IMAGE_SHA"), err))
	}
	s := string(stdout)
	return strings.TrimSuffix(s, "\n")
}
