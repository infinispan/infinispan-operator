package util

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
)

type CurlClient struct {
	credentials Credentials
	config      HttpConfig
	*Kubernetes
}

type HttpConfig struct {
	Username  string
	Password  string
	Namespace string
	Protocol  string
}

type HttpClient interface {
	Head(podName, path string, headers map[string]string) (*http.Response, error, string)
	Get(podName, path string, headers map[string]string) (*http.Response, error, string)
	Post(podName, path, payload string, headers map[string]string) (*http.Response, error, string)
}

func NewCurlClient(c HttpConfig, kubernetes *Kubernetes) *CurlClient {
	return &CurlClient{
		config: c,
		credentials: Credentials{
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
	curl := fmt.Sprintf("curl -i --insecure --http1.1 %s %s %s %s", user, headerString(headers), strings.Join(args, " "), httpURL)

	execOut, execErr, err := c.Kubernetes.ExecWithOptions(
		ExecOptions{
			Command:   []string{"bash", "-c", curl},
			PodName:   podName,
			Namespace: c.config.Namespace,
		})

	if err != nil {
		return nil, err, execErr
	}

	rsp, err := http.ReadResponse(bufio.NewReader(&execOut), nil)
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
