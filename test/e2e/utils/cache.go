package utils

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v14 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"k8s.io/apimachinery/pkg/util/wait"
)

type CacheHelper struct {
	Client      HTTPClient
	CacheName   string
	CacheClient api.Cache
}

func NewCacheHelper(cacheName string, client HTTPClient) *CacheHelper {
	return NewCacheHelperForOperand(cacheName, CurrentOperand, client)
}

func NewCacheHelperForOperand(cacheName string, operand version.Operand, client HTTPClient) *CacheHelper {
	return &CacheHelper{
		CacheClient: ispnClient.New(operand, client).Cache(cacheName),
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

func (c *CacheHelper) CreateDistributedCache(encoding mime.MimeType) {
	config := fmt.Sprintf("{\"distributed-cache\":{\"mode\":\"SYNC\",\"remote-timeout\": 60000,\"encoding\":{\"media-type\":\"%s\"}}}", encoding)
	c.Create(config, mime.ApplicationJson)
}

func (c *CacheHelper) CreateAndPopulateIndexedCache(entries int) {
	if c.Exists() {
		Log().Infof("Cache '%s' already exists", c.CacheName)
		return
	}
	proto := `
package book_sample;

/* @Indexed */
message Book {
	/* @Field(store = Store.YES, analyze = Analyze.YES) */
	/* @Text(projectable = true) */
	optional string title = 1;

	/* @Text(projectable = true) */
	optional string description = 2;

	// no native Date type available in Protobuf
	optional int32 publicationYear = 3;

	repeated Author authors = 4;
}

message Author {
	optional string name = 1;
	optional string surname = 2;
}
`
	headers := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}
	_, err := c.Client.Post("rest/v2/caches/___protobuf_metadata/schema.proto", proto, headers)
	ExpectNoError(err)

	config := "{\"distributed-cache\":{\"encoding\":{\"media-type\":\"application/x-protostream\"},\"persistence\":{\"file-store\":{}},\"indexing\":{\"indexed-entities\":[\"book_sample.Book\"]}}}"
	c.Create(config, mime.ApplicationJson)
	c.Client.Quiet(true)
	for i := 0; i < entries; i++ {
		data := fmt.Sprintf("{\"_type\":\"book_sample.Book\",\"title\":\"book%d\"}", i)
		c.Put(data, data, mime.ApplicationJson)
	}
	c.Client.Quiet(false)
	Log().Infof("Populated cache %s with %d entries", c.CacheName, entries)
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
		Log().Warnln("Attempt ", i, " failed on", err.Error(), ". Retrying...")
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

// PopulatePlainCache populates a cache with bounded parallelism
func (c *CacheHelper) PopulatePlainCache(entries int) {
	c.Client.Quiet(true)
	for i := 0; i < entries; i++ {
		data := strconv.Itoa(i)
		c.Put(data, data, mime.TextPlain)
	}
	c.Client.Quiet(false)
	Log().Infof("Populated cache %s with %d entries", c.CacheName, entries)
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
	path := v14.NewPathResolver().Caches(fmt.Sprintf("/%s?action=set-availability&availability=%s", url.PathEscape(c.CacheName), availability))
	_, err := c.Client.Post(path, "", nil)
	ExpectNoError(err)
}
