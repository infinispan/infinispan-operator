package infinispan

import (
	"bytes"
	"context"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	cconsts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// Test if the cluster is working correctly
func TestTLSWithExistingKeystore(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Security = ispnv1.InfinispanSecurity{
			EndpointEncryption: tutils.EndpointEncryption(i.Name),
		}
	})

	// Create secret
	serverName := tutils.GetServerName(spec)
	keystore, tlsConfig := tutils.CreateKeystore(serverName)
	secret := tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
	testKube.CreateSecret(secret)
	defer testKube.DeleteSecret(secret)

	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionTLSSecretValid)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure that we can connect to the endpoint with TLS
	client_ := tutils.HTTPSClientForCluster(spec, tlsConfig, testKube)
	checkRestConnection(client_)
}

func TestTLSConditionWithBadKeystore(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Security = ispnv1.InfinispanSecurity{
			EndpointEncryption: tutils.EndpointEncryption(i.Name),
		}
	})

	// Create secret
	serverName := tutils.GetServerName(spec)
	keystore, _ := tutils.CreateKeystore(serverName)
	secret := tutils.EncryptionBadSecretKeystore(spec.Name, tutils.Namespace, keystore)
	testKube.CreateSecret(secret)
	defer testKube.DeleteSecret(secret)

	// Register it
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanConditionFalse(spec.Name, spec.Namespace, ispnv1.ConditionTLSSecretValid)
}

func checkRestConnection(client tutils.HTTPClient) {
	_, err := ispnClient.New(tutils.LatestOperand, client).Container().Members()
	tutils.ExpectNoError(err)
}

func TestEndpointEncryptionUpdate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Security = ispnv1.InfinispanSecurity{
			EndpointEncryption: &ispnv1.EndpointEncryption{
				Type: ispnv1.CertificateSourceTypeNoneNoEncryption,
			},
		}
	})

	// Create secret with server certificates
	serverName := tutils.GetServerName(spec)
	cert, privKey, tlsConfig := tutils.CreateServerCertificates(serverName)
	secret := tutils.EncryptionSecret(spec.Name, tutils.Namespace, privKey, cert)
	testKube.CreateSecret(secret)
	defer testKube.DeleteSecret(secret)

	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Security = ispnv1.InfinispanSecurity{
			EndpointEncryption: tutils.EndpointEncryption(spec.Name),
		}
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
		// Ensure that we can connect to the endpoint with TLS
		client_ := tutils.HTTPSClientForCluster(spec, tlsConfig, testKube)
		checkRestConnection(client_)
	}

	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestUpdateEncryptionSecrets(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Security = ispnv1.InfinispanSecurity{
			EndpointEncryption: tutils.EndpointEncryption(i.Name),
		}
	})

	// Create secret
	serverName := tutils.GetServerName(spec)
	keystore, truststore, tlsConfig := tutils.CreateKeyAndTruststore(serverName, false)
	keystoreSecret := tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
	truststoreSecret := tutils.EncryptionSecretClientTrustore(spec.Name, tutils.Namespace, truststore)
	testKube.CreateSecret(keystoreSecret)
	defer testKube.DeleteSecret(keystoreSecret)
	testKube.CreateSecret(truststoreSecret)
	defer testKube.DeleteSecret(truststoreSecret)

	// Create Cluster
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure that we can connect to the endpoint with TLS
	client_ := tutils.HTTPSClientForCluster(spec, tlsConfig, testKube)
	checkRestConnection(client_)

	namespacedName := types.NamespacedName{Namespace: spec.Namespace, Name: spec.GetStatefulSetName()}
	// Get the cluster's StatefulSet and current generation
	ss := appsv1.StatefulSet{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), namespacedName, &ss))
	originalGeneration := ss.Status.ObservedGeneration

	// Update secret to contain new keystore
	newKeystore, newTruststore, newTlsConfig := tutils.CreateKeyAndTruststore(serverName, false)
	if bytes.Equal(keystore, newKeystore) || bytes.Equal(truststore, newTruststore) {
		panic("Expected new store")
	}

	keystoreSecret = testKube.GetSecret(keystoreSecret.Name, keystoreSecret.Namespace)
	keystoreSecret.Data["keystore.p12"] = newKeystore
	testKube.UpdateSecret(keystoreSecret)

	truststoreSecret = testKube.GetSecret(truststoreSecret.Name, truststoreSecret.Namespace)
	keystoreSecret.Data[cconsts.EncryptTruststoreKey] = newKeystore
	testKube.UpdateSecret(truststoreSecret)

	// Wait for a new generation to appear
	err := wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), namespacedName, &ss))
		return ss.Status.ObservedGeneration >= originalGeneration+1, nil
	})
	tutils.ExpectNoError(err)

	// Wait that current and update revisions match. This ensures that the rolling upgrade completes
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), namespacedName, &ss))
		return ss.Status.CurrentRevision == ss.Status.UpdateRevision, nil
	})
	tutils.ExpectNoError(err)

	// Ensure that we can connect to the endpoint with the new TLS settings
	client_ = tutils.HTTPSClientForCluster(spec, newTlsConfig, testKube)
	checkRestConnection(client_)
}
