package k8s

import (
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	"github.com/infinispan/infinispan-operator/test/e2e/constants"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/pkg/controller/apis/config/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// NewKubernetesFromController creates a new Kubernetes instance from controller runtime Manager
func NewKubernetesFromController(mgr manager.Manager) *Kubernetes {
	config := mgr.GetConfig()
	config = SetConfigDefaults(config)
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
	config = SetConfigDefaults(config)
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

func SetConfigDefaults(config *rest.Config) *rest.Config {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/api"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	return config
}

// MapperProvider is a function that provides RESTMapper instances
type MapperProvider func(cfg *rest.Config, opts ...apiutil.DynamicRESTMapperOption) (meta.RESTMapper, error)

// NewKubernetesFromConfig creates a new Kubernetes instance from configuration.
// The configuration is resolved locally from known locations.
func NewKubernetesFromLocalConfig(scheme *runtime.Scheme, mapperProvider MapperProvider) (*Kubernetes, error) {
	config := resolveConfig()
	config = SetConfigDefaults(config)
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

func createOptions(scheme *runtime.Scheme, mapper meta.RESTMapper) client.Options {
	return client.Options{
		Scheme: scheme,
		Mapper: mapper,
	}
}

// findKubeConfig returns local Kubernetes configuration
func findKubeConfig() string {
	return common.GetEnvWithDefault("KUBECONFIG", constants.ClusterUpKubeConfig)
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