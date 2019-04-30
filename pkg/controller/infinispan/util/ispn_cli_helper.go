package util

import (
	"bytes"
	"os"
	"regexp"
	"strconv"

	"k8s.io/client-go/kubernetes/scheme"

	v1 "k8s.io/api/core/v1"
	coreV1 "k8s.io/client-go/kubernetes/typed/core/v1"
	rest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type IspnCliHelper struct {
	coreClient *coreV1.CoreV1Client
	restConfig *rest.Config
}

func getConfigLocation() string {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		return kubeConfig
	} else {
		return "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
	}
}

var configLocation = getConfigLocation()

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

// GetClusterSize get the cluster size via the ISPN cli
func (help *IspnCliHelper) GetClusterSize(namespace, namePod string) (int, error) {
	cliCommand := "/subsystem=datagrid-infinispan/cache-container=clustered/:read-attribute(name=cluster-size)\n"
	commands := []string{"/opt/jboss/infinispan-server/bin/ispn-cli.sh", "--connect"}
	var execIn, execOut, execErr bytes.Buffer
	execIn.WriteString(cliCommand)
	err := help.ExecuteCmdOnPod(namespace, namePod, commands,
		&execIn, &execOut, &execErr)
	if err == nil {
		result := execOut.String()
		// Match the correct line in the output
		resultRegExp := regexp.MustCompile("\"result\" => \"\\d+\"")
		// Match the result value
		valueRegExp := regexp.MustCompile("\\d+")
		resultLine := resultRegExp.FindString(result)
		resultValueStr := valueRegExp.FindString(resultLine)
		return strconv.Atoi(resultValueStr)
	}
	return -1, err
}

// GetClusterMembers get the cluster members via the ISPN cli
func (help *IspnCliHelper) GetClusterMembers(namespace, namePod string) (string, error) {
	cliCommand := "/subsystem=datagrid-infinispan/cache-container=clustered/:read-attribute(name=members)\n"
	commands := []string{"/opt/jboss/infinispan-server/bin/ispn-cli.sh", "--connect"}
	var execIn, execOut, execErr bytes.Buffer
	execIn.WriteString(cliCommand)
	err := help.ExecuteCmdOnPod(namespace, namePod, commands,
		&execIn, &execOut, &execErr)
	if err == nil {
		result := execOut.String()
		// Match the correct line in the output
		resultRegExp := regexp.MustCompile("\"result\" => \"\\[.*\\]\"")
		// Match the result value
		valueRegExp := regexp.MustCompile("\\[.*\\]")
		resultLine := resultRegExp.FindString(result)
		resultValue := valueRegExp.FindString(resultLine)
		return resultValue, nil
	}
	return "-error-", err
}
