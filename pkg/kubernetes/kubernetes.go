package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"

	"github.com/go-logr/logr"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Kubernetes abstracts interaction with a Kubernetes cluster
type Kubernetes struct {
	Client     client.Client
	RestClient rest.Interface
	RestConfig *rest.Config
}

// MapperProvider is a function that provides RESTMapper instances
type MapperProvider func(cfg *rest.Config, opts ...apiutil.DynamicRESTMapperOption) (meta.RESTMapper, error)

// NewKubernetesFromLocalConfig creates a new Kubernetes instance from configuration.
// The configuration is resolved locally from known locations.
func NewKubernetesFromLocalConfig(scheme *runtime.Scheme, mapperProvider MapperProvider, ctx string) (*Kubernetes, error) {
	config := resolveConfig(ctx)
	config = setConfigDefaults(config)
	mapper, err := mapperProvider(config)
	if err != nil {
		return nil, err
	}
	kubernetes, err := client.New(config, createOptions(scheme, mapper))
	if err != nil {
		return nil, err
	}
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}

	return &Kubernetes{
		Client:     kubernetes,
		RestClient: restClient,
		RestConfig: config,
	}, nil
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

// NewKubernetesFromConfig creates a new Kubernetes from the Kubernetes master URL to connect to
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

func (k Kubernetes) IsGroupVersionSupported(groupVersion string, kind string) (bool, error) {
	cli, err := discovery.NewDiscoveryClientForConfig(k.RestConfig)
	if err != nil {
		return false, err
	}
	res, err := cli.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, v := range res.APIResources {
		if v.Kind == kind {
			return true, nil
		}
	}

	return false, nil
}

// GetSecret returns secret associated with given secret name
func (k Kubernetes) GetSecret(secretName, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := k.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: secretName}, secret)
	if err != nil {
		return nil, err
	}
	return secret, err
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
		VersionedParams(&corev1.PodExecOptions{
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

// FindKubeConfig returns local Kubernetes configuration
func FindKubeConfig() string {
	return consts.GetEnvWithDefault("KUBECONFIG", consts.DefaultKubeConfig)
}

func setConfigDefaults(config *rest.Config) *rest.Config {
	gv := corev1.SchemeGroupVersion
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
func (k Kubernetes) hasServiceCAsCRDResource(version string) bool {
	// Using an ad-hoc path
	req := k.RestClient.Get().AbsPath("apis/apiextensions.k8s.io/" + version + "/customresourcedefinitions/servicecas.operator.openshift.io")
	result := req.Do()
	var status int
	result.StatusCode(&status)
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

// GetServingCertsMode returns a label that identify the kind of serving
// certs service is available. Returns 'openshift.io' for service-ca on openshift
func (k Kubernetes) GetServingCertsMode() string {
	if k.hasServiceCAsCRDResource("v1beta1") || k.hasServiceCAsCRDResource("v1") {
		return "openshift.io"

		// Code to check if other modes of serving TLS cert service is available
		// can be added here
	}
	return ""
}

func (k Kubernetes) GetKubernetesRESTConfig(masterURL, secretName, namespace string, logger logr.Logger) (*restclient.Config, error) {
	logger.Info("connect to backup Kubernetes cluster", "url", masterURL)

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

	for _, secretKey := range []string{"certificate-authority", "client-certificate", "client-key"} {
		if value, ok := secret.Data[secretKey]; !ok || len(value) == 0 {
			return nil, fmt.Errorf("%s required connect to Kubernetes cluster", secretKey)
		}
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

func (k Kubernetes) GetNodeHost(logger logr.Logger) (string, error) {
	//The IPs must be fetch. Some cases, the API server (which handles REST requests) isn't the same as the worker
	//So, we get the workers list. It needs some permissions cluster-reader permission
	//oc create clusterrolebinding <name> -n ${NAMESPACE} --clusterrole=cluster-reader --serviceaccount=${NAMESPACE}:<account-name>
	workerList := &corev1.NodeList{}

	//select workers first
	req, err := labels.NewRequirement("node-role.kubernetes.io/worker", selection.Exists, nil)
	if err != nil {
		return "", err
	}
	listOps := &client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*req),
	}
	err = k.Client.List(context.TODO(), workerList, listOps)

	if err != nil || len(workerList.Items) == 0 {
		// Fallback selecting everything
		err = k.Client.List(context.TODO(), workerList, &client.ListOptions{})
		if err != nil {
			return "", err
		}
	}

	nodes := workerList.Items
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})

	for _, node := range nodes {
		//host := k.PublicIP() //returns REST API endpoint. not good.
		//iterate over the all the nodes and return the first ready
		nodeStatus := node.Status
		for _, nodeCondition := range nodeStatus.Conditions {
			if nodeCondition.Type == corev1.NodeReady && nodeCondition.Status == corev1.ConditionTrue && len(nodeStatus.Addresses) > 0 {
				for _, addressType := range []corev1.NodeAddressType{corev1.NodeExternalIP, corev1.NodeInternalIP} {
					if host := GetNodeAddress(node, addressType); host != "" {
						logger.Info("Found ready worker node.", "Host", host)
						return host, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("no worker node found")
}

func GetNodeAddress(node corev1.Node, addressType corev1.NodeAddressType) string {
	for _, a := range node.Status.Addresses {
		if a.Type == addressType && a.Address != "" {
			return a.Address
		}
	}
	return ""
}

// GetExternalAddress extract LoadBalancer Hostname (typically for AWS load-balancers) or IP (typically for GCE or OpenStack load-balancers) address
func (k Kubernetes) GetExternalAddress(route *corev1.Service) string {
	// If the cluster exposes external IP then return it
	if len(route.Status.LoadBalancer.Ingress) > 0 {
		if route.Status.LoadBalancer.Ingress[0].IP != "" {
			return fmt.Sprintf("%s:%d", route.Status.LoadBalancer.Ingress[0].IP, route.Spec.Ports[0].Port)
		}
		if route.Status.LoadBalancer.Ingress[0].Hostname != "" {
			return fmt.Sprintf("%s:%d", route.Status.LoadBalancer.Ingress[0].Hostname, route.Spec.Ports[0].Port)
		}
	}
	// Return empty address if nothing available
	return ""
}

// ResourcesList returns a typed list of resource associated with the cluster
func (k Kubernetes) ResourcesList(namespace string, set labels.Set, list runtime.Object) error {
	labelSelector := labels.SelectorFromSet(set)
	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	err := k.Client.List(context.TODO(), list, listOps)
	return err
}

func (k Kubernetes) Logs(pod, namespace string) (logs string, err error) {
	readCloser, err := k.RestClient.Get().Namespace(namespace).Resource("pods").Name(pod).SubResource("log").Stream()
	if err != nil {
		return "", err
	}

	defer func() {
		cerr := readCloser.Close()
		if err == nil {
			err = cerr
		}
	}()

	body, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return "", err
	}
	return string(body), err
}
