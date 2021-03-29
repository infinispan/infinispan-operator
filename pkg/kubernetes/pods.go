package kubernetes

import (
	"context"

	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPodDefaultImage returns an Infinispan pod's default image.
// If the default image cannot be found, it returns the running image.
func GetPodDefaultImage(container corev1.Container) string {
	envs := container.Env
	for _, env := range envs {
		if env.Name == "DEFAULT_IMAGE" {
			return env.Value
		}
	}

	return container.Image
}

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
		if cond.Type == corev1.ContainersReady && cond.Status == corev1.ConditionTrue {
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

func GetOperatorPodOwnerRef(ns string, client crclient.Client) (*metav1.OwnerReference, error) {
	// Get current Pod the operator is running in
	pod, err := k8sutil.GetPod(context.TODO(), client, ns)
	if err != nil {
		return nil, err
	}
	podOwnerRefs := metav1.NewControllerRef(pod, pod.GroupVersionKind())
	// Get Owner that the Pod belongs to
	ownerRef := metav1.GetControllerOf(pod)
	finalOwnerRef, err := findFinalOwnerRef(ns, ownerRef, client)
	if err != nil {
		return nil, err
	}
	if finalOwnerRef != nil {
		return finalOwnerRef, nil
	}

	// Default to returning Pod as the Owner
	return podOwnerRefs, nil
}

// findFinalOwnerRef tries to locate the final controller/owner based on the owner reference provided.
func findFinalOwnerRef(ns string, ownerRef *metav1.OwnerReference, client crclient.Client) (*metav1.OwnerReference, error) {
	if ownerRef == nil {
		return nil, nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(ownerRef.APIVersion)
	obj.SetKind(ownerRef.Kind)
	err := client.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: ownerRef.Name}, obj)
	if err != nil {
		return nil, err
	}
	newOwnerRef := metav1.GetControllerOf(obj)
	if newOwnerRef != nil {
		return findFinalOwnerRef(ns, newOwnerRef, client)
	}

	return ownerRef, nil
}
