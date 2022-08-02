package infinispan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	cconsts "github.com/infinispan/infinispan-operator/controllers/constants"
	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
)

// TestExternalServiceWithAuth starts a cluster and checks application
// and management connection with authentication
func TestExplicitCredentials(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	usr := "connectorusr"
	pass := "connectorpass"
	newpass := "connectornewpass"
	identitiesYaml, err := users.CreateIdentitiesFor(usr, pass)
	tutils.ExpectNoError(err)

	// Create secret with application credentials
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "conn-secret-test",
			Namespace: tutils.Namespace,
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{cconsts.ServerIdentitiesFilename: string(identitiesYaml)},
	}
	testKube.CreateSecret(&secret)
	defer testKube.DeleteSecret(&secret)

	// Create Infinispan
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Security.EndpointSecretName = "conn-secret-test"
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	schema := testKube.GetSchemaForRest(ispn)
	testAuthentication(ispn, schema, usr, pass)
	// Update the auth credentials.
	identitiesYaml, err = users.CreateIdentitiesFor(usr, newpass)
	tutils.ExpectNoError(err)

	// Create secret with application credentials
	secret1 := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "conn-secret-test-1",
			Namespace: tutils.Namespace,
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{cconsts.ServerIdentitiesFilename: string(identitiesYaml)},
	}
	testKube.CreateSecret(&secret1)
	defer testKube.DeleteSecret(&secret1)

	// Get the associate statefulset
	ss := appsv1.StatefulSet{}

	// Get the current generation
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.GetStatefulSetName()}, &ss))
	generation := ss.Status.ObservedGeneration

	err = testKube.UpdateInfinispan(spec, func() {
		spec.Spec.Security.EndpointSecretName = "conn-secret-test-1"
	})
	tutils.ExpectNoError(err)

	// Wait for a new generation to appear
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss))
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	tutils.ExpectNoError(err)

	// Sleep for a while to be sure that the old pods are gone
	time.Sleep(10 * time.Second)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
	testAuthentication(ispn, schema, usr, newpass)
}

func testAuthentication(ispn *ispnv1.Infinispan, schema, usr, pass string) {
	client_ := testKube.WaitForExternalService(ispn, tutils.RouteTimeout, tutils.NewHTTPClient(usr, pass, schema))

	badCredClient := tutils.NewHTTPClient("badUser", "badPass", schema)
	badCredClient.SetHostAndPort(client_.GetHostAndPort())

	noCredClient := tutils.NewHTTPClientNoAuth(schema)
	noCredClient.SetHostAndPort(client_.GetHostAndPort())

	cacheName := "test"

	createCacheBadCreds(cacheName, badCredClient)
	createCacheBadCreds(cacheName, noCredClient)

	cacheHelper := tutils.NewCacheHelper(cacheName, client_)
	cacheHelper.CreateWithDefault()
	defer cacheHelper.Delete()
	cacheHelper.TestBasicUsage("test", "test-operator")
}

func createCacheBadCreds(cacheName string, client tutils.HTTPClient) {
	err := ispnClient.New(client).Cache(cacheName).Create("", mime.ApplicationYaml)
	if err == nil {
		panic("Cache creation should fail")
	}
	var httpErr *httpClient.HttpError
	if !errors.As(err, &httpErr) {
		panic("Unexpected error type")
	}
	if httpErr.Status != http.StatusUnauthorized {
		panic(httpErr)
	}
}

func TestAuthenticationDisabled(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	})

	// Create the cluster
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure the identities secret is not created
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: tutils.Namespace,
		Name:      spec.GetSecretName(),
	}
	tutils.ExpectNotFound(testKube.Kubernetes.Client.Get(context.TODO(), key, secret))

	// Ensure that rest requests do not require authentication
	schema := testKube.GetSchemaForRest(spec)
	client_ := testKube.WaitForExternalService(spec, tutils.RouteTimeout, tutils.NewHTTPClientNoAuth(schema))
	rsp, err := client_.Get("rest/v2/caches", nil)
	tutils.ExpectNoError(err)
	if rsp.StatusCode != http.StatusOK {
		tutils.ThrowHTTPError(rsp)
	}
}

func TestEndpointAuthenticationUpdate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(true)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
	}
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	})
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestUpdateOperatorPassword(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube, nil)
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	newPassword := "supersecretoperatorpassword"
	secret, err := testKube.Kubernetes.GetSecret(spec.GetAdminSecretName(), spec.Namespace, context.TODO())
	tutils.ExpectNoError(err)
	_, err = kube.CreateOrPatch(context.TODO(), testKube.Kubernetes.Client, secret, func() error {
		secret.Data["password"] = []byte(newPassword)
		return nil
	})
	tutils.ExpectNoError(err)

	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (bool, error) {
		secret, err = testKube.Kubernetes.GetSecret(spec.GetAdminSecretName(), spec.Namespace, context.TODO())
		tutils.ExpectNoError(err)
		identities := secret.Data[cconsts.ServerIdentitiesFilename]
		pwd, err := users.FindPassword(spec.GetOperatorUser(), identities)
		tutils.ExpectNoError(err)
		fmt.Printf("Pwd=%s, Identities=%s", pwd, string(identities))
		return pwd == newPassword, nil
	})
	tutils.ExpectNoError(err)
}
