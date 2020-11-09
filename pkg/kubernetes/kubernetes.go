package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	// Using for local run and test only
	ClusterUpKubeConfig = "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
)

// Kubernetes abstracts interaction with a Kubernetes cluster
type Kubernetes struct {
	Client     client.Client
	RestClient rest.Interface
	RestConfig *rest.Config
}

// MapperProvider is a function that provides RESTMapper instances
type MapperProvider func(cfg *rest.Config, opts ...apiutil.DynamicRESTMapperOption) (meta.RESTMapper, error)

// NewKubernetesFromConfig creates a new Kubernetes instance from configuration.
// The configuration is resolved locally from known locations.
func NewKubernetesFromLocalConfig(scheme *runtime.Scheme, mapperProvider MapperProvider, ctx string) (*Kubernetes, error) {
	config := resolveConfig(ctx)
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
		RestClient: restClient,
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
		RestClient: restClient,
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
		RestClient: restClient,
		RestConfig: config,
	}
	return kubernetes, nil
}

// GetSecret returns secret associated with given secret name
func (k Kubernetes) GetSecret(secretName, namespace string) (*v1.Secret, error) {
	secret := &v1.Secret{}
	err := k.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: secretName}, secret)
	if err != nil {
		return nil, err
	}
	return secret, err
}

// GetPodIP returns a pod's IP address
func (k Kubernetes) GetPodIP(podName, namespace string) (string, error) {
	pod := &v1.Pod{}
	err := k.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: podName}, pod)
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
			Command: options.Command,
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     false,
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

func resolveConfig(ctx string) *rest.Config {
	internal, _ := rest.InClusterConfig()
	if internal == nil {
		kubeConfig := FindKubeConfig()
		configOvr := clientcmd.ConfigOverrides{}
		if ctx != "" {
			configOvr.CurrentContext = ctx
		}
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
			&configOvr)
		external, _ := clientConfig.ClientConfig()
		return external
	}
	return internal
}

func (k Kubernetes) GetNodePort(service *v1.Service) int32 {
	return service.Spec.Ports[0].NodePort
}

// PublicIP returns the public IP address of the Kubernetes cluster
func (k Kubernetes) PublicIP() string {
	u, _ := url.Parse(k.RestConfig.Host)
	return u.Hostname()
}

// GetNodesHost return the addresses of the k8s nodes
func (k Kubernetes) GetNodesHost() []string {
	nodes := &v1.NodeList{}
	k.Client.List(context.TODO(), nodes)
	nodeHosts := make([]string, 0, nodes.Size())
	for _, n := range nodes.Items {
		for _, v := range n.Status.Addresses {
			if v.Type == v1.NodeHostName {
				// Fallback using hostname if internal IP address is not available
				nodeHosts = append(nodeHosts, v.Address)
			}
			if v.Type == v1.NodeInternalIP {
				nodeHosts = append(nodeHosts, v.Address)
				break
			}
		}
	}
	return nodeHosts
}

// FindKubeConfig returns local Kubernetes configuration
func FindKubeConfig() string {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		return kubeConfig
	}
	return ClusterUpKubeConfig
}

func setConfigDefaults(config *rest.Config) *rest.Config {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/api"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	return config
}

func createOptions(scheme *runtime.Scheme, mapper meta.RESTMapper) client.Options {
	return client.Options{
		Scheme: scheme,
		Mapper: mapper,
	}
}

// ServiceCAsCRDResourceExists returns true if the platform
// has the servicecas.operator.openshift.io custom resource deployed
// Used to check if serviceca operator is serving TLS certificates
func (k Kubernetes) hasServiceCAsCRDResource() bool {
	// Using an ad-hoc path
	req := k.RestClient.Get().AbsPath("apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions/servicecas.operator.openshift.io")
	result := req.Do()
	var status int
	result.StatusCode(&status)
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

// getServingCertsMode returns a label that identify the kind of serving
// certs service is available. Returns 'openshift.io' for service-ca on openshift
func (k Kubernetes) GetServingCertsMode() string {
	if k.hasServiceCAsCRDResource() {
		return "openshift.io"

		// Code to check if other modes of serving TLS cert service is available
		// can be added here
	}
	return ""
}

func (k Kubernetes) GetMinikubeRESTConfig(masterURL, secretName, namespace string, logger logr.Logger) (*restclient.Config, error) {
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

func (k Kubernetes) GetOpenShiftRESTConfig(masterURL, secretName, namespace string, logger logr.Logger) (*restclient.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags(masterURL, "")
	if err != nil {
		logger.Error(err, "unable to create REST configuration", "master URL", masterURL)
		return nil, err
	}

	// Skip-tls for accessing other OpenShift clusters
	config.Insecure = true

	secret, err := k.GetSecret(secretName, namespace)
	if err != nil {
		logger.Error(err, "unable to find Secret", "secret name", secretName)
		return nil, err
	}

	if token, ok := secret.Data["token"]; ok {
		config.BearerToken = string(token)
		return config, nil
	}

	return nil, fmt.Errorf("token required connect to OpenShift cluster")
}

// ResourcesList returns a typed list of resource associated with the cluster
func (k Kubernetes) ResourcesList(namespace string, set labels.Set, list runtime.Object) error {
	labelSelector := labels.SelectorFromSet(set)
	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	err := k.Client.List(context.TODO(), list, listOps)
	return err
}
