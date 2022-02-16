package main

import (
	"net/http"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	cconsts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAuthorizationDisabledByDefault(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := tutils.DefaultSpec(t, testKube)
	identities := func() users.Identities {
		return users.Identities{
			Credentials: []users.Credentials{{
				Username: "usr",
				Password: "pass",
				Roles:    []string{"monitor"},
			}},
		}
	}

	verify := func(client tutils.HTTPClient) {
		_, err := ispnClient.New(client).Caches().Names()
		tutils.ExpectNoError(err)
	}
	testAuthorization(ispn, identities, verify)
}

func TestAuthorizationWithCustomRoles(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := tutils.DefaultSpec(t, testKube)

	customRoleName := "custom-role"
	ispn.Spec.Security.Authorization = &v1.Authorization{
		Enabled: true,
		Roles: []ispnv1.AuthorizationRole{{
			Name:        customRoleName,
			Permissions: []string{"ALL"},
		}},
	}

	identities := func() users.Identities {
		return users.Identities{
			Credentials: []users.Credentials{
				{
					Username: "usr",
					Password: "pass",
					Roles:    []string{customRoleName},
				}, {
					Username: "monitor-user",
					Password: "pass",
					Roles:    []string{"monitor"},
				}, {
					// #1296 Add a user with no Roles defined to ensure that IDENTITIES_BATCH works as expected
					Username: "usr-no-role",
					Password: "pass",
				},
			},
		}
	}

	verify := func(client tutils.HTTPClient) {
		tutils.NewCacheHelper("succeed-cache", client).CreateWithDefault()
		schema := testKube.GetSchemaForRest(ispn)
		failClient := tutils.NewHTTPClient("monitor-user", "pass", schema)
		failClient.SetHostAndPort(client.GetHostAndPort())
		rsp, err := failClient.Post("rest/v2/caches/fail-cache", "", nil)
		tutils.ExpectNoError(err)
		if rsp.StatusCode != http.StatusForbidden {
			tutils.ThrowHTTPError(rsp)
		}
	}
	testAuthorization(ispn, identities, verify)
}

func testAuthorization(ispn *v1.Infinispan, createIdentities func() users.Identities, verify func(tutils.HTTPClient)) {
	namespace := tutils.Namespace
	secretName := ispn.Name + "-id-secret"
	ispn.Spec.Security.EndpointSecretName = secretName

	identities := createIdentities()
	identitiesYaml, err := yaml.Marshal(identities)
	tutils.ExpectNoError(err)

	// Create secret with application credentials
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{cconsts.ServerIdentitiesFilename: string(identitiesYaml)},
	}
	testKube.CreateSecret(&secret)
	defer testKube.DeleteSecret(&secret)

	// Create the cluster
	testKube.CreateInfinispan(ispn, namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	schema := testKube.GetSchemaForRest(ispn)
	user := identities.Credentials[0].Username
	pass := identities.Credentials[0].Password
	client_ := testKube.WaitForExternalService(ispn, tutils.RouteTimeout, tutils.NewHTTPClient(user, pass, schema))

	// Verify authorization works as expected
	verify(client_)
}
