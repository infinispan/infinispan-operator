package curl

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	client "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
)

type CurlClient struct {
	credentials security.Credentials
	config      client.HttpConfig
	*kube.Kubernetes
}

func New(c client.HttpConfig, kubernetes *kube.Kubernetes) *CurlClient {
	return &CurlClient{
		config: c,
		credentials: security.Credentials{
			Username: c.Username,
			Password: c.Password,
		},
		Kubernetes: kubernetes,
	}
}

func (c *CurlClient) Get(podName, path string, headers map[string]string) (*http.Response, error, string) {
	return c.executeCurlCommand(podName, path, headers)
}

func (c *CurlClient) Head(podName, path string, headers map[string]string) (*http.Response, error, string) {
	return c.executeCurlCommand(podName, path, headers, "--head")
}

func (c *CurlClient) Post(podName, path, payload string, headers map[string]string) (*http.Response, error, string) {
	data := ""
	if payload != "" {
		data = fmt.Sprintf("-d $'%s'", payload)
	}
	return c.executeCurlCommand(podName, path, headers, data, "-X POST")
}

func (c *CurlClient) executeCurlCommand(podName string, path string, headers map[string]string, args ...string) (*http.Response, error, string) {
	httpURL := fmt.Sprintf("%s://%s:%d/%s", c.config.Protocol, podName, consts.InfinispanPort, path)
	user := fmt.Sprintf("-u %v:%v", c.credentials.Username, c.credentials.Password)
	curl := fmt.Sprintf("curl -i --insecure --digest --http1.1 %s %s %s %s", user, headerString(headers), strings.Join(args, " "), httpURL)

	execOut, execErr, err := c.Kubernetes.ExecWithOptions(
		kube.ExecOptions{
			Command:   []string{"bash", "-c", curl},
			PodName:   podName,
			Namespace: c.config.Namespace,
		})

	if err != nil {
		return nil, err, execErr
	}

	reader := bufio.NewReader(&execOut)
	rsp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return nil, err, ""
	}

	if rsp.StatusCode != http.StatusUnauthorized {
		return rsp, nil, "Expected 401 DIGEST response before content"
	}

	rsp, err = http.ReadResponse(reader, nil)
	if err != nil {
		return nil, err, ""
	}

	// Save response body
	b := new(bytes.Buffer)
	io.Copy(b, rsp.Body)
	rsp.Body.Close()
	rsp.Body = ioutil.NopCloser(b)

	return rsp, nil, ""
}

func headerString(headers map[string]string) string {
	if headers == nil {
		return ""
	}
	b := new(bytes.Buffer)
	for key, value := range headers {
		fmt.Fprintf(b, "-H \"%s: %s\" ", key, value)
	}
	return b.String()
}
