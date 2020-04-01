package k8s

import (
	"bytes"
	"context"
	"net/url"

	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Kubernetes abstracts interaction with a Kubernetes cluster
type Kubernetes struct {
	Client     client.Client
	RestClient rest.Interface
	RestConfig *rest.Config
}

// GetSecret returns secret associated with given secret name
func (k Kubernetes) GetSecret(secretName, namespace string) (*v1.Secret, error) {
	secret := &v1.Secret{}
	ns := types.NamespacedName{Name: secretName, Namespace: namespace}
	err := k.Client.Get(context.TODO(), ns, secret)
	if err != nil {
		return nil, err
	}
	return secret, err
}

// GetPassword returns password associated with a user in a given secret
func (k Kubernetes) GetPassword(user, secretName, namespace string) (string, error) {
	secret, err := k.GetSecret(secretName, namespace)
	if err != nil {
		return "", nil
	}

	descriptor := secret.Data["identities.yaml"]
	pass, err := util.FindPassword(user, descriptor)
	if err != nil {
		return "", err
	}

	return pass, nil
}

// GetPodIP returns a pod's IP address
func (k Kubernetes) GetPodIP(podName, namespace string) (string, error) {
	pod := &v1.Pod{}
	ns := types.NamespacedName{Name: podName, Namespace: namespace}
	err := k.Client.Get(context.TODO(), ns, pod)
	if err != nil {
		return "", err
	}

	return pod.Status.PodIP, err
}

// ExecOptions specify execution options
type ExecOptions struct {
	Command   []string
	Namespace string
	PodName   string
}

// ExecWithOptions executes command on pod
// command example { "/usr/bin/ls", "folderName" }
func (k Kubernetes) ExecWithOptions(options ExecOptions) (bytes.Buffer, string, error) {
	// Create a POST request
	execRequest := k.RestClient.Post().
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
	exec, err := remotecommand.NewSPDYExecutor(k.RestConfig, "POST", execRequest.URL())
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
	if err != nil {
		return execOut, execErr.String(), err
	}

	return execOut, "", err
}

func (k Kubernetes) GetNodePort(service *v1.Service) int32 {
	return service.Spec.Ports[0].NodePort
}

// PublicIP returns the public IP address of the Kubernetes cluster
func (k Kubernetes) PublicIP() string {
	u, _ := url.Parse(k.RestConfig.Host)
	return u.Hostname()
}

func (k Kubernetes) GetMinikubeRESTConfig(masterURL string, secretName string, namespace string, logger logr.Logger) (*restclient.Config, error) {
	logger.Info("connect to backup minikube cluster", "url", masterURL)

	config, err := clientcmd.BuildConfigFromFlags(masterURL, "")
	if err != nil {
		logger.Error(err, "unable to create REST configuration", "master URL", masterURL)
		return nil, err
	}

	secret, err := k.GetSecret(secretName, namespace)
	if err != nil {
		logger.Error(err, "unable to find Secret", "secret name", secretName)
		return nil, err
	}

	config.CAData = secret.Data["certificate-authority"]
	config.CertData = secret.Data["client-certificate"]
	config.KeyData = secret.Data["client-key"]

	return config, nil
}
