package k8s

import (
	"fmt"
	"net/http"
	"os"

	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/k8s"
	"github.com/infinispan/infinispan-operator/pkg/launcher"
	tconstants "github.com/infinispan/infinispan-operator/test/e2e/constants"
	"github.com/infinispan/infinispan-operator/test/e2e/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// MapperProvider is a function that provides RESTMapper instances
type MapperProvider func(cfg *rest.Config, opts ...apiutil.DynamicRESTMapperOption) (meta.RESTMapper, error)

// NewKubernetesFromConfig creates a new Kubernetes instance from configuration.
// The configuration is resolved locally from known locations.
func NewKubernetesFromLocalConfig(scheme *runtime.Scheme, mapperProvider MapperProvider) (*k8s.Kubernetes, error) {
	config := resolveConfig()
	config = k8s.SetConfigDefaults(config)
	mapper, err := mapperProvider(config)
	if err != nil {
		return nil, err
	}
	kubernetes, err := client.New(config, createOptions(scheme, mapper))
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}

	return &k8s.Kubernetes{
		Client:     kubernetes,
		RestClient: restClient,
		RestConfig: config,
	}, err
}

// findKubeConfig returns local Kubernetes configuration
func findKubeConfig() string {
	return common.GetEnvWithDefault("KUBECONFIG", tconstants.ClusterUpKubeConfig)
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

func createOptions(scheme *runtime.Scheme, mapper meta.RESTMapper) client.Options {
	return client.Options{
		Scheme: scheme,
		Mapper: mapper,
	}
}

// Run the operator locally
func runOperatorLocally(stopCh chan struct{}, namespace string) {
	kubeConfig := findKubeConfig()
	_ = os.Setenv("WATCH_NAMESPACE", namespace)
	_ = os.Setenv("KUBECONFIG", kubeConfig)
	launcher.Launch(launcher.Parameters{StopChannel: stopCh})
}

func debugPods(required int, pods []v1.Pod) {
	log.Info("pod list incomplete", "required", required, "pod list size", len(pods))
	for _, pod := range pods {
		log.Info("pod info", "name", pod.ObjectMeta.Name, "statuses", pod.Status.ContainerStatuses)
	}
}

func checkExternalAddress(client *http.Client, user, password, hostAndPort string, logger logr.Logger) bool {
	httpURL := fmt.Sprintf("http://%s/%s", hostAndPort, constants.ServerHTTPHealthPath)
	req, err := http.NewRequest("GET", httpURL, nil)
	utils.ExpectNoError(err)
	req.SetBasicAuth(user, password)
	resp, err := client.Do(req)
	if err != nil {
		logger.Error(err, "Unable to complete HTTP request for external address")
		return false
	}
	logger.Info("Received response for external address", "response code", resp.StatusCode)
	return resp.StatusCode == http.StatusOK
}
