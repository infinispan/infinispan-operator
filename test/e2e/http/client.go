package http

import (
	"bytes"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/infinispan/infinispan-operator/test/e2e/utils"
)

type HttpClient interface {
	Delete(path string, headers map[string]string) (*http.Response, error)
	Get(path string, headers map[string]string) (*http.Response, error)
	Post(path, payload string, headers map[string]string) (*http.Response, error)
}

type authenticationRealm struct {
	Username, Password, Realm, NONCE, QOP, Opaque, Algorithm string
}

type client struct {
	*http.Client
	username string
	password string
	protocol string
	authRealm *authenticationRealm
	requestCounter int
}

func New(username, password, protocol string) HttpClient {
	return &client{
		username: username,
		password: password,
		protocol: protocol,
		authRealm: nil,
		requestCounter: 0,
		Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
	}
}

func (c *client) Delete(path string, headers map[string]string) (*http.Response, error) {
	httpURL := fmt.Sprintf("%s://%s", c.protocol, path)
	fmt.Println("DELETE ", httpURL)
	req, err := http.NewRequest("DELETE", httpURL, nil)
	utils.ExpectNoError(err)
	return c.exec(req, headers)
}

func (c *client) Get(path string, headers map[string]string) (*http.Response, error) {
	httpURL := fmt.Sprintf("%s://%s", c.protocol, path)
	fmt.Println("GET ", httpURL)
	req, err := http.NewRequest("GET", httpURL, nil)
	utils.ExpectNoError(err)
	return c.exec(req, headers)
}

func (c *client) Post(path, payload string, headers map[string]string) (*http.Response, error) {
	httpURL := fmt.Sprintf("%s://%s", c.protocol, path)
	body := bytes.NewBuffer([]byte(payload))
	fmt.Println("POST ", httpURL)
	req, err := http.NewRequest("POST", httpURL, body)
	utils.ExpectNoError(err)
	return c.exec(req, headers)
}

func (c *client) exec(req *http.Request, headers map[string]string) (*http.Response, error) {
	if c.authRealm == nil {
		rsp, err := c.Do(req)
		utils.ExpectNoError(err)
		if rsp.StatusCode != http.StatusUnauthorized {
			return rsp, fmt.Errorf("Expected 401 DIGEST response before content: %v", rsp)
		}
		c.authRealm = getAuthorization(c.username, c.password, rsp)
	}
	c.requestCounter++
	authStr := getAuthString(c.authRealm, req.URL, req.Method, c.requestCounter)
	for header, value := range headers {
		req.Header.Add(header, value)
	}
	req.Header.Add("Authorization", authStr)
	rsp, err := c.Do(req)
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

func getAuthString(auth *authenticationRealm, url *url.URL, method string, nc int) string {
	a1 := auth.Username + ":" + auth.Realm + ":" + auth.Password
	h := md5.New()
	io.WriteString(h, a1)
	ha1 := hex.EncodeToString(h.Sum(nil))

	h = md5.New()
	a2 := method + ":" + url.Path
	io.WriteString(h, a2)
	ha2 := hex.EncodeToString(h.Sum(nil))

	nc_str := fmt.Sprintf("%08x", nc)
	hnc := "MTM3MDgw"

	respdig := fmt.Sprintf("%s:%s:%s:%s:%s:%s", ha1, auth.NONCE, nc_str, hnc, auth.QOP, ha2)
	h = md5.New()
	io.WriteString(h, respdig)
	respdig = hex.EncodeToString(h.Sum(nil))

	base := "username=\"%s\", realm=\"%s\", nonce=\"%s\", uri=\"%s\", response=\"%s\""
	base = fmt.Sprintf(base, auth.Username, auth.Realm, auth.NONCE, url.Path, respdig)
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
