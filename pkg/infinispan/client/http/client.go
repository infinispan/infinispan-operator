package http

import "net/http"

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
