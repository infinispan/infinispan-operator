package kubernetes

import (
	"context"
	"reflect"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func AreAllPodsReady(podList *corev1.PodList) bool {
	for _, pod := range podList.Items {
		containerStatuses := pod.Status.ContainerStatuses
		if len(containerStatuses) == 0 || !containerStatuses[0].Ready {
			return false
		}
	}

	return true
}

func ArePodIPsReady(pods *corev1.PodList) bool {
	for _, pod := range pods.Items {
		if pod.Status.PodIP == "" {
			return false
		}
	}

	return len(pods.Items) > 0
}

func IsPodReady(pod corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func GetEnvVarIndex(envVarName string, env *[]corev1.EnvVar) int {
	for i, value := range *env {
		if value.Name == envVarName {
			return i
		}
	}
	return -1
}

func IsInitContainersEqual(srcContainer, destContainer []corev1.Container) bool {
	if len(srcContainer) != len(destContainer) {
		return false
	}
	for _, srcInitContainer := range srcContainer {
		if dstInitContainerIdx := ContainerIndex(destContainer, srcInitContainer.Name); dstInitContainerIdx < 0 {
			return false
		} else {
			if !reflect.DeepEqual(srcInitContainer.Command, destContainer[dstInitContainerIdx].Command) ||
				!reflect.DeepEqual(srcInitContainer.Args, destContainer[dstInitContainerIdx].Args) {
				return false
			}
		}
	}
	return true
}

func InitContainerFailed(containerStatuses []corev1.ContainerStatus) bool {
	for _, containerStatus := range containerStatuses {
		if containerStatus.LastTerminationState.Terminated != nil && containerStatus.LastTerminationState.Terminated.ExitCode != 0 {
			return true
		}
	}
	return false
}

func ContainerIndex(containers []corev1.Container, name string) int {
	for i, container := range containers {
		if container.Name == name {
			return i
		}
	}
	return -1
}

// findFinalOwnerRef tries to locate the final controller/owner based on the owner reference provided.
func findFinalOwnerRef(ns string, ownerRef *metav1.OwnerReference, client crclient.Client, ctx context.Context) (*metav1.OwnerReference, error) {
	if ownerRef == nil {
		return nil, nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(ownerRef.APIVersion)
	obj.SetKind(ownerRef.Kind)
	err := client.Get(ctx, types.NamespacedName{Namespace: ns, Name: ownerRef.Name}, obj)
	if err != nil {
		return nil, err
	}
	newOwnerRef := metav1.GetControllerOf(obj)
	if newOwnerRef != nil {
		return findFinalOwnerRef(ns, newOwnerRef, client, ctx)
	}

	return ownerRef, nil
}

func GetOperatorPodOwnerRef(ns string, client crclient.Client, ctx context.Context) (*metav1.OwnerReference, error) {
	// Get current Pod the operator is running in
	pod, err := GetPod(ctx, client, ns)
	if err != nil {
		return nil, err
	}
	podOwnerRefs := metav1.NewControllerRef(pod, pod.GroupVersionKind())
	// Get Owner that the Pod belongs to
	ownerRef := metav1.GetControllerOf(pod)
	finalOwnerRef, err := findFinalOwnerRef(ns, ownerRef, client, ctx)
	if err != nil {
		return nil, err
	}
	if finalOwnerRef != nil {
		return finalOwnerRef, nil
	}

	// Default to returning Pod as the Owner
	return podOwnerRefs, nil
}

// FilterPodsByOwnerUID Remove pods from podList not owned by the provided UID
func FilterPodsByOwnerUID(podList *corev1.PodList, ownerId types.UID) {
	pos := 0
	for _, item := range podList.Items {
		for _, reference := range item.GetOwnerReferences() {
			if reference.UID == ownerId {
				podList.Items[pos] = item
				pos++
			}
		}
	}
	podList.Items = podList.Items[:pos]
}

func GetContainer(name string, spec *corev1.PodSpec) *corev1.Container {
	for i, c := range spec.Containers {
		if c.Name == name {
			return &spec.Containers[i]
		}
	}
	return nil
}

func GetInitContainer(name string, spec *corev1.PodSpec) *corev1.Container {
	for i, c := range spec.InitContainers {
		if c.Name == name {
			return &spec.InitContainers[i]
		}
	}
	return nil
}

func VolumeExists(name string, spec *corev1.PodSpec) bool {
	for _, volume := range spec.Volumes {
		if volume.Name == name {
			return true
		}
	}
	return false
}

func VolumeMountExists(name string, container *corev1.Container) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == name {
			return true
		}
	}
	return false
}

func SortPodsByName(podList *corev1.PodList) {
	if podList == nil {
		return
	}
	sort.Slice(podList.Items, func(i, j int) bool {
		// Compare the Name field of the ObjectMeta for two pods
		return podList.Items[i].ObjectMeta.Name < podList.Items[j].ObjectMeta.Name
	})
}
