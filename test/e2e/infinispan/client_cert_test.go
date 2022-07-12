package infinispan

import (
	"crypto/tls"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

func TestClientCertValidate(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		authType = ispnv1.ClientCertValidate
		serverName := tutils.GetServerName(spec)
		keystore, truststore, tlsConfig := tutils.CreateKeyAndTruststore(serverName, false)
		keystoreSecret = tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
		truststoreSecret = tutils.EncryptionSecretClientTrustore(spec.Name, tutils.Namespace, truststore)
		return
	})
}

func TestClientCertValidateNoAuth(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		spec.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
		authType = ispnv1.ClientCertValidate
		serverName := tutils.GetServerName(spec)
		keystore, truststore, tlsConfig := tutils.CreateKeyAndTruststore(serverName, false)
		keystoreSecret = tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
		truststoreSecret = tutils.EncryptionSecretClientTrustore(spec.Name, tutils.Namespace, truststore)
		return
	})
}

func TestClientCertAuthenticate(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		authType = ispnv1.ClientCertAuthenticate
		serverName := tutils.GetServerName(spec)
		keystore, truststore, tlsConfig := tutils.CreateKeyAndTruststore(serverName, true)
		keystoreSecret = tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
		truststoreSecret = tutils.EncryptionSecretClientTrustore(spec.Name, tutils.Namespace, truststore)
		return
	})
}

func TestClientCertWithKeyCrtFiles(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		authType = ispnv1.ClientCertAuthenticate
		serverName := tutils.GetServerName(spec)
		keyCertPair, truststore, tlsConfig := tutils.CreateKeyCertAndTruststore(serverName, true)
		keystoreSecret = tutils.EncryptionSecret(spec.Name, tutils.Namespace, keyCertPair.PrivateKey, keyCertPair.Certificate)
		truststoreSecret = tutils.EncryptionSecretClientTrustore(spec.Name, tutils.Namespace, truststore)
		return
	})
}

func TestClientCertValidateWithAuthorization(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		spec.Spec.Security.Authorization = &v1.Authorization{
			Enabled: true,
			Roles: []ispnv1.AuthorizationRole{
				{
					Name:        "client",
					Permissions: []string{"ALL"},
				},
			},
		}
		authType = ispnv1.ClientCertValidate
		serverName := tutils.GetServerName(spec)
		keystore, truststore, tlsConfig := tutils.CreateKeyAndTruststore(serverName, false)
		keystoreSecret = tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
		truststoreSecret = tutils.EncryptionSecretClientTrustore(spec.Name, tutils.Namespace, truststore)
		return
	})
}

func TestClientCertAuthenticateWithAuthorization(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		spec.Spec.Security.Authorization = &v1.Authorization{
			Enabled: true,
			Roles: []ispnv1.AuthorizationRole{
				{
					Name:        "client",
					Permissions: []string{"ALL"},
				},
			},
		}
		authType = ispnv1.ClientCertAuthenticate
		serverName := tutils.GetServerName(spec)
		keystore, truststore, tlsConfig := tutils.CreateKeyAndTruststore(serverName, true)
		keystoreSecret = tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
		truststoreSecret = tutils.EncryptionSecretClientTrustore(spec.Name, tutils.Namespace, truststore)
		return
	})
}

func TestClientCertGeneratedTruststoreAuthenticate(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		authType = ispnv1.ClientCertAuthenticate
		serverName := tutils.GetServerName(spec)
		keystore, caCert, clientCert, tlsConfig := tutils.CreateKeystoreAndClientCerts(serverName)
		keystoreSecret = tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
		truststoreSecret = tutils.EncryptionSecretClientCert(spec.Name, tutils.Namespace, caCert, clientCert)
		return
	})
}

func TestClientCertGeneratedTruststoreValidate(t *testing.T) {
	testClientCert(t, func(spec *v1.Infinispan) (authType ispnv1.ClientCertType, keystoreSecret, truststoreSecret *corev1.Secret, tlsConfig *tls.Config) {
		authType = ispnv1.ClientCertValidate
		serverName := tutils.GetServerName(spec)
		keystore, caCert, _, tlsConfig := tutils.CreateKeystoreAndClientCerts(serverName)
		keystoreSecret = tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
		truststoreSecret = tutils.EncryptionSecretClientCert(spec.Name, tutils.Namespace, caCert, nil)
		return
	})
}

func testClientCert(t *testing.T, initializer func(*v1.Infinispan) (v1.ClientCertType, *corev1.Secret, *corev1.Secret, *tls.Config)) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	spec := tutils.DefaultSpec(t, testKube, nil)

	// Create the keystore & truststore for the server with a compatible client tls configuration
	authType, keystoreSecret, truststoreSecret, tlsConfig := initializer(spec)
	spec.Spec.Security.EndpointEncryption = tutils.EndpointEncryptionClientCert(spec.Name, authType)

	testKube.CreateSecret(keystoreSecret)
	defer testKube.DeleteSecret(keystoreSecret)

	testKube.CreateSecret(truststoreSecret)
	defer testKube.DeleteSecret(truststoreSecret)

	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure that we can connect to the endpoint with TLS
	client_ := tutils.HTTPSClientForCluster(spec, tlsConfig, testKube)
	tutils.NewCacheHelper("test", client_).CreateWithDefault()

	// Scale the cluster down to ensure that Operator authorization works as expected
	ispn.Spec.Replicas = 0
	testKube.Update(ispn)
	testKube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
}
