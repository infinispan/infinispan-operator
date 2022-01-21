package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers"
	cconsts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func TestUpdateOperatorPassword(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
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
		pwd, err := users.FindPassword(cconsts.DefaultOperatorUser, identities)
		tutils.ExpectNoError(err)
		fmt.Printf("Pwd=%s, Identities=%s", pwd, string(identities))
		return pwd == newPassword, nil
	})
	tutils.ExpectNoError(err)
}

func TestUpdateEncryptionSecrets(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Replicas = 1
	spec.Spec.Security = ispnv1.InfinispanSecurity{
		EndpointEncryption: tutils.EndpointEncryption(spec.Name),
	}

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
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
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
	keystoreSecret.Data[controllers.EncryptPkcs12KeystoreName] = newKeystore
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

// Test if single node working correctly
func TestNodeStartup(t *testing.T) {
	// Create a resource without passing any config
	spec := tutils.DefaultSpec(testKube)
	spec.Annotations = make(map[string]string)
	spec.Annotations[v1.TargetLabels] = "my-svc-label"
	spec.Labels = make(map[string]string)
	spec.Labels["my-svc-label"] = "my-svc-value"
	tutils.ExpectNoError(os.Setenv(v1.OperatorTargetLabelsEnvVarName, "{\"operator-svc-label\":\"operator-svc-value\"}"))
	defer os.Unsetenv(v1.OperatorTargetLabelsEnvVarName)
	spec.Annotations[v1.PodTargetLabels] = "my-pod-label"
	spec.Labels["my-svc-label"] = "my-svc-value"
	spec.Labels["my-pod-label"] = "my-pod-value"
	tutils.ExpectNoError(os.Setenv(v1.OperatorPodTargetLabelsEnvVarName, "{\"operator-pod-label\":\"operator-pod-value\"}"))
	defer os.Unsetenv(v1.OperatorPodTargetLabelsEnvVarName)
	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	pod := corev1.Pod{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Name: spec.Name + "-0", Namespace: tutils.Namespace}, &pod))

	// Checking labels propagation to pods
	// from Infinispan CR to pods
	if pod.Labels["my-pod-label"] != ispn.Labels["my-pod-label"] {
		panic("Infinispan CR labels haven't been propagated to pods")
	}

	// from operator environment
	if tutils.RunLocalOperator == "TRUE" {
		// running locally, labels are hardcoded and set by the testsuite
		if pod.Labels["operator-pod-label"] != "operator-pod-value" ||
			ispn.Labels["operator-pod-label"] != "operator-pod-value" {
			panic("Infinispan CR labels haven't been propagated to pods")
		}
	} else {
		// Get the operator namespace from the env if it's different
		// from the testsuite one
		operatorNS := tutils.OperatorNamespace
		if operatorNS == "" {
			operatorNS = spec.Namespace
		}
		// operator deployed on cluster, labels are set by the deployment
		if !areOperatorLabelsPropagated(operatorNS, ispnv1.OperatorPodTargetLabelsEnvVarName, pod.Labels) {
			panic("Operator labels haven't been propagated to pods")
		}
	}

	svcList := &corev1.ServiceList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(ispn.Namespace, map[string]string{"infinispan_cr": "test-node-startup"}, svcList, context.TODO()))
	if len(svcList.Items) == 0 {
		panic("No services found for cluster")
	}
	for _, svc := range svcList.Items {
		// from Infinispan CR to service
		if svc.Labels["my-svc-label"] != ispn.Labels["my-svc-label"] {
			panic("Infinispan CR labels haven't been propagated to services")
		}

		// from operator environment
		if tutils.RunLocalOperator == "TRUE" {
			// running locally, labels are hardcoded and set by the testsuite
			if svc.Labels["operator-svc-label"] != "operator-svc-value" ||
				ispn.Labels["operator-svc-label"] != "operator-svc-value" {
				panic("Labels haven't been propagated to services")
			}
		} else {
			// Get the operator namespace from the env if it's different
			// from the testsuite one
			operatorNS := tutils.OperatorNamespace
			if operatorNS == "" {
				operatorNS = spec.Namespace
			}
			// operator deployed on cluster, labels are set by the deployment
			if !areOperatorLabelsPropagated(operatorNS, ispnv1.OperatorTargetLabelsEnvVarName, svc.Labels) {
				panic("Operator labels haven't been propagated to services")
			}
		}
	}
}

// areOperatorLabelsPropagated helper function that read the labels from the infinispan operator pod
// and match them with the labels map provided by the caller
func areOperatorLabelsPropagated(namespace, varName string, labels map[string]string) bool {
	podList := &corev1.PodList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(namespace, map[string]string{"name": tutils.OperatorName}, podList, context.TODO()))
	if len(podList.Items) == 0 {
		panic("Cannot get the Infinispan operator pod")
	}
	labelsAsString := ""
	for _, item := range podList.Items[0].Spec.Containers[0].Env {
		if item.Name == varName {
			labelsAsString = item.Value
		}
	}
	if labelsAsString == "" {
		return true
	}
	opLabels := make(map[string]string)
	if json.Unmarshal([]byte(labelsAsString), &opLabels) != nil {
		return true
	}
	for name, value := range opLabels {
		if labels[name] != value {
			return false
		}
	}
	return true
}

// Run some functions for testing rights not covered by integration tests
func TestRolesSynthetic(t *testing.T) {
	_, err := serviceAccountKube.Kubernetes.GetNodeHost(log, context.TODO())
	tutils.ExpectNoError(err)

	_, err = kube.FindStorageClass("not-present-storage-class", serviceAccountKube.Kubernetes.Client, context.TODO())
	if !k8sErrors.IsNotFound(err) {
		tutils.ExpectNoError(err)
	}
}

// Test if single node with n ephemeral storage
func TestNodeWithEphemeralStorage(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Service.Container = &ispnv1.InfinispanServiceContainerSpec{EphemeralStorage: true}
	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Making sure no PVCs were created
	pvcs := &corev1.PersistentVolumeClaimList{}
	err := testKube.Kubernetes.ResourcesList(spec.Namespace, controllers.PodLabels(spec.Name), pvcs, context.TODO())
	tutils.ExpectNoError(err)
	if len(pvcs.Items) > 0 {
		tutils.ExpectNoError(fmt.Errorf("persistent volume claims were found (count = %d) but not expected for ephemeral storage configuration", len(pvcs.Items)))
	}
}

// Test if the cluster is working correctly
func TestClusterFormation(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Replicas = 2
	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
}

// Test if the cluster is working correctly
func TestClusterFormationWithTLS(t *testing.T) {
	t.Parallel()

	// Create a resource without passing any config
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Replicas = 2
	spec.Spec.Security = ispnv1.InfinispanSecurity{
		EndpointEncryption: tutils.EndpointEncryption(spec.Name),
	}

	// Create secret with server certificates
	serverName := tutils.GetServerName(spec)
	cert, privKey, tlsConfig := tutils.CreateServerCertificates(serverName)
	secret := tutils.EncryptionSecret(spec.Name, tutils.Namespace, privKey, cert)
	testKube.CreateSecret(secret)
	defer testKube.DeleteSecret(secret)
	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure that we can connect to the endpoint with TLS
	client_ := tutils.HTTPSClientForCluster(spec, tlsConfig, testKube)
	checkRestConnection(client_)
}

// Test if the cluster is working correctly
func TestTLSWithExistingKeystore(t *testing.T) {
	t.Parallel()
	// Create a resource without passing any config
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Replicas = 1
	spec.Spec.Security = ispnv1.InfinispanSecurity{
		EndpointEncryption: tutils.EndpointEncryption(spec.Name),
	}
	// Create secret
	serverName := tutils.GetServerName(spec)
	keystore, tlsConfig := tutils.CreateKeystore(serverName)
	secret := tutils.EncryptionSecretKeystore(spec.Name, tutils.Namespace, keystore)
	testKube.CreateSecret(secret)
	defer testKube.DeleteSecret(secret)
	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure that we can connect to the endpoint with TLS
	client_ := tutils.HTTPSClientForCluster(spec, tlsConfig, testKube)
	checkRestConnection(client_)
}

func checkRestConnection(client tutils.HTTPClient) {
	_, err := ispnClient.New(client).Container().Members()
	tutils.ExpectNoError(err)
}

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
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Spec.Replicas = 1

	// Create the keystore & truststore for the server with a compatible client tls configuration
	authType, keystoreSecret, truststoreSecret, tlsConfig := initializer(spec)
	spec.Spec.Security.EndpointEncryption = tutils.EndpointEncryptionClientCert(spec.Name, authType)

	testKube.CreateSecret(keystoreSecret)
	defer testKube.DeleteSecret(keystoreSecret)

	testKube.CreateSecret(truststoreSecret)
	defer testKube.DeleteSecret(truststoreSecret)

	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure that we can connect to the endpoint with TLS
	client_ := tutils.HTTPSClientForCluster(spec, tlsConfig, testKube)
	tutils.NewCacheHelper("test", client_).CreateWithDefault()
}

// Test if spec.container.cpu update is handled
func TestContainerCPUUpdateWithTwoReplicas(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.CPU = "900m:550m"
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		limit := resource.MustParse("900m")
		request := resource.MustParse("550m")
		if limit.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"]) != 0 ||
			request.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"]) != 0 {
			panic("CPU field not updated")
		}
	}
	spec := tutils.MinimalSpec
	spec.Name = strcase.ToKebab(t.Name())
	spec.Labels = map[string]string{"test-name": t.Name()}
	genericTestForContainerUpdated(spec, modifier, verifier)
}

// Test if spec.container.memory update is handled
func TestContainerMemoryUpdate(t *testing.T) {
	t.Parallel()
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.Memory = "512Mi:256Mi"
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		limit := resource.MustParse("512Mi")
		request := resource.MustParse("256Mi")
		if limit.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Limits["memory"]) != 0 ||
			request.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Requests["memory"]) != 0 {
			panic("Memory field not updated")
		}
	}
	spec := tutils.DefaultSpec(testKube)
	spec.Name = strcase.ToKebab(t.Name())
	spec.Labels = map[string]string{"test-name": t.Name()}
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestContainerJavaOptsUpdate(t *testing.T) {
	t.Parallel()
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.ExtraJvmOpts = "-XX:NativeMemoryTracking=summary"
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		env := ss.Spec.Template.Spec.Containers[0].Env
		for _, value := range env {
			if value.Name == "JAVA_OPTIONS" {
				if value.Value != "-XX:NativeMemoryTracking=summary" {
					panic("JAVA_OPTIONS not updated")
				} else {
					return
				}
			}
		}
		panic("JAVA_OPTIONS not updated")
	}
	spec := tutils.DefaultSpec(testKube)
	spec.Name = strcase.ToKebab(t.Name())
	spec.Labels = map[string]string{"test-name": t.Name()}
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestEndpointAuthenticationUpdate(t *testing.T) {
	t.Parallel()
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(true)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
	}
	spec := tutils.DefaultSpec(testKube)
	spec.Name = strcase.ToKebab(t.Name())
	spec.Labels = map[string]string{"test-name": t.Name()}
	spec.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

func TestEndpointEncryptionUpdate(t *testing.T) {
	t.Parallel()
	spec := tutils.DefaultSpec(testKube)
	spec.Name = strcase.ToKebab(t.Name())
	spec.Labels = map[string]string{"test-name": t.Name()}
	spec.Spec.Security = ispnv1.InfinispanSecurity{
		EndpointEncryption: &ispnv1.EndpointEncryption{
			Type: ispnv1.CertificateSourceTypeNoneNoEncryption,
		},
	}

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

// Test if single node working correctly
func genericTestForContainerUpdated(ispn ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan, *appsv1.StatefulSet)) {
	testKube.CreateInfinispan(&ispn, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, ispn.Labels)
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	verifyStatefulSetUpdate(ispn, modifier, verifier)
}

func verifyStatefulSetUpdate(ispn ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan, *appsv1.StatefulSet)) {
	// Get the associate StatefulSet
	ss := appsv1.StatefulSet{}
	// Get the current generation
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetStatefulSetName()}, &ss))
	generation := ss.Status.ObservedGeneration

	tutils.ExpectNoError(testKube.UpdateInfinispan(&ispn, func() {
		modifier(&ispn)
	}))

	// Wait for a new generation to appear
	err := wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ss))
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	tutils.ExpectNoError(err)

	// Wait that current and update revisions match
	// this ensures that the rolling upgrade completes
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &ss))
		return ss.Status.CurrentRevision == ss.Status.UpdateRevision, nil
	})
	tutils.ExpectNoError(err)

	// Check that the update has been propagated
	verifier(&ispn, &ss)
}

func TestCacheService(t *testing.T) {
	t.Parallel()
	testCacheService(t.Name())
}

func testCacheService(testName string) {
	spec := tutils.DefaultSpec(testKube)
	spec.Name = strcase.ToKebab(testName)
	spec.Spec.Service.Type = ispnv1.ServiceTypeCache
	spec.Spec.Expose = tutils.ExposeServiceSpec(testKube)

	spec.Labels = map[string]string{"test-name": testName}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper("default", client_)
	cacheHelper.WaitForCacheToExist()
	cacheHelper.TestBasicUsage("test", "test-operator")
}

// TestPermanentCache creates a permanent cache the stop/start
// the cluster and checks that the cache is still there
func TestPermanentCache(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	cacheName := "test"
	// Define function for the generic stop/start test procedure
	var createPermanentCache = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		tutils.NewCacheHelper(cacheName, client_).CreateWithDefault("PERMANENT")
	}

	var usePermanentCache = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		key := "test"
		value := "test-operator"
		cacheHelper := tutils.NewCacheHelper(cacheName, client_)
		cacheHelper.TestBasicUsage(key, value)
		cacheHelper.Delete()
	}

	genericTestForGracefulShutdown(name, createPermanentCache, usePermanentCache)
}

// TestCheckDataSurviveToShutdown creates a cache with file-store the stop/start
// the cluster and checks that the cache and the data are still there
func TestCheckDataSurviveToShutdown(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	cacheName := "test"
	template := `<infinispan><cache-container><distributed-cache name ="` + cacheName +
		`"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>`
	key := "test"
	value := "test-operator"

	// Define function for the generic stop/start test procedure
	var createCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(cacheName, client_)
		cacheHelper.Create(template, mime.ApplicationXml)
		cacheHelper.Put(key, value, mime.TextPlain)
	}

	var useCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(cacheName, client_)
		actual, _ := cacheHelper.Get(key)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		cacheHelper.Delete()
	}

	genericTestForGracefulShutdown(name, createCacheWithFileStore, useCacheWithFileStore)
}

func genericTestForGracefulShutdown(clusterName string, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan)) {
	// Create a resource without passing any config
	// Register it
	spec := tutils.DefaultSpec(testKube)
	spec.Name = clusterName
	spec.Labels = map[string]string{"test-name": clusterName}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Do something that needs to be permanent
	modifier(ispn)

	// Delete the cluster
	testKube.GracefulShutdownInfinispan(spec)
	testKube.GracefulRestartInfinispan(spec, 1, tutils.SinglePodTimeout)

	// Do something that checks that permanent changes are there again
	verifier(ispn)
}

func TestExternalService(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())

	// Create a resource without passing any config
	spec := ispnv1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    tutils.CPU,
				Memory: tutils.Memory,
			},
			Replicas: 1,
			Expose:   tutils.ExposeServiceSpec(testKube),
		},
	}

	// Register it
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(&spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)

	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper("test-cache", client_)
	cacheHelper.CreateWithDefault()
	cacheHelper.TestBasicUsage("test", "test-operator")
}

// TestExternalServiceWithAuth starts a cluster and checks application
// and management connection with authentication
func TestExternalServiceWithAuth(t *testing.T) {
	t.Parallel()
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

	name := strcase.ToKebab(t.Name())

	// Create Infinispan
	spec := ispnv1.Infinispan{
		TypeMeta: tutils.InfinispanTypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Security: ispnv1.InfinispanSecurity{EndpointSecretName: "conn-secret-test"},
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    tutils.CPU,
				Memory: tutils.Memory,
			},
			Replicas: 1,
			Expose:   tutils.ExposeServiceSpec(testKube),
		},
	}
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(&spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)

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

	err = testKube.UpdateInfinispan(&spec, func() {
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
	// The restart is ongoing, and it would that more than 10 sec,
	// so we're not introducing any delay
	time.Sleep(10 * time.Second)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
	testAuthentication(ispn, schema, usr, newpass)
}

func testAuthentication(ispn *ispnv1.Infinispan, schema, usr, pass string) {
	client_ := testKube.WaitForExternalService(ispn, tutils.RouteTimeout, tutils.NewHTTPClient(usr, pass, schema))
	badClient := tutils.NewHTTPClient("badUser", "badPass", schema)
	badClient.SetHostAndPort(client_.GetHostAndPort())

	cacheName := "test"
	createCacheBadCreds(cacheName, badClient)
	cacheHelper := tutils.NewCacheHelper(cacheName, client_)
	cacheHelper.CreateWithDefault()
	defer cacheHelper.Delete()
	cacheHelper.TestBasicUsage("test", "test-operator")
}

func TestAuthenticationDisabled(t *testing.T) {
	t.Parallel()
	namespace := tutils.Namespace
	// Create a resource without passing any config
	name := strcase.ToKebab(t.Name())
	spec := tutils.DefaultSpec(testKube)
	spec.Name = name
	spec.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)

	// Create the cluster
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, name, namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Ensure the identities secret is not created
	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: namespace,
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

func TestAuthorizationDisabledByDefault(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	ispn := tutils.DefaultSpec(testKube)
	ispn.Name = name
	ispn.Labels = map[string]string{"test-name": t.Name()}

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
	name := strcase.ToKebab(t.Name())
	ispn := tutils.DefaultSpec(testKube)

	customRoleName := "custom-role"
	ispn.Name = name
	ispn.Labels = map[string]string{"test-name": t.Name()}
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
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, ispn.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	schema := testKube.GetSchemaForRest(ispn)
	user := identities.Credentials[0].Username
	pass := identities.Credentials[0].Password
	client_ := testKube.WaitForExternalService(ispn, tutils.RouteTimeout, tutils.NewHTTPClient(user, pass, schema))

	// Verify authorization works as expected
	verify(client_)
}

func TestExternalDependenciesHttp(t *testing.T) {
	if os.Getenv("NO_NGINX") != "" {
		t.Skip("Skipping test, no Nginx available.")
	}
	webServerConfig := prepareWebServer()
	defer testKube.DeleteResource(tutils.Namespace, labels.SelectorFromSet(map[string]string{"app": tutils.WebServerName}), webServerConfig, tutils.SinglePodTimeout)

	namespace := tutils.Namespace
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Labels = map[string]string{"test-name": t.Name()}
	spec.Spec.Dependencies = &ispnv1.InfinispanExternalDependencies{
		Artifacts: []ispnv1.InfinispanExternalArtifacts{
			{Url: fmt.Sprintf("http://%s:%d/task01-1.0.0.jar", tutils.WebServerName, tutils.WebServerPortNumber)},
			{Url: fmt.Sprintf("http://%s:%d/task02-1.0.0.zip", tutils.WebServerName, tutils.WebServerPortNumber)},
		},
	}

	// Create the cluster
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, name, namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	client_ := tutils.HTTPClientForCluster(ispn, testKube)

	validateTaskExecution := func(task, param string, status int, result string) {
		url := fmt.Sprintf("rest/v2/tasks/%s?action=exec&param.name=%s", task, param)
		resp, err := client_.Post(url, "", nil)
		tutils.ExpectNoError(err)
		defer func(Body io.ReadCloser) {
			tutils.ExpectNoError(Body.Close())
		}(resp.Body)
		if resp.StatusCode != status {
			panic(fmt.Sprintf("Unexpected response code %d for the Server Task execution", resp.StatusCode))
		}
		if resp.StatusCode == http.StatusOK {
			body, err := ioutil.ReadAll(resp.Body)
			tutils.ExpectNoError(err)
			if string(body) != result {
				panic(fmt.Sprintf("Unexpected task %s response '%s' from the Server Task", task, string(body)))
			}
		}
	}

	for _, task := range []string{"01", "02"} {
		validateTaskExecution("task-"+task, "World", http.StatusOK, "Hello World")
	}

	var externalLibraryAddModify = func(ispn *ispnv1.Infinispan) {
		libs := &ispn.Spec.Dependencies.Artifacts
		*libs = append(*libs, ispnv1.InfinispanExternalArtifacts{Url: fmt.Sprintf("http://%s:%d/task03-1.0.0.tar.gz", tutils.WebServerName, tutils.WebServerPortNumber)})
	}
	var externalLibraryAddVerify = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
		validateTaskExecution("task-03", "World", http.StatusOK, "Hello World")
	}
	verifyStatefulSetUpdate(*ispn, externalLibraryAddModify, externalLibraryAddVerify)

	var externalLibraryHashModify = func(ispn *ispnv1.Infinispan) {
		for taskName, taskData := range webServerConfig.BinaryData {
			for artifactIndex, artifact := range ispn.Spec.Dependencies.Artifacts {
				if strings.Contains(artifact.Url, taskName) {
					ispn.Spec.Dependencies.Artifacts[artifactIndex].Hash = fmt.Sprintf("sha1:%s", hash.HashByte(taskData))
				}
			}
		}
	}

	var externalLibraryHashVerify = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
		for _, task := range []string{"01", "02", "03"} {
			validateTaskExecution("task-"+task, "World", http.StatusOK, "Hello World")
		}
	}

	verifyStatefulSetUpdate(*ispn, externalLibraryHashModify, externalLibraryHashVerify)

	var externalLibraryFailHashModify = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Dependencies.Artifacts[1].Hash = fmt.Sprintf("sha1:%s", "failhash")
	}

	tutils.ExpectNoError(testKube.UpdateInfinispan(ispn, func() {
		externalLibraryFailHashModify(ispn)
	}))

	podList := &corev1.PodList{}
	tutils.ExpectNoError(wait.Poll(tutils.DefaultPollPeriod, tutils.SinglePodTimeout, func() (done bool, err error) {
		err = testKube.Kubernetes.ResourcesList(ispn.Namespace, controllers.PodLabels(ispn.Name), podList, context.TODO())
		if err != nil {
			return false, nil
		}
		for _, pod := range podList.Items {
			if kube.InitContainerFailed(pod.Status.InitContainerStatuses) {
				return true, nil
			}

		}
		return false, nil
	}))

	var externalLibraryRemoveModify = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Dependencies = nil
	}
	var externalLibraryRemoveVerify = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
		for _, task := range []string{"01", "02", "03"} {
			validateTaskExecution("task-"+task, "", http.StatusBadRequest, "")
		}
	}
	verifyStatefulSetUpdate(*ispn, externalLibraryRemoveModify, externalLibraryRemoveVerify)
}

func prepareWebServer() *corev1.ConfigMap {
	webServerConfig := &corev1.ConfigMap{}
	testKube.LoadResourceFromYaml("../utils/data/external-libs-config.yaml", webServerConfig)
	webServerConfig.Namespace = tutils.Namespace
	testKube.Create(webServerConfig)

	webServerPodConfig := tutils.WebServerPod(tutils.WebServerName, tutils.Namespace, webServerConfig.Name, tutils.WebServerRootFolder, tutils.WebServerImageName)
	tutils.ExpectNoError(controllerutil.SetControllerReference(webServerConfig, webServerPodConfig, tutils.Scheme))
	testKube.Create(webServerPodConfig)

	webServerService := tutils.WebServerService(tutils.WebServerName, tutils.Namespace)
	tutils.ExpectNoError(controllerutil.SetControllerReference(webServerConfig, webServerService, tutils.Scheme))
	testKube.Create(webServerService)

	testKube.WaitForPods(1, tutils.SinglePodTimeout, &client.ListOptions{Namespace: tutils.Namespace, LabelSelector: labels.SelectorFromSet(map[string]string{"app": tutils.WebServerName})}, nil)
	return webServerConfig
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

func TestPodDegradationAfterOOM(t *testing.T) {
	t.Parallel()

	//Creating Infinispan cluster
	ispn := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	ispn.Name = name
	ispn.Labels = map[string]string{"test-name": t.Name()}
	ispn.Spec.Replicas = 2
	ispn.Spec.Container.Memory = "256Mi"

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, ispn.Labels)
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	//Creating cache
	cacheName := "failover-cache"
	template := `<replicated-cache name ="` + cacheName + `"><encoding media-type="text/plain"/></replicated-cache>`
	veryLongValue := GenerateStringWithCharset(100000)
	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client_)
	cacheHelper.Create(template, mime.ApplicationXml)

	client_.Quiet(true)
	//Generate tons of random entries
	for key := 1; key < 50000; key++ {
		strKey := strconv.Itoa(key)
		if err := cacheHelper.CacheClient.Put(strKey, veryLongValue, mime.TextPlain); err != nil {
			fmt.Printf("ERROR for key=%d, Description=%s\n", key, err)
			break
		}
	}
	client_.Quiet(false)

	//Check if all pods are running, and they are not degraded
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	//Verify whether the pod restarted for an OOM exception
	hasOOMhappened := false
	podList := &corev1.PodList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(tutils.Namespace, controllers.PodLabels(ispn.Name), podList, context.TODO()))

	for _, pod := range podList.Items {
		status := pod.Status.ContainerStatuses

	out:
		for _, containerStatuses := range status {
			if containerStatuses.LastTerminationState.Terminated != nil {
				terminatedPod := containerStatuses.LastTerminationState.Terminated

				if terminatedPod.Reason == "OOMKilled" {
					hasOOMhappened = true
					fmt.Printf("ExitCode='%d' Reason='%s' Message='%s'\n", terminatedPod.ExitCode, terminatedPod.Reason, terminatedPod.Message)
					break out
				}
			}
		}
	}

	if kube.AreAllPodsReady(podList) && hasOOMhappened {
		fmt.Println("All pods are ready")
	} else if kube.AreAllPodsReady(podList) && !hasOOMhappened {
		panic("Test finished without an OutOfMemory occurred")
	} else {
		panic("One of the pods is degraded")
	}
}

func GenerateStringWithCharset(length int) string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var seededRand = rand.New(rand.NewSource(time.Now().UnixNano()))

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// Test custom configuration with cache-container element
func TestUserXmlCustomConfig(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "xml")
	testCustomConfig(t, configMap)
}

func TestUserYamlCustomConfig(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "yaml")
	testCustomConfig(t, configMap)
}

func TestUserJsonCustomConfig(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "json")
	testCustomConfig(t, configMap)
}

func testCustomConfig(t *testing.T, configMap *corev1.ConfigMap) {
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	// Create a resource without passing any config
	ispn := tutils.DefaultSpec(testKube)
	ispn.Spec.ConfigMapName = configMap.Name
	ispn.Name = strcase.ToKebab(t.Name())
	// Register it
	ispn.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, ispn.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
	cacheHelper.TestBasicUsage("testkey", "test-operator")
}

// TestUserCustomConfigWithAuthUpdate tests that user custom config works well with update
// using authentication update to trigger a cluster update
func TestUserCustomConfigWithAuthUpdate(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	var modifier = func(ispn *ispnv1.Infinispan) {
		// testing cache pre update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
		ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(true)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(testKube)
	ispn.Name = strcase.ToKebab(t.Name())
	ispn.Labels = map[string]string{"test-name": t.Name()}
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	ispn.Spec.ConfigMapName = configMap.Name
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

// TestUserCustomConfigUpdateOnNameChange tests that user custom config works well with user config update
func TestUserCustomConfigUpdateOnNameChange(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)
	configMapChanged := newCustomConfigMap(t.Name()+"Changed", "xml")
	testKube.Create(configMapChanged)
	defer testKube.DeleteConfigMap(configMapChanged)

	var modifier = func(ispn *ispnv1.Infinispan) {
		// testing cache pre update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
		ispn.Spec.ConfigMapName = configMapChanged.Name
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name()+"Changed", client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(testKube)
	ispn.Name = strcase.ToKebab(t.Name())
	ispn.Labels = map[string]string{"test-name": t.Name()}
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	ispn.Spec.ConfigMapName = configMap.Name
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

func TestUserCustomConfigUpdateOnChange(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	newCacheName := t.Name() + "Updated"
	var modifier = func(ispn *ispnv1.Infinispan) {
		// testing cache pre update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
		configMapUpdated := newCustomConfigMap(newCacheName, "xml")
		// Reuse old name to test CM in-place update
		configMapUpdated.Name = strcase.ToKebab(t.Name())
		testKube.UpdateConfigMap(configMapUpdated)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(newCacheName, client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(testKube)
	ispn.Name = strcase.ToKebab(t.Name())
	ispn.Labels = map[string]string{"test-name": t.Name()}
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	ispn.Spec.ConfigMapName = configMap.Name
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

// TestUserCustomConfigUpdateOnAdd tests that user custom config works well with user config update
func TestUserCustomConfigUpdateOnAdd(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	var modifier = func(ispn *ispnv1.Infinispan) {
		tutils.ExpectNoError(testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.ConfigMapName = configMap.Name
		}))
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(testKube)
	ispn.Name = strcase.ToKebab(t.Name())
	ispn.Labels = map[string]string{"test-name": t.Name()}
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

func newCustomConfigMap(name, format string) *corev1.ConfigMap {
	var userCacheContainer string
	switch format {
	case "xml":
		userCacheContainer = `<infinispan
xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
xsi:schemaLocation="urn:infinispan:config:13.0 http://www.infinispan.org/schemas/infinispan-config-13.0.xsd
					urn:infinispan:server:13.0 http://www.infinispan.org/schemas/infinispan-server-13.0.xsd"
xmlns="urn:infinispan:config:13.0"
xmlns:server="urn:infinispan:server:13.0">
	<cache-container name="default" statistics="true">
		<distributed-cache name="` + name + `"/>
	</cache-container>
</infinispan>`
	case "yaml":
		userCacheContainer = `infinispan:
  cacheContainer:
  	name: default
		distributedCache:
			name: ` + name
	case "json":
		userCacheContainer = `{ "infinispan": { "cacheContainer": { "name": "default", "distributedCache": { "name": "` + name + `"}}}}`
	}

	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: strcase.ToKebab(name),
		Namespace: tutils.Namespace},
		Data: map[string]string{"infinispan-config." + format: userCacheContainer},
	}
}

func TestConfigListenerDeployment(t *testing.T) {
	// t.Parallel()
	ispn := tutils.DefaultSpec(testKube)
	ispn.Name = strcase.ToKebab(t.Name())
	ispn.Labels = map[string]string{"test-name": t.Name()}
	ispn.Spec.ConfigListener = &v1.ConfigListenerSpec{
		Enabled: true,
	}

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(ispn.Namespace, ispn.Labels)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	// Wait for ConfigListener Deployment to be created
	clName, namespace := ispn.GetConfigListenerName(), ispn.Namespace
	testKube.WaitForDeployment(clName, namespace)

	waitForNoConfigListener := func() {
		err := wait.Poll(tutils.ConditionPollPeriod, tutils.ConditionWaitTimeout, func() (bool, error) {
			exists := testKube.AssertK8ResourceExists(clName, namespace, &appsv1.Deployment{}) &&
				testKube.AssertK8ResourceExists(clName, namespace, &rbacv1.Role{}) &&
				testKube.AssertK8ResourceExists(clName, namespace, &rbacv1.RoleBinding{}) &&
				testKube.AssertK8ResourceExists(clName, namespace, &corev1.ServiceAccount{})
			return !exists, nil
		})
		tutils.ExpectNoError(err)
	}

	// Ensure that the deployment is deleted if the spec is updated
	err := testKube.UpdateInfinispan(ispn, func() {
		ispn.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: false,
		}
	})
	tutils.ExpectNoError(err)
	waitForNoConfigListener()

	// Re-add the ConfigListener to ensure that it's removed when the Infinispan CR is finally deleted
	err = testKube.UpdateInfinispan(ispn, func() {
		ispn.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: true,
		}
	})
	tutils.ExpectNoError(err)
	testKube.WaitForDeployment(clName, namespace)

	// Ensure that deployment is deleted with the Infinispan CR
	testKube.DeleteInfinispan(ispn)
	waitForNoConfigListener()
}
