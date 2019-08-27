package util

import (
	"bytes"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"os"
)

var client *corev1.CoreV1Client
var config *rest.Config

// GetSecret returns secret associated with given secret name
func GetSecret(secretName, namespace string) (*v1.Secret, error) {
	secret, err := client.
		Secrets(namespace).
		Get(secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return secret, nil
}

// GetPodIP returns a pod's IP address
func GetPodIP(podName, namespace string) (string, error) {
	pod, err := client.
		Pods(namespace).
		Get(podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return pod.Status.PodIP, nil
}

// ExecOptions specify execution options
type ExecOptions struct {
	Command   []string
	Namespace string
	PodName   string
}

// ExecWithOptions executes command on pod
// command example { "/usr/bin/ls", "folderName" }
func ExecWithOptions(options ExecOptions) (bytes.Buffer, string, error) {
	// Create a POST request
	execRequest := client.RESTClient().Post().
		Resource("pods").
		Name(options.PodName).
		Namespace(options.Namespace).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: "infinispan",
			Command:   options.Command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	var execOut, execErr bytes.Buffer
	// Create an executor
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", execRequest.URL())
	if err != nil {
		return execOut, "", err
	}
	// Run the command
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})
	return execOut, "", err
}

func init() {
	config = resolveConfig()
	if config != nil {
		client, _ = corev1.NewForConfig(config)
	}
}

func resolveConfig() *rest.Config {
	internal, _ := rest.InClusterConfig()
	if internal == nil {
		kubeConfig := findKubeConfig()
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
			&clientcmd.ConfigOverrides{})
		external, _ := clientConfig.ClientConfig()
		return external
	}
	return internal
}

func findKubeConfig() string {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		return kubeConfig
	}
	return "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
}
