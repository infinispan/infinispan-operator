package schema

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	cconsts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	testifyAssert "github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ctx      = context.TODO()
	testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)


const sampleProto = `syntax = "proto2";

package test;

message Person {
  required string name = 1;
  required int32 id = 2;
  optional string email = 3;
}
`

const updatedProto = `syntax = "proto2";

package test;

message Person {
  required string name = 1;
  required int32 id = 2;
  optional string email = 3;
  optional string phone = 4;
}
`

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func initCluster(t *testing.T, configListener bool) *v1.Infinispan {
	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: configListener,
			Logging: &v1.ConfigListenerLoggingSpec{
				Level: v1.ConfigListenerLoggingDebug,
			},
		}
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, v1.ConditionWellFormed)

	if configListener {
		testKube.WaitForDeployment(spec.GetConfigListenerName(), tutils.Namespace)
	}
	return ispn
}

func assertConfigListenerHasNoErrorsOrRestarts(t *testing.T, i *v1.Infinispan) {
	testKube.WaitForDeployment(i.GetConfigListenerName(), tutils.Namespace)
	podList := testKube.WaitForPods(1, tutils.SinglePodTimeout, &client.ListOptions{
		Namespace: tutils.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app":         "infinispan-config-listener-pod",
			"clusterName": i.Name,
		})},
		nil,
	)
	testifyAssert.Equal(t, 1, len(podList.Items))

	pod := podList.Items[0]
	testifyAssert.Equal(t, int32(0), pod.Status.ContainerStatuses[0].RestartCount)
	logs, err := testKube.Kubernetes.Logs(provision.InfinispanListenerContainer, pod.Name, tutils.Namespace, false, ctx)
	tutils.ExpectNoError(err)
	testifyAssert.NotContains(t, logs, "ERROR", "Error(s) exist in ConfigListener logs")
}

// TestSchemaCR verifies that creating a Schema CR registers the schema on the server,
// and deleting the CR removes it.
func TestSchemaCR(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)

	schemaName := ispn.Name + ".proto"
	schema := schemaCR(schemaName, sampleProto, ispn)
	testKube.Create(schema)
	testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)

	// Assert that the schema exists on the server
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)
	schemaHelper := tutils.NewSchemaHelper(schemaName, httpClient)
	schemaHelper.WaitForSchemaToExist()

	// Delete the Schema CR
	testKube.DeleteSchema(schema)

	// Assert that the schema is removed from the server once the Schema CR has been deleted
	schemaHelper.WaitForSchemaToNotExist()
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
}

// TestUpdateSchemaCR verifies that updating the schema content in a Schema CR
// propagates the change to the server.
func TestUpdateSchemaCR(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	schemaName := ispn.Name + ".proto"

	// Create Schema CR
	schema := schemaCR(schemaName, sampleProto, ispn)
	testKube.Create(schema)
	schema = testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)
	testifyAssert.Equal(t, int64(1), schema.GetGeneration())

	// Update schema content
	schema.Spec.Schema = updatedProto
	testKube.Update(schema)

	// Assert CR spec.Schema updated
	testKube.WaitForSchemaState(schemaName, ispn.Name, tutils.Namespace, func(s *v2alpha1.Schema) bool {
		return strings.Contains(s.Spec.Schema, "phone")
	})

	// Assert CR remains ready
	schema = testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)
	testifyAssert.Equal(t, int64(2), schema.GetGeneration())

	// Assert the server has the updated schema content
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)
	schemaHelper := tutils.NewSchemaHelper(schemaName, httpClient)
	serverSchema := schemaHelper.Get()
	testifyAssert.Contains(t, serverSchema, "phone")

	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
}

// TestSchemaWithServerLifecycle verifies that schemas created/updated/deleted via the
// REST API are reflected as Schema CRs by the ConfigListener.
func TestSchemaWithServerLifecycle(t *testing.T) {
	tutils.SkipPriorTo(t, cconsts.MinVersionSchemaReconciliation.String(), "Bidirectional schema sync requires Infinispan >= "+cconsts.MinVersionSchemaReconciliation.String())
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	schemaName := ispn.Name + ".proto"

	// Create schema via REST
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)
	schemaHelper := tutils.NewSchemaHelper(schemaName, httpClient)
	schemaHelper.Create(sampleProto)

	// Assert CR created and ready
	cr := testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)

	// Assert that the owner reference has been correctly set to the Infinispan CR
	testifyAssert.True(t, kubernetes.IsOwnedBy(cr, ispn), "Schema has unexpected owner reference")

	// Update schema via REST
	schemaHelper.CreateOrUpdate(updatedProto)

	// Assert CR spec.Schema updated
	testKube.WaitForSchemaState(schemaName, ispn.Name, tutils.Namespace, func(s *v2alpha1.Schema) bool {
		return strings.Contains(s.Spec.Schema, "phone")
	})

	// Delete schema via REST
	schemaHelper.Delete()

	// Assert CR deleted
	err := wait.Poll(10*time.Millisecond, tutils.MaxWaitTimeout, func() (bool, error) {
		return !testKube.AssertK8ResourceExists(cr.Name, tutils.Namespace, &v2alpha1.Schema{}), nil
	})
	tutils.ExpectNoError(err)
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
}

// TestSchemaClusterRecreate verifies that Schema CRs become unready when the cluster
// is deleted and are reconciled when the cluster is recreated.
func TestSchemaClusterRecreate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, false)
	schemaName := ispn.Name + ".proto"

	// Create Schema CR
	cr := schemaCR(schemaName, sampleProto, ispn)
	testKube.Create(cr)
	testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)

	// Assert that the schema exists on the server
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)
	schemaHelper := tutils.NewSchemaHelper(schemaName, httpClient)
	schemaHelper.WaitForSchemaToExist()

	// Delete the original cluster
	testKube.DeleteInfinispan(ispn)

	// Wait for the Schema CR to become unready as the Infinispan CR no longer exists
	testKube.WaitForSchemaCondition(schemaName, ispn.Name, tutils.Namespace, v2alpha1.SchemaCondition{
		Type:   v2alpha1.SchemaConditionReady,
		Status: metav1.ConditionFalse,
	})

	// Recreate the cluster
	ispn = initCluster(t, false)

	// Assert that the schema is recreated and the CR has the ready status
	testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)

	// Assert that the schema exists on the server
	httpClient = tutils.HTTPClientForCluster(ispn, testKube)
	schemaHelper = tutils.NewSchemaHelper(schemaName, httpClient)
	schemaHelper.WaitForSchemaToExist()
}

// TestCacheSchemaDependency verifies the dependency between Cache and Schema CRs:
// 1. A Cache CR referencing a non-existent schema should fail (not ready, not on server)
// 2. Creating the Schema CR should unblock the Cache CR (both become ready on server)
// 3. Data can be stored and retrieved using the proto types
func TestCacheSchemaDependency(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)

	schemaName := ispn.Name + ".proto"
	bookProto := `
package book_sample;

/* @Indexed */
message Book {
	/* @Field(store = Store.YES, analyze = Analyze.YES) */
	/* @Text(projectable = true) */
	optional string title = 1;

	/* @Text(projectable = true) */
	optional string description = 2;

	optional int32 publicationYear = 3;
}
`
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)

	// Create a Cache CR that references a Schema CR that does not yet exist
	schemaCRName := strings.TrimSuffix(schemaName, ".proto")
	cacheName := ispn.Name + "-indexed"
	cacheConfig := `{"distributed-cache":{"encoding":{"media-type":"application/x-protostream"},"indexing":{"indexed-entities":["book_sample.Book"]}}}`
	cache := cacheCR(cacheName, cacheConfig, ispn)
	cache.Spec.SchemaRefs = []v2alpha1.SchemaRef{{Name: schemaCRName}}
	testKube.Create(cache)

	// Assert the Cache CR becomes not-ready because the schema doesn't exist
	testKube.WaitForCacheCondition(cacheName, ispn.Name, tutils.Namespace, v2alpha1.CacheCondition{
		Type:   v2alpha1.CacheConditionReady,
		Status: metav1.ConditionFalse,
	})

	// Assert the cache does NOT exist on the server
	cacheHelper := tutils.NewCacheHelper(cacheName, httpClient)
	testifyAssert.False(t, cacheHelper.Exists(), "Cache should not exist on server without schema")

	// Now create the Schema CR
	schema := schemaCR(schemaName, bookProto, ispn)
	testKube.Create(schema)
	testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)

	// Verify the schema is registered on the server
	schemaHelper := tutils.NewSchemaHelper(schemaName, httpClient)
	schemaHelper.WaitForSchemaToExist()

	// The cache controller requeues on failure, so the Cache CR should eventually become ready
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Verify the cache now exists on the server
	cacheHelper.WaitForCacheToExist()

	// Put and get data using the proto types
	numEntries := 3
	for i := 0; i < numEntries; i++ {
		data := fmt.Sprintf(`{"_type":"book_sample.Book","title":"book%d","publicationYear":%d}`, i, 2020+i)
		cacheHelper.Put(data, data, mime.ApplicationJson)
	}
	cacheHelper.AssertSize(numEntries)

	// Verify we can retrieve an entry
	value, exists := cacheHelper.Get(`{"_type":"book_sample.Book","title":"book0","publicationYear":2020}`)
	testifyAssert.True(t, exists, "Entry should exist in cache")
	testifyAssert.Contains(t, value, "book0")

	// Delete the cache first, then the schema
	testKube.DeleteCache(cache)
	cacheHelper.WaitForCacheToNotExist()

	testKube.DeleteSchema(schema)
	schemaHelper.WaitForSchemaToNotExist()

	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
}

func TestCacheMultipleSchemaDependencies(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)

	bookSchemaName := ispn.Name + "-book.proto"
	bookProto := `
package book_sample;

message Book {
	optional string title = 1;
	optional int32 publicationYear = 2;
}
`
	authorSchemaName := ispn.Name + "-author.proto"
	authorProto := `
package author_sample;

message Author {
	optional string name = 1;
}
`
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)

	bookCRName := strings.TrimSuffix(bookSchemaName, ".proto")
	authorCRName := strings.TrimSuffix(authorSchemaName, ".proto")

	// Create a Cache CR that references two Schema CRs that do not yet exist
	cacheName := ispn.Name + "-multi-schema"
	cacheConfig := `{"distributed-cache":{"encoding":{"media-type":"application/x-protostream"}}}`
	cache := cacheCR(cacheName, cacheConfig, ispn)
	cache.Spec.SchemaRefs = []v2alpha1.SchemaRef{{Name: bookCRName}, {Name: authorCRName}}
	testKube.Create(cache)

	// Cache should not be ready because neither schema exists
	testKube.WaitForCacheCondition(cacheName, ispn.Name, tutils.Namespace, v2alpha1.CacheCondition{
		Type:   v2alpha1.CacheConditionReady,
		Status: metav1.ConditionFalse,
	})

	cacheHelper := tutils.NewCacheHelper(cacheName, httpClient)
	testifyAssert.False(t, cacheHelper.Exists(), "Cache should not exist on server without schemas")

	// Create the first Schema CR — cache should still not be ready
	bookSchema := schemaCR(bookSchemaName, bookProto, ispn)
	testKube.Create(bookSchema)
	testKube.WaitForSchemaConditionReady(bookSchemaName, ispn.Name, tutils.Namespace)

	bookSchemaHelper := tutils.NewSchemaHelper(bookSchemaName, httpClient)
	bookSchemaHelper.WaitForSchemaToExist()

	// Cache should still be not-ready because the second schema is missing
	testKube.WaitForCacheCondition(cacheName, ispn.Name, tutils.Namespace, v2alpha1.CacheCondition{
		Type:   v2alpha1.CacheConditionReady,
		Status: metav1.ConditionFalse,
	})
	testifyAssert.False(t, cacheHelper.Exists(), "Cache should not exist with only one schema ready")

	// Create the second Schema CR — cache should become ready
	authorSchema := schemaCR(authorSchemaName, authorProto, ispn)
	testKube.Create(authorSchema)
	testKube.WaitForSchemaConditionReady(authorSchemaName, ispn.Name, tutils.Namespace)

	authorSchemaHelper := tutils.NewSchemaHelper(authorSchemaName, httpClient)
	authorSchemaHelper.WaitForSchemaToExist()

	// Now the cache should become ready
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
	cacheHelper.WaitForCacheToExist()

	// Clean up
	testKube.DeleteCache(cache)
	cacheHelper.WaitForCacheToNotExist()

	testKube.DeleteSchema(bookSchema)
	bookSchemaHelper.WaitForSchemaToNotExist()

	testKube.DeleteSchema(authorSchema)
	authorSchemaHelper.WaitForSchemaToNotExist()

	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
}

// TestSchemaInUseProtection verifies that a Schema CR referenced by a Cache CR
// cannot be deleted until the Cache reference is removed.
func TestSchemaInUseProtection(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, false)

	schemaName := ispn.Name + ".proto"
	schema := schemaCR(schemaName, sampleProto, ispn)
	testKube.Create(schema)
	testKube.WaitForSchemaConditionReady(schemaName, ispn.Name, tutils.Namespace)

	// Create a Cache CR that references the Schema
	schemaCRName := strings.TrimSuffix(schemaName, ".proto")
	cacheName := ispn.Name + "-protected"
	cacheConfig := `{"distributed-cache":{"encoding":{"media-type":"application/x-protostream"}}}`
	cache := cacheCR(cacheName, cacheConfig, ispn)
	cache.Spec.SchemaRefs = []v2alpha1.SchemaRef{{Name: schemaCRName}}
	testKube.Create(cache)
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Attempt to delete the Schema CR
	testKube.DeleteSchema(schema)

	// Schema should still exist with a not-ready condition indicating it's in use
	testKube.WaitForSchemaState(schemaName, ispn.Name, tutils.Namespace, func(s *v2alpha1.Schema) bool {
		if s.GetDeletionTimestamp().IsZero() {
			return false
		}
		c := s.GetCondition(v2alpha1.SchemaConditionReady)
		return c.Status == metav1.ConditionFalse && strings.Contains(c.Message, cacheName)
	})

	// Delete the Cache CR, removing the reference
	testKube.DeleteCache(cache)

	// Now the Schema should be fully deleted
	err := wait.Poll(10*time.Millisecond, tutils.MaxWaitTimeout, func() (bool, error) {
		return testKube.FindSchemaResource(schemaName, ispn.Name, tutils.Namespace) == nil, nil
	})
	tutils.ExpectNoError(err)
}

func cacheCR(cacheName string, template string, i *v1.Infinispan) *v2alpha1.Cache {
	cache := &v2alpha1.Cache{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Cache",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cacheName,
			Namespace: i.Namespace,
			Labels:    i.ObjectMeta.Labels,
		},
		Spec: v2alpha1.CacheSpec{
			ClusterName: i.Name,
			Name:        cacheName,
			Template:    template,
		},
	}
	cache.Default()
	return cache
}

func schemaCR(schemaName string, proto string, i *v1.Infinispan) *v2alpha1.Schema {
	s := &v2alpha1.Schema{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Schema",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.TrimSuffix(schemaName, ".proto"),
			Namespace: i.Namespace,
			Labels:    i.ObjectMeta.Labels,
		},
		Spec: v2alpha1.SchemaSpec{
			ClusterName: i.Name,
			Name:        schemaName,
			Schema:      proto,
		},
	}
	return s
}
