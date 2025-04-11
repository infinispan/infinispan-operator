package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	corev1 "k8s.io/api/core/v1"
)

func (k *TestKubernetes) WriteAllMetricsToFile(dir, namespace string) {
	ispnList := &v1.InfinispanList{}
	err := k.Kubernetes.ResourcesList(namespace, map[string]string{}, ispnList, context.TODO())
	if err != nil {
		LogError(fmt.Errorf("unable to list Infinispan CRs: %w", err))
		return
	}

	for _, ispn := range ispnList.Items {
		k.WriteInfinispanMetricsToFile(&ispn, dir)
	}
}

func (k *TestKubernetes) WriteInfinispanMetricsToFile(i *v1.Infinispan, dir string) {
	var pods []corev1.Pod
	getPods := func(app string) {
		podList := &corev1.PodList{}
		err := k.Kubernetes.ResourcesList(i.Namespace, map[string]string{"app": app, "infinispan_cr": i.Name}, podList, context.TODO())
		if err != nil {
			LogError(fmt.Errorf("unable to retrieve pods with 'app=%s': %w", app, err))
		}
		pods = append(pods, podList.Items...)
	}
	getPods("infinispan-pod")
	getPods("infinispan-zero-pod")

	if len(pods) == 0 {
		Log().Warnf("No pods found for Infinispan '%s:%s'", i.Name, i.Namespace)
		return
	}

	adminSecret := k.GetSecret(i.GetAdminSecretName(), i.Namespace)
	if adminSecret == nil {
		LogError(fmt.Errorf("'%s' secret not found", i.GetAdminSecretName()))
		return
	}

	user, pass := string(adminSecret.Data[consts.AdminUsernameKey]), string(adminSecret.Data[consts.AdminPasswordKey])
	client := NewHTTPClient(user, pass, "http")

	for _, pod := range pods {
		if err := k.writeMetricsToFile(dir, &pod, client); err != nil {
			LogError(fmt.Errorf("unable to write metrics to file for pod '%s:'%s': %w", pod.Name, pod.Namespace, err))
		}
	}
}

func (k *TestKubernetes) writeMetricsToFile(dir string, pod *corev1.Pod, client HTTPClient) error {
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("pod not running. status.phase='%s'", pod.Status.Phase)
	}

	// Forward pod port
	client.SetHostAndPort("localhost:11223")
	stopChan, err := k.PortForward(pod.Name, pod.Namespace, []string{"11223:11223"})

	// Always close port-forward channel
	defer close(stopChan)
	if err != nil {
		return err
	}

	// Retrieve metrics
	rsp, err := client.Get("metrics", nil)
	if err != nil {
		return err
	}

	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response: '%s'", rsp.Status)
	}

	// Write metrics to file
	outFile, err := os.Create(fmt.Sprintf("/%s/%s.metrics.log", dir, pod.Name))
	if err != nil {
		return err
	}
	defer func(outFile *os.File) {
		err := outFile.Close()
		if err != nil {
			LogError(err)
		}
	}(outFile)
	_, err = io.Copy(outFile, rsp.Body)
	return err
}
