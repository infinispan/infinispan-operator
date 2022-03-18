package manage

import (
	"fmt"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func AwaitPodIps(i *ispnv1.Infinispan, ctx pipeline.Context) {
	podList, err := ctx.InfinispanPods()
	if err != nil {
		return
	}

	if !kube.ArePodIPsReady(podList) {
		ctx.Log().Info("Pod IPs are not ready yet")
		ctx.RequeueAfter(consts.DefaultWaitClusterPodsNotReady,
			ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionWellFormed, metav1.ConditionUnknown, "Pods are not ready")
				i.RemoveCondition(ispnv1.ConditionCrossSiteViewFormed)
			}),
		)
	}
}

// RemoveFailedInitContainers Recover Pods with updated init containers in case of fails
func RemoveFailedInitContainers(i *ispnv1.Infinispan, ctx pipeline.Context) {
	podList, err := ctx.InfinispanPods()
	if err != nil {
		return
	}

	statefulSet := &appsv1.StatefulSet{}
	if err := ctx.Resources().Load(i.GetStatefulSetName(), statefulSet, pipeline.RetryOnErr); err != nil {
		return
	}

	for _, pod := range podList.Items {
		if !kube.IsInitContainersEqual(statefulSet.Spec.Template.Spec.InitContainers, pod.Spec.InitContainers) {
			if kube.InitContainerFailed(pod.Status.InitContainerStatuses) {
				if err := ctx.Resources().Delete(pod.Name, &pod); err != nil {
					ctx.Requeue(err)
					return
				}
			}
		}
	}
}

// UpdatePodLabels Ensure all pods have upto date labels
func UpdatePodLabels(i *ispnv1.Infinispan, ctx pipeline.Context) {
	podList, err := ctx.InfinispanPods()
	if err != nil {
		return
	}

	if len(podList.Items) == 0 {
		return
	}

	labelsForPod := i.PodLabels()
	for _, pod := range podList.Items {
		podLabels := make(map[string]string)
		for index, value := range pod.Labels {
			if _, ok := labelsForPod[index]; ok || consts.SystemPodLabels[index] {
				podLabels[index] = value
			}
		}
		for index, value := range labelsForPod {
			podLabels[index] = value
		}

		mutateFn := func() error {
			if pod.CreationTimestamp.IsZero() {
				return errors.NewNotFound(corev1.Resource(""), pod.Name)
			}
			pod.Labels = podLabels
			return nil
		}
		_, err := ctx.Resources().CreateOrUpdate(&pod, false, mutateFn, pipeline.IgnoreNotFound, pipeline.RetryOnErr)
		if err != nil {
			return
		}
	}
}

func ConfigureLoggers(infinispan *ispnv1.Infinispan, ctx pipeline.Context) {
	if infinispan.Spec.Logging == nil || len(infinispan.Spec.Logging.Categories) == 0 {
		return
	}

	podList, err := ctx.InfinispanPods()
	if err != nil {
		return
	}

	for _, pod := range podList.Items {
		logging := ctx.InfinispanClientForPod(pod.Name).Logging()
		serverLoggers, err := logging.GetLoggers()
		if err != nil {
			ctx.Requeue(fmt.Errorf("unable to obtain loggers: %w", err))
			return
		}
		for category, level := range infinispan.Spec.Logging.Categories {
			serverLevel, ok := serverLoggers[category]
			if !(ok && string(level) == serverLevel) {
				if err := logging.SetLogger(category, string(level)); err != nil {
					ctx.Requeue(fmt.Errorf("unable to set logger %s=%s: %w", category, string(level), err))
					return
				}
			}
		}
	}
}
