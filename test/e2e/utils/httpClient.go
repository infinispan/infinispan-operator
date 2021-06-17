package utils

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

const DEBUG = false

// HTTPClient can perform HTTP operations
type HTTPClient interface {
	Delete(path string, headers map[string]string) (*http.Response, error)
	Get(path string, headers map[string]string) (*http.Response, error)
	Post(path, payload string, headers map[string]string) (*http.Response, error)
}

type authType int

const (
	authNone authType = iota
	authDigest
	authCert
)

type authenticationRealm struct {
	Username, Password, Realm, NONCE, QOP, Opaque, Algorithm string
}

type httpClientConfig struct {
	*http.Client
	username *string
	password *string
	protocol string
	auth     authType
}

// NewHTTPClient return a new HTTPClient
func NewHTTPClient(username, password, protocol string) HTTPClient {
	return newClient(authDigest, &username, &password, protocol, &tls.Config{
		InsecureSkipVerify: true,
	})
}

func NewHTTPClientNoAuth(protocol string) HTTPClient {
	return newClient(authNone, nil, nil, protocol, &tls.Config{
		InsecureSkipVerify: true,
	})
}

func NewHTTPSClientNoAuth(tlsConfig *tls.Config) HTTPClient {
	return newClient(authNone, nil, nil, "https", tlsConfig)
}

func NewHTTPSClientCert(tlsConfig *tls.Config) HTTPClient {
	return newClient(authCert, nil, nil, "https", tlsConfig)
}

func NewHTTPSClient(username, password string, tlsConfig *tls.Config) HTTPClient {
	return newClient(authDigest, &username, &password, "https", tlsConfig)
}

func newClient(auth authType, username, password *string, protocol string, tlsConfig *tls.Config) HTTPClient {
	return &httpClientConfig{
		username: username,
		password: password,
		protocol: protocol,
		auth:     auth,
		Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

func (c *httpClientConfig) Delete(path string, headers map[string]string) (*http.Response, error) {
	return c.exec("DELETE", path, "", nil)
}

func (c *httpClientConfig) Get(path string, headers map[string]string) (*http.Response, error) {
	return c.exec("GET", path, "", nil)
}

func (c *httpClientConfig) Post(path, payload string, headers map[string]string) (*http.Response, error) {
	return c.exec("POST", path, payload, headers)
}

func (c *httpClientConfig) exec(method, path, payload string, headers map[string]string) (*http.Response, error) {
	httpURL, err := url.Parse(fmt.Sprintf("%s://%s", c.protocol, path))
	ExpectNoError(err)
	fmt.Printf("%s: %s\n", method, httpURL)
	rsp, err := c.request(httpURL, method, payload, headers)
	if err != nil {
		return nil, err
	}

	if c.auth == authDigest && rsp.StatusCode == http.StatusUnauthorized {
		ExpectNoError(rsp.Body.Close())
		h := headers
		if h == nil {
			h = map[string]string{}
		}
		authRealm := getAuthorization(*c.username, *c.password, rsp)
		authStr := getAuthString(authRealm, httpURL.RequestURI(), method, 0)
		h["Authorization"] = authStr
		rsp, err = c.request(httpURL, method, payload, h)
	}
	return rsp, err
}

func (c *httpClientConfig) request(url *url.URL, method, payload string, headers map[string]string) (*http.Response, error) {
	var body io.Reader
	if payload != "" {
		body = bytes.NewBuffer([]byte(payload))
	}
	req, err := http.NewRequest(method, url.String(), body)
	ExpectNoError(err)
	for header, value := range headers {
		req.Header.Add(header, value)
	}

	if DEBUG {
		dump, err := httputil.DumpRequestOut(req, true)
		ExpectNoError(err)
		fmt.Printf("Req>>>>>>>>>>>>>>>>\n%s\n\n", string(dump))
	}

	rsp, err := c.Do(req)
	if DEBUG {
		dump, err := httputil.DumpResponse(rsp, true)
		ExpectNoError(err)
		fmt.Printf("Rsp<<<<<<<<<<<<<<<<\n%s\n\n", string(dump))
	}
	return rsp, err
}

func getAuthorization(username, password string, resp *http.Response) *authenticationRealm {
	header := resp.Header.Get("www-authenticate")
	parts := strings.SplitN(header, " ", 2)
	parts = strings.Split(parts[1], ", ")
	opts := make(map[string]string)

	for _, part := range parts {
		vals := strings.SplitN(part, "=", 2)
		key := vals[0]
		val := strings.Trim(vals[1], "\",")
		opts[key] = val
	}

	auth := authenticationRealm{
		username, password,
		opts["realm"], opts["nonce"], opts["qop"], opts["opaque"], opts["algorithm"],
	}
	return &auth
}

func getAuthString(auth *authenticationRealm, path, method string, nc int) string {
	a1 := auth.Username + ":" + auth.Realm + ":" + auth.Password
	h := md5.New()
	_, err := io.WriteString(h, a1)
	ExpectNoError(err)

	ha1 := hex.EncodeToString(h.Sum(nil))

	h = md5.New()
	a2 := method + ":" + path
	_, err = io.WriteString(h, a2)
	ExpectNoError(err)

	ha2 := hex.EncodeToString(h.Sum(nil))

	nc_str := fmt.Sprintf("%08x", nc)
	hnc := getCnonce()

	respdig := fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, auth.NONCE, nc_str, hnc, auth.QOP, ha2)
	h = md5.New()
	_, err = io.WriteString(h, respdig)
	ExpectNoError(err)

	respdig = hex.EncodeToString(h.Sum(nil))

	base := "username=\"%s\", realm=\"%s\", nonce=\"%s\", uri=\"%s\", response=\"%s\""
	base = fmt.Sprintf(base, auth.Username, auth.Realm, auth.NONCE, path, respdig)
	if auth.Opaque != "" {
		base += fmt.Sprintf(", opaque=\"%s\"", auth.Opaque)
	}
	if auth.QOP != "" {
		base += fmt.Sprintf(", qop=\"%s\", nc=%s, cnonce=\"%s\"", auth.QOP, nc_str, hnc)
	}
	if auth.Algorithm != "" {
		base += fmt.Sprintf(", algorithm=\"%s\"", auth.Algorithm)
	}

	return "Digest " + base
}

func getCnonce() string {
	b := make([]byte, 8)
	_, err := io.ReadFull(rand.Reader, b)
	ExpectNoError(err)
	return fmt.Sprintf("%x", b)[:16]
}
