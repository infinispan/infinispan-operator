package manage

import (
	"fmt"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// ClusterScaling can be removed once persistentVolumeClaimRetentionPolicy becomes stable and that k8s version is
// our baseline.
// https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#persistentvolumeclaim-retention
func ClusterScaling(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if *i.Status.Replicas == i.Spec.Replicas {
		return
	}

	statefulSet := &appsv1.StatefulSet{}
	if err := ctx.Resources().Load(i.GetStatefulSetName(), statefulSet, pipeline.InvalidateCache); err != nil {
		if errors.IsNotFound(err) {
			// No existing StatefulSet so nothing todo
			return
		}
		ctx.Requeue(fmt.Errorf("unable to retrieve StatefulSet in ClusterScaling: %w", err))
		return
	}

	if *statefulSet.Spec.Replicas != i.Spec.Replicas {
		// The StatefulSet has not been updated yet, so continue with reconciliation
		return
	}

	scalingUp := i.IsConditionTrue(ispnv1.ConditionScalingUp) && statefulSet.Status.ReadyReplicas < i.Spec.Replicas
	scalingDown := i.IsConditionTrue(ispnv1.ConditionScalingDown) && statefulSet.Status.ReadyReplicas > i.Spec.Replicas
	if scalingUp || scalingDown {
		// The StatefulSet spec has already been updated to i.spec.replicas, so we must wait for the pods to be updated
		ctx.Log().Info("waiting for the StatefulSet to scale", string(ispnv1.ConditionScalingUp), scalingUp, string(ispnv1.ConditionScalingDown), scalingDown)
		ctx.RequeueAfter(consts.DefaultWaitClusterPodsNotReady, nil)
		return
	}

	if *i.Status.Replicas > i.Spec.Replicas {
		// Scaling down
		if !i.IsConditionTrue(ispnv1.ConditionScalingDown) {
			ctx.Requeue(
				ctx.UpdateInfinispan(func() {
					i.SetCondition(ispnv1.ConditionScalingDown, metav1.ConditionTrue, fmt.Sprintf("Scaling down to %d replicas", i.Spec.Replicas))
				}),
			)
			return
		}

		if i.Spec.Replicas != 0 && *i.Status.Replicas != statefulSet.Status.CurrentReplicas {
			// Remove all that have an index between CurrentReplicas and i.Spec.Replicas
			for idx := *i.Status.Replicas - 1; idx >= statefulSet.Status.CurrentReplicas; idx-- {
				pvc := fmt.Sprintf("%s-%s-%d", provision.DataMountVolume, statefulSet.Name, idx)
				if err := ctx.Resources().Delete(pvc, &corev1.PersistentVolumeClaim{}); client.IgnoreNotFound(err) != nil {
					ctx.Requeue(fmt.Errorf("unable to remove PVC '%s' for old pod: %w", pvc, err))
					return
				}
			}
		}
	} else {
		// Scaling up
		if !i.IsConditionTrue(ispnv1.ConditionScalingUp) {
			ctx.Requeue(
				ctx.UpdateInfinispan(func() {
					i.SetCondition(ispnv1.ConditionScalingUp, metav1.ConditionTrue, fmt.Sprintf("Scaling up to %d replicas", i.Spec.Replicas))
				}),
			)
			return
		}

	}

	// Scaling has completed so remove the associated conditions and update .Status.Replicas
	_ = ctx.UpdateInfinispan(func() {
		i.Status.Replicas = &i.Spec.Replicas
		i.RemoveCondition(ispnv1.ConditionScalingDown)
		i.RemoveCondition(ispnv1.ConditionScalingUp)
	})
}
