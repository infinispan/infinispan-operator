package utils

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"k8s.io/apimachinery/pkg/util/wait"
)

func CacheURL(cacheName, hostAddr string) string {
	return fmt.Sprintf("%v/rest/v2/caches/%s", hostAddr, cacheName)
}

func CreateCache(cacheName, hostAddr string, flags string, client HTTPClient) *http.Response {
	httpURL := CacheURL(cacheName, hostAddr)
	headers := map[string]string{}
	if flags != "" {
		headers["Flags"] = flags
	}
	resp, err := client.Post(httpURL, "", headers)
	ExpectNoError(err)
	return resp
}

func CreateCacheAndValidate(cacheName, hostAddr string, flags string, client HTTPClient) {
	resp := CreateCache(cacheName, hostAddr, flags, client)
	defer CloseHttpResponse(resp)
	if resp.StatusCode != http.StatusOK {
		panic(HttpError{resp.StatusCode})
	}
}

func CreateCacheWithXMLTemplate(cacheName, hostAddr, template string, client HTTPClient) {
	httpURL := CacheURL(cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpURL)
	headers := map[string]string{
		"Content-Type": "application/xml;charset=UTF-8",
	}
	resp, err := client.Post(httpURL, template, headers)
	defer CloseHttpResponse(resp)
	ExpectNoError(err)
	// Accept all the 2xx success codes
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		ThrowHTTPError(resp)
	}
}

func TestBasicCacheUsage(key, value, cacheName, hostAddr string, client HTTPClient) {
	keyURL := fmt.Sprintf("%v/%v", CacheURL(cacheName, hostAddr), key)
	PutViaRoute(keyURL, value, client)
	actual := GetViaRoute(keyURL, client)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func PutViaRoute(url, value string, client HTTPClient) {
	headers := map[string]string{
		"Content-Type": "text/plain",
	}
	resp, err := client.Post(url, value, headers)
	defer CloseHttpResponse(resp)
	ExpectNoError(err)
	if resp.StatusCode != http.StatusNoContent {
		ThrowHTTPError(resp)
	}
}

func DeleteCache(cacheName, hostAddr string, client HTTPClient) {
	httpURL := CacheURL(cacheName, hostAddr)
	resp, err := client.Delete(httpURL, nil)
	ExpectNoError(err)

	if resp.StatusCode != http.StatusOK {
		panic(HttpError{resp.StatusCode})
	}
}

func GetViaRoute(url string, client HTTPClient) string {
	resp, err := client.Get(url, nil)
	ExpectNoError(err)
	defer func(Body io.ReadCloser) {
		ExpectNoError(Body.Close())
	}(resp.Body)
	if resp.StatusCode != http.StatusOK {
		ThrowHTTPError(resp)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	ExpectNoError(err)
	return string(bodyBytes)
}

func WaitForCacheToBeCreated(cacheName, hostAddr string, client HTTPClient) {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		httpURL := CacheURL(cacheName, hostAddr)
		fmt.Printf("Waiting for cache to be created")
		resp, err := client.Get(httpURL, nil)
		if err != nil {
			return false, err
		}
		return resp.StatusCode == http.StatusOK, nil
	})
	ExpectNoError(err)
}
