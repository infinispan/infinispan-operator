package util

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Kubernetes abstracts interaction with a Kubernetes cluster
type Kubernetes struct {
	Client     client.Client
	restClient *rest.RESTClient
	RestConfig *rest.Config
}

// MapperProvider is a function that provides RESTMapper instances
type MapperProvider func(c *rest.Config) (meta.RESTMapper, error)

// NewKubernetesFromConfig creates a new Kubernetes instance from configuration.
// The configuration is resolved locally from known locations.
func NewKubernetesFromLocalConfig(scheme *runtime.Scheme, mapperProvider MapperProvider) (*Kubernetes, error) {
	config := resolveConfig()
	config = setConfigDefaults(config)
	mapper, err := mapperProvider(config)
	if err != nil {
		return nil, err
	}
	kubernetes, err := client.New(config, createOptions(scheme, mapper))
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}

	return &Kubernetes{
		Client:     kubernetes,
		restClient: restClient,
		RestConfig: config,
	}, err
}

// NewKubernetesFromController creates a new Kubernetes instance from controller runtime Manager
func NewKubernetesFromController(mgr manager.Manager) *Kubernetes {
	config := mgr.GetConfig()
	config = setConfigDefaults(config)
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		panic(err.Error())
	}

	return &Kubernetes{
		Client:     mgr.GetClient(),
		restClient: restClient,
		RestConfig: config,
	}

}

// NewKubernetesFromMasterURL creates a new Kubernetes from the Kubernetes master URL to connect to
func NewKubernetesFromConfig(config *rest.Config) (*Kubernetes, error) {
	kubeClient, err := client.New(config, client.Options{})
	if err != nil {
		return nil, err
	}
	config = setConfigDefaults(config)
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		panic(err.Error())
	}
	kubernetes := &Kubernetes{
		Client:     kubeClient,
		restClient: restClient,
		RestConfig: config,
	}
	return kubernetes, nil
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
	execRequest := k.restClient.Post().
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

func resolveConfig() *rest.Config {
	internal, _ := rest.InClusterConfig()
	if internal == nil {
		kubeConfig := FindKubeConfig()
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
			&clientcmd.ConfigOverrides{})
		external, _ := clientConfig.ClientConfig()
		return external
	}
	return internal
}

//func (k Kubernetes) GetPort(service *v1.Service) int32 {
//	return service.Spec.Ports[0].Port
//}

func (k Kubernetes) GetNodePort(service *v1.Service) int32 {
	return service.Spec.Ports[0].NodePort
}

// PublicIP returns the public IP address of the Kubernetes cluster
func (k Kubernetes) PublicIP() string {
	u, _ := url.Parse(k.RestConfig.Host)
	return u.Hostname()
}

// FindKubeConfig returns local Kubernetes configuration
func FindKubeConfig() string {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		return kubeConfig
	}
	return "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
}

func setConfigDefaults(config *rest.Config) *rest.Config {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/api"
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	return config
}

func createOptions(scheme *runtime.Scheme, mapper meta.RESTMapper) client.Options {
	return client.Options{
		Scheme: scheme,
		Mapper: mapper,
	}
}

func (k Kubernetes) GetMemoryLimitBytes(podName, namespace string) (uint64, error) {
	command := []string{"cat", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}
	execOptions := ExecOptions{Command: command, PodName: podName, Namespace: namespace}
	execOut, execErr, err := k.ExecWithOptions(execOptions)

	if err != nil {
		return 0, fmt.Errorf("unexpected error getting memory limit bytes, stderr: %v, err: %v", execErr, err)
	}

	result := strings.TrimSuffix(execOut.String(), "\n")
	limitBytes, err := strconv.ParseUint(result, 10, 64)
	if err != nil {
		return 0, err
	}

	return limitBytes, nil
}

func (k Kubernetes) GetMaxMemoryUnboundedBytes(podName, namespace string) (uint64, error) {
	command := []string{"cat", "/proc/meminfo"}
	execOptions := ExecOptions{Command: command, PodName: podName, Namespace: namespace}
	execOut, execErr, err := k.ExecWithOptions(execOptions)

	if err != nil {
		return 0, fmt.Errorf("unexpected error getting max unbounded memory, stderr: %v, err: %v", execErr, err)
	}

	result := execOut.String()
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "MemTotal:") {
			tokens := strings.Fields(line)
			maxUnboundKb, err := strconv.ParseUint(tokens[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return maxUnboundKb * 1024, nil
		}
	}

	return 0, fmt.Errorf("meminfo lacking MemTotal information")
}

// ServiceCAsCRDResourceExists returns true if the platform
// has the servicecas.operator.openshift.io custom resource deployed
// Used to check if serviceca operator is serving TLS certificates
func (k Kubernetes) HasServiceCAsCRDResource() bool {
	// Using an ad-hoc path
	req := k.restClient.Get().AbsPath("apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions/servicecas.operator.openshift.io")
	result := req.Do()
	var status int
	result.StatusCode(&status)
	return status >= 200 && status < 299
}
