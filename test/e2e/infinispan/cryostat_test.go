package infinispan

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	cryostatv1beta1 "github.com/infinispan/infinispan-operator/pkg/apis/cryostat/v1beta1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/manage"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCryostatProvisioning(t *testing.T) {
	tutils.AssumeGVKAvailable(t, testKube, pipeline.CryostatGVK)
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	replicas := 1
	i := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Cryostat = &ispnv1.CryostatSpec{Enabled: true}
	})

	testKube.CreateInfinispan(i, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, i.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(i.Name, i.Namespace, ispnv1.ConditionWellFormed)

	cryostat := &cryostatv1beta1.Cryostat{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cryostatv1beta1.GroupVersion.String(),
			Kind:       "Cryostat",
		},
	}

	tutils.ExpectNoError(
		wait.Poll(tutils.DefaultPollPeriod, tutils.MaxWaitTimeout, func() (done bool, err error) {
			nsName := types.NamespacedName{Namespace: i.Namespace, Name: i.GetCryostatName()}
			if err := testKube.Kubernetes.Client.Get(context.TODO(), nsName, cryostat); runtimeClient.IgnoreNotFound(err) != nil {
				return false, err
			} else if errors.IsNotFound(err) {
				return false, nil
			}

			availableCondition := manage.CryostatDeploymentAvailableCondition(cryostat)
			if availableCondition.Status != metav1.ConditionTrue {
				return false, nil
			}

			// If an Openshift Route is not available, then applicationUrl remains empty
			return !testKube.IsGVKAvailable(pipeline.RouteGVK) || cryostat.Status.ApplicationURL != "", nil
		}),
	)

	tutils.ExpectNoError(
		wait.Poll(tutils.DefaultPollPeriod, tutils.MaxWaitTimeout, func() (done bool, err error) {
			podList := &corev1.PodList{}
			listOpts := &runtimeClient.ListOptions{
				Namespace: tutils.Namespace,
				LabelSelector: labels.SelectorFromSet(
					map[string]string{
						"app":  cryostat.Name,
						"kind": "cryostat",
					},
				),
			}
			if err := testKube.Kubernetes.Client.List(context.TODO(), podList, listOpts); runtimeClient.IgnoreNotFound(err) != nil {
				return false, err
			}

			if len(podList.Items) != 1 {
				return false, nil
			}

			pod := podList.Items[0]
			if !kube.IsPodReady(pod) {
				return false, nil
			}

			token := base64.StdEncoding.EncodeToString([]byte(testKube.Kubernetes.RestConfig.BearerToken))
			cmd := fmt.Sprintf("curl -v -k -f -H \"Authorization: Bearer %s\" https://%s:%d/%s/1", token, pod.Name, manage.CryostatPort, manage.CryostatCredentialsApi)
			_, err = testKube.Kubernetes.ExecWithOptions(
				kube.ExecOptions{
					Container: cryostat.Name,
					Command:   []string{"bash", "-c", cmd},
					PodName:   pod.Name,
					Namespace: cryostat.Namespace,
				},
			)
			if err == nil {
				return true, nil
			}
			return false, nil
		}),
	)
}
