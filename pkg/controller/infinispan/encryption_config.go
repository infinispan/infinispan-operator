package infinispan

import (
	"fmt"
	"strings"

	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	EncryptKeystoreName = "keystore.p12"
	EncryptKeystorePath = ServerRoot + "/conf/keystore"
)

func ConfigureServerEncryption(i *v1.Infinispan, c *config.InfinispanConfiguration, client client.Client) (*reconcile.Result, error) {
	if !i.IsEncryptionEnabled() {
		return nil, nil
	}

	secretContains := func(secret *corev1.Secret, keys ...string) bool {
		for _, k := range keys {
			if _, ok := secret.Data[k]; !ok {
				return false
			}
		}
		return true
	}

	configureNewKeystore := func(c *config.InfinispanConfiguration) {
		c.Keystore.CrtPath = consts.ServerEncryptKeystoreRoot
		c.Keystore.Path = EncryptKeystorePath
		c.Keystore.Password = "password"
		c.Keystore.Alias = "server"
	}

	// Configure Keystore
	keystoreSecret := &corev1.Secret{}
	if result, err := kube.LookupResource(i.GetKeystoreSecretName(), i.Namespace, keystoreSecret, i, client, log, eventRec); result != nil {
		return result, err
	}

	if i.IsEncryptionCertFromService() {
		if strings.Contains(i.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			configureNewKeystore(c)
		}
	} else {
		if secretContains(keystoreSecret, EncryptKeystoreName) {
			// If user provide a keystore in secret then use it ...
			c.Keystore.Path = fmt.Sprintf("%s/%s", consts.ServerEncryptKeystoreRoot, EncryptKeystoreName)
			c.Keystore.Password = string(keystoreSecret.Data["password"])
			c.Keystore.Alias = string(keystoreSecret.Data["alias"])
		} else if secretContains(keystoreSecret, corev1.TLSPrivateKeyKey, corev1.TLSCertKey) {
			configureNewKeystore(c)
		}
	}

	// Configure Truststore
	if i.IsClientCertEnabled() {
		trustSecret := &corev1.Secret{}
		if result, err := kube.LookupResource(i.GetTruststoreSecretName(), i.Namespace, trustSecret, i, client, log, eventRec); result != nil {
			return result, err
		}

		c.Endpoints.ClientCert = string(i.Spec.Security.EndpointEncryption.ClientCert)
		c.Truststore.Path = fmt.Sprintf("%s/%s", consts.ServerEncryptTruststoreRoot, consts.EncryptTruststoreKey)

		if userPass, ok := trustSecret.Data[consts.EncryptTruststorePasswordKey]; ok {
			c.Truststore.Password = string(userPass)
		} else {
			c.Truststore.Password = "password"
		}
	}
	return nil, nil
}
