package kubernetes

import corev1 "k8s.io/api/core/v1"

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

	return true
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
	return 0
}
