package utils

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
)

const DEBUG = false

// HTTPClient can perform HTTP operations
type HTTPClient interface {
	httpClient.HttpClient
	Quiet(quiet bool)
	SetHostAndPort(hostAndPort string)
	GetHostAndPort() string
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
	hostAndPort string
	username    *string
	password    *string
	protocol    string
	auth        authType
	quiet       bool
}

func ThrowHTTPError(rsp *http.Response) {
	errorBytes, _ := io.ReadAll(rsp.Body)
	panic(httpClient.HttpError{
		Status:  rsp.StatusCode,
		Message: string(errorBytes),
	})
}

// NewHTTPClient return a new HTTPClient
func NewHTTPClient(username, password, protocol string) HTTPClient {
	return NewClient(authDigest, &username, &password, protocol, &tls.Config{
		InsecureSkipVerify: true,
	})
}

func NewHTTPClientNoAuth(protocol string) HTTPClient {
	return NewClient(authNone, nil, nil, protocol, &tls.Config{
		InsecureSkipVerify: true,
	})
}

func NewClient(auth authType, username, password *string, protocol string, tlsConfig *tls.Config) HTTPClient {
	return &httpClientConfig{
		username: username,
		password: password,
		protocol: protocol,
		auth:     auth,
		Client: &http.Client{
			Timeout: 30 * time.Second,
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

func (c *httpClientConfig) Head(path string, headers map[string]string) (*http.Response, error) {
	return c.exec("HEAD", path, "", nil)
}

func (c *httpClientConfig) Post(path, payload string, headers map[string]string) (*http.Response, error) {
	return c.exec("POST", path, payload, headers)
}

func (c *httpClientConfig) Put(path, payload string, headers map[string]string) (*http.Response, error) {
	return c.exec("PUT", path, payload, headers)
}

func (c *httpClientConfig) PostMultipart(path string, parts map[string]string, headers map[string]string) (*http.Response, error) {
	return nil, errors.New("multipart support needs to be implemented")
}

func (c *httpClientConfig) SetHostAndPort(hostAndPort string) {
	c.hostAndPort = hostAndPort
}

func (c *httpClientConfig) GetHostAndPort() string {
	return c.hostAndPort
}

func (c *httpClientConfig) Quiet(quiet bool) {
	c.quiet = quiet
}

func (c *httpClientConfig) exec(method, path, payload string, headers map[string]string) (*http.Response, error) {
	httpURL, err := url.Parse(fmt.Sprintf("%s://%s/%s", c.protocol, c.hostAndPort, path))
	ExpectNoError(err)
	if !c.quiet {
		fmt.Printf("%s: %s\n", method, httpURL)
	}
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
	if DEBUG && rsp != nil {
		dump, err := httputil.DumpResponse(rsp, true)
		ExpectNoError(err)
		fmt.Printf("Rsp<<<<<<<<<<<<<<<<\n%s\n\n", string(dump))
	}
	return rsp, err
}

func getAuthorization(username, password string, resp *http.Response) *authenticationRealm {
	var digestRealm string
	for _, realm := range resp.Header.Values("www-authenticate") {
		if strings.HasPrefix(realm, "Digest realm") {
			digestRealm = realm
			break
		}
	}

	parts := strings.SplitN(digestRealm, " ", 2)
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
