package utils

import (
	"fmt"
	"strconv"

	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
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
	exists, err := c.CacheClient.Exists()
	ExpectNoError(err)
	if !exists {
		panic(fmt.Sprintf("Caches %s does not exist", c.CacheName))
	}
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
