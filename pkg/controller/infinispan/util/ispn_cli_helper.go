package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes/scheme"

	v1 "k8s.io/api/core/v1"
	coreV1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IspnCliHelper represent an helper for running CLI commands
type IspnCliHelper struct {
	coreClient *coreV1.CoreV1Client
	restConfig *rest.Config
}

func getConfigLocation() string {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		return kubeConfig
	}
	return "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
}

var configLocation = getConfigLocation()

// NewIspnCliHelper create an IspnCliHelper
func NewIspnCliHelper() *IspnCliHelper {
	help := new(IspnCliHelper)
	help.restConfig, _ = rest.InClusterConfig()
	if help.restConfig == nil {
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: configLocation},
			&clientcmd.ConfigOverrides{})
		help.restConfig, _ = clientConfig.ClientConfig()
	}

	help.coreClient, _ = coreV1.NewForConfig(help.restConfig)
	return help
}

// GetClusterSize get the cluster size via the ISPN cli
func (help *IspnCliHelper) GetClusterSize(namespace, namePod, name string) (int, error) {
	members, err := help.GetClusterMembers(namespace, namePod, name)
	if err != nil {
		return -1, err
	}

	return len(members), nil
}

type ClusterHealth struct {
	Nodes []string `json:"node_names"`
}

type Health struct {
	ClusterHealth  ClusterHealth `json:"cluster_health"`
}

// GetClusterMembers get the cluster members via the ISPN cli
func (help *IspnCliHelper) GetClusterMembers(namespace, podName, name string) ([]string, error) {
	podIp, err := help.GetPodIp(namespace, podName)
	if err != nil {
		return nil, err
	}

	pass, err := help.GetOperatorPassword(name, namespace)
	if err != nil {
		return nil, err
	}

	httpUrl := fmt.Sprintf("http://%v:11222/rest/v2/cache-managers/DefaultCacheManager/health", podIp)
	commands := []string{"curl", "-u", fmt.Sprintf("operator:%v", pass), httpUrl}
	var execIn, execOut, execErr bytes.Buffer
	err = help.ExecuteCmdOnPod(namespace, podName, commands,
		&execIn, &execOut, &execErr)
	if err == nil {
		result := execOut.Bytes()

		var health Health
		err = json.Unmarshal(result, &health)
		if err != nil {
			return nil, fmt.Errorf("unable to decode: %v", err)
		}

		return health.ClusterHealth.Nodes, nil
	}
	return nil, err
}

func (help *IspnCliHelper) GetPodIp(namespace, podName string) (string, error) {
	pod, err := help.coreClient.
		Pods(namespace).
		Get(podName, metaV1.GetOptions{})
	if err != nil {
		return "", err
	}
	return pod.Status.PodIP, nil
}

func (help *IspnCliHelper) GetOperatorPassword(name, ns string) (string, error) {
	return GetPassword("operator", name, ns, help.coreClient)
}

func GetPassword(usr, name, ns string, coreClient *coreV1.CoreV1Client) (string, error) {
	secretName := GetSecretName(name)
	secret, err := coreClient.
		Secrets(ns).
		Get(secretName, metaV1.GetOptions{})
	if err != nil {
		return "", err
	}

	descriptor := secret.Data["identities.yaml"]
	pass, err := FindPassword(usr, descriptor)
	if err != nil {
		return "", err
	}

	return pass, nil
}

// ExecuteCmdOnPod Excecutes command on pod
// commands array example { "/usr/bin/ls", "folderName" }
// execIn, execOut, execErr stdin, stdout, stderr stream for the command
func (help *IspnCliHelper) ExecuteCmdOnPod(namespace, podName string, commands []string,
	execIn, execOut, execErr *bytes.Buffer) error {
	// Create a POST request
	execRequest := help.coreClient.RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&v1.PodExecOptions{
			Container: "infinispan",
			Command:   commands,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	// Create an executor
	exec, err := remotecommand.NewSPDYExecutor(help.restConfig, "POST", execRequest.URL())
	if err != nil {
		return err
	}
	// Run the command
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  execIn,
		Stdout: execOut,
		Stderr: execErr,
		Tty:    false,
	})
	return err
}

// GetEnvWithDefault return GetEnv(name) if exists else
// return defVal
func GetEnvWithDefault(name, defVal string) string {
	str := os.Getenv(name)
	if str != "" {
		return str
	}
	return defVal
}
