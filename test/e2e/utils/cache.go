package utils

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v13 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v13"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"k8s.io/apimachinery/pkg/util/wait"
)

type CacheHelper struct {
	Client      HTTPClient
	CacheName   string
	CacheClient api.Cache
}

func NewCacheHelper(cacheName string, client HTTPClient) *CacheHelper {
	return &CacheHelper{
		CacheClient: ispnClient.New(LatestOperand, client).Cache(cacheName),
		CacheName:   cacheName,
		Client:      client,
	}
}

func (c *CacheHelper) AssertCacheExists() {
	if !c.Exists() {
		panic(fmt.Sprintf("Caches %s does not exist", c.CacheName))
	}
}

func (c *CacheHelper) Exists() bool {
	exists, err := c.CacheClient.Exists()
	ExpectNoError(err)
	return exists
}

func (c *CacheHelper) AssertSize(expectedEntries int) {
	if entries := c.Size(); entries != expectedEntries {
		panic(fmt.Errorf("expected %d entries found %d", expectedEntries, entries))
	}
}

func (c *CacheHelper) Create(payload string, contentType mime.MimeType) {
	ExpectNoError(c.CacheClient.Create(payload, contentType))
}

func (c *CacheHelper) CreateWithDefault(flags ...string) {
	// mimeType is ignored by the server when the payload is ""
	ExpectNoError(c.CacheClient.Create("", mime.ApplicationYaml, flags...))
}

func (c *CacheHelper) Update(payload string, contentType mime.MimeType) {
	ExpectNoError(c.CacheClient.UpdateConfig(payload, contentType))
}

func (c *CacheHelper) TestBasicUsage(key, value string) {
	c.Put(key, value, mime.TextPlain)
	actual, exists := c.Get(key)
	if !exists {
		panic(fmt.Errorf("entry with key '%s' doesn't exist", key))
	}
	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func (c *CacheHelper) TestBasicUsageWithRetry(key, value string, retries int) {
	c.PutWithRetry(key, value, mime.TextPlain, retries)
	actual, exists := c.Get(key)
	if !exists {
		panic(fmt.Errorf("entry with key '%s' doesn't exist", key))
	}
	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func (c *CacheHelper) Delete() {
	ExpectNoError(c.CacheClient.Delete())
}

func (c *CacheHelper) Get(key string) (string, bool) {
	val, exists, err := c.CacheClient.Get(key)
	ExpectNoError(err)
	return val, exists
}

func (c *CacheHelper) Put(key, value string, contentType mime.MimeType) {
	ExpectNoError(c.CacheClient.Put(key, value, contentType))
}

func (c *CacheHelper) PutWithRetry(key, value string, contentType mime.MimeType, retries int) {
	err := c.CacheClient.Put(key, value, contentType)
	for i := 0; i < retries; i++ {
		if err == nil {
			return
		}
		fmt.Println("Attempt ", i, " failed on", err.Error(), ". Retrying...")
		time.Sleep(time.Second)
		err = c.CacheClient.Put(key, value, contentType)
	}
	ExpectNoError(err)
}

func (c *CacheHelper) Populate(numEntries int) {
	c.Client.Quiet(true)
	for i := 0; i < numEntries; i++ {
		key := strconv.Itoa(i)
		value := fmt.Sprintf("{\"value\":\"%d\"}", i)
		c.Put(key, value, mime.ApplicationJson)
	}
	c.Client.Quiet(false)
}

func (c *CacheHelper) Size() int {
	size, err := c.CacheClient.Size()
	ExpectNoError(err)
	return size
}

func (c *CacheHelper) WaitForCacheToExist() {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		exists, err := c.CacheClient.Exists()
		if err != nil {
			return false, err
		}
		return exists, nil
	})
	ExpectNoError(err)
}

func (c *CacheHelper) WaitForCacheToNotExist() {
	err := wait.Poll(DefaultPollPeriod, MaxWaitTimeout, func() (done bool, err error) {
		exists, err := c.CacheClient.Exists()
		if err != nil {
			return false, err
		}
		return !exists, nil
	})
	ExpectNoError(err)
}

func (c *CacheHelper) Available(available bool) {
	var availability string
	if available {
		availability = "AVAILABLE"
	} else {
		availability = "DEGRADED_MODE"
	}
	path := v13.NewPathResolver().Caches(fmt.Sprintf("/%s?action=set-availability&availability=%s", url.PathEscape(c.CacheName), availability))
	_, err := c.Client.Post(path, "", nil)
	ExpectNoError(err)
}
