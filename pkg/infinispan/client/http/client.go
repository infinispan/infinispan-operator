package http

import (
	"net/http"
)

type Credentials struct {
	Username string
	Password string
}

type HttpConfig struct {
	Credentials *Credentials
	Namespace   string
	Protocol    string
}

type HttpClient interface {
	Head(podName, path string, headers map[string]string) (*http.Response, error, string)
	Get(podName, path string, headers map[string]string) (*http.Response, error, string)
	Post(podName, path, payload string, headers map[string]string) (*http.Response, error, string)
	Put(podName, path, payload string, headers map[string]string) (*http.Response, error, string)
	Delete(podName, path string, headers map[string]string) (*http.Response, error, string)
}
