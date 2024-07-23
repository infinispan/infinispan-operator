package infinispan

import (
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var config = `
<infinispan
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
    xsi:schemaLocation="urn:infinispan:config:14.0 https://infinispan.org/schemas/infinispan-config-14.0.xsd
                        urn:infinispan:server:14.0 https://infinispan.org/schemas/infinispan-server-14.0.xsd
                        urn:org:jgroups http://www.jgroups.org/schema/jgroups-5.2.xsd"
    xmlns="urn:infinispan:config:14.0"
    xmlns:server="urn:infinispan:server:14.0">

    <server xmlns="urn:infinispan:server:14.0">
        <data-sources>
          <data-source name="postgres" jndi-name="jdbc/postgres">
            <connection-factory driver="org.postgresql.Driver"
                                username="dbuser"
                                url="${org.infinispan.server.test.postgres.jdbcUrl}">
              <credential-reference store="credentials"
                                    alias="dbpassword"/>
            </connection-factory>
            <connection-pool max-size="10"
                             min-size="1"
                             background-validation="1000"
                             idle-removal="1"
                             initial-size="1"
                             leak-detection="10000"/>
          </data-source>
        </data-sources>
      </server>
</infinispan>
`

func TestCredentialStoreEntries(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "credential-store-secret",
			Namespace: tutils.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"dbpassword":   "changeme",
			"ldappassword": "changeme2",
		},
	}
	testKube.CreateSecret(secret)
	defer testKube.DeleteSecret(secret)

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "credential-store-config",
			Namespace: tutils.Namespace,
		},
		Data: map[string]string{
			"infinispan-config.xml": config,
		},
	}
	testKube.CreateConfigMap(configMap)
	defer testKube.DeleteConfigMap(configMap)

	var modifier = func(ispn *ispnv1.Infinispan) {
		assert.Contains(t, credentials(ispn), "dbpassword")
		assert.Contains(t, credentials(ispn), "ldappassword")
		secret = testKube.GetSecret(secret.Name, secret.Namespace)
		delete(secret.Data, "ldappassword")
		secret.Data["dbpassword2"] = []byte("a new one")
		testKube.UpdateSecret(secret)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		i := testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		assert.Contains(t, credentials(i), "dbpassword2")
		assert.NotContains(t, credentials(i), "ldappassword")
	}
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Security.CredentialStoreSecretName = secret.Name
		i.Spec.ConfigMapName = configMap.Name
		i.Spec.Dependencies = &ispnv1.InfinispanExternalDependencies{
			Artifacts: []ispnv1.InfinispanExternalArtifacts{{Maven: "org.postgresql:postgresql:42.5.1"}},
		}
	})
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func credentials(i *ispnv1.Infinispan) string {
	execOut, err := testKube.Kubernetes.ExecWithOptions(
		kube.ExecOptions{
			Container: provision.InfinispanContainer,
			Command:   []string{"bash", "-c", "./bin/cli.sh credentials ls -p secret"},
			PodName:   i.Status.PodStatus.Ready[0],
			Namespace: tutils.Namespace,
		},
	)
	tutils.ExpectNoError(err)
	return execOut.String()
}
