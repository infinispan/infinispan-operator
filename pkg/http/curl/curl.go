// Package curl provides a http implementation that utilises curl requests on an existing k8s pod
package curl

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
)

type Credentials struct {
	Username string
	Password string
}

type Config struct {
	Credentials *Credentials
	Podname     string
	Namespace   string
	Protocol    string
	Port        int
}

type Client struct {
	Config Config
	*kube.Kubernetes
}

func New(c Config, kubernetes *kube.Kubernetes) *Client {
	return &Client{
		Config:     c,
		Kubernetes: kubernetes,
	}
}

func (c *Client) CloneForPod(podName string) *Client {
	client := New(c.Config, c.Kubernetes)
	client.Config.Podname = podName
	return client
}

func (c *Client) Get(path string, headers map[string]string) (*http.Response, error) {
	return c.executeCurlCommand(path, headers)
}

func (c *Client) Head(path string, headers map[string]string) (*http.Response, error) {
	return c.executeCurlCommand(path, headers, "--head")
}

func (c *Client) Post(path, payload string, headers map[string]string) (*http.Response, error) {
	data := ""
	if payload != "" {
		data = fmt.Sprintf("-d $'%s'", payload)
	}
	return c.executeCurlCommand(path, headers, data, "-X POST")
}

func (c *Client) Put(path, payload string, headers map[string]string) (*http.Response, error) {
	data := ""
	if payload != "" {
		data = fmt.Sprintf("-d $'%s'", payload)
	}
	return c.executeCurlCommand(path, headers, data, "-X PUT")
}

func (c *Client) Delete(path string, headers map[string]string) (*http.Response, error) {
	return c.executeCurlCommand(path, headers, "-X DELETE")
}

func (c *Client) executeCurlCommand(path string, headers map[string]string, args ...string) (*http.Response, error) {
	httpURL := fmt.Sprintf("%s://%s:%d/%s", c.Config.Protocol, c.Config.Podname, c.Config.Port, path)

	headerStr := headerString(headers)
	argStr := strings.Join(args, " ")

	if c.Config.Credentials != nil {
		return c.executeCurlWithAuth(httpURL, headerStr, argStr)
	}
	return c.executeCurlNoAuth(httpURL, headerStr, argStr)
}

func (c *Client) executeCurlWithAuth(httpURL, headers, args string) (*http.Response, error) {
	user := fmt.Sprintf("-u %v:%v", c.Config.Credentials.Username, c.Config.Credentials.Password)
	curl := fmt.Sprintf("curl -i --insecure --digest --http1.1 %s %s %s %s", user, headers, args, httpURL)

	execOut, err := c.exec(curl)
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(&execOut)
	rsp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return nil, err
	}

	if rsp.StatusCode != http.StatusUnauthorized {
		return rsp, fmt.Errorf("expected 401 DIGEST response before content")
	}

	return handleContent(reader)
}

func (c *Client) executeCurlNoAuth(httpURL, headers, args string) (*http.Response, error) {
	curl := fmt.Sprintf("curl -i --insecure --http1.1 %s %s %s", headers, args, httpURL)
	execOut, err := c.exec(curl)
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(&execOut)
	return handleContent(reader)
}

func (c *Client) exec(cmd string) (bytes.Buffer, error) {
	return c.Kubernetes.ExecWithOptions(
		kube.ExecOptions{
			Command:   []string{"bash", "-c", cmd},
			PodName:   c.Config.Podname,
			Namespace: c.Config.Namespace,
		})
}

func handleContent(reader *bufio.Reader) (*http.Response, error) {
	rsp, err := http.ReadResponse(reader, nil)
	if err != nil {
		return nil, err
	}

	// Save response body
	b := new(bytes.Buffer)
	if _, err = io.Copy(b, rsp.Body); err != nil {
		return nil, err
	}
	if err := rsp.Body.Close(); err != nil {
		return nil, err
	}
	rsp.Body = ioutil.NopCloser(b)
	return rsp, nil
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
