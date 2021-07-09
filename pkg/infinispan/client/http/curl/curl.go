package curl

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	client "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
)

type CurlClient struct {
	credentials *client.Credentials
	config      client.HttpConfig
	*kube.Kubernetes
}

func New(c client.HttpConfig, kubernetes *kube.Kubernetes) *CurlClient {
	return &CurlClient{
		config:      c,
		credentials: c.Credentials,
		Kubernetes:  kubernetes,
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

func (c *CurlClient) Put(podName, path, payload string, headers map[string]string) (*http.Response, error, string) {
	data := ""
	if payload != "" {
		data = fmt.Sprintf("-d $'%s'", payload)
	}
	return c.executeCurlCommand(podName, path, headers, data, "-X PUT")
}

func (c *CurlClient) executeCurlCommand(podName string, path string, headers map[string]string, args ...string) (*http.Response, error, string) {
	httpURL := fmt.Sprintf("%s://%s:%d/%s", c.config.Protocol, podName, c.config.Port, path)

	headerStr := headerString(headers)
	argStr := strings.Join(args, " ")

	if c.credentials != nil {
		return c.executeCurlWithAuth(httpURL, headerStr, argStr, podName)
	}
	return c.executeCurlNoAuth(httpURL, headerStr, argStr, podName)
}

func (c *CurlClient) executeCurlWithAuth(httpURL, headers, args, podName string) (*http.Response, error, string) {
	user := fmt.Sprintf("-u %v:%v", c.credentials.Username, c.credentials.Password)
	curl := fmt.Sprintf("curl -i --insecure --digest --http1.1 %s %s %s %s", user, headers, args, httpURL)

	execOut, execErr, err := c.exec(curl, podName)
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

	return handleContent(reader)
}

func (c *CurlClient) executeCurlNoAuth(httpURL, headers, args, podName string) (*http.Response, error, string) {
	curl := fmt.Sprintf("curl -i --insecure --http1.1 %s %s %s", headers, args, httpURL)
	execOut, execErr, err := c.exec(curl, podName)
	if err != nil {
		return nil, err, execErr
	}

	reader := bufio.NewReader(&execOut)
	return handleContent(reader)
}

func (c *CurlClient) exec(cmd, podName string) (bytes.Buffer, string, error) {
	return c.Kubernetes.ExecWithOptions(
		kube.ExecOptions{
			Command:   []string{"bash", "-c", cmd},
			PodName:   podName,
			Namespace: c.config.Namespace,
		})
}

func handleContent(reader *bufio.Reader) (*http.Response, error, string) {
	rsp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return nil, err, ""
	}

	// Save response body
	b := new(bytes.Buffer)
	if _, err = io.Copy(b, rsp.Body); err != nil {
		return nil, err, ""
	}
	if err := rsp.Body.Close(); err != nil {
		return nil, err, ""
	}
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
