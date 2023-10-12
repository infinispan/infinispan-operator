package configure

import (
	"fmt"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	EncryptPkcs12KeystoreName = "keystore.p12"
	EncryptPemKeystoreName    = "keystore.pem"
)

func Keystore(i *ispnv1.Infinispan, ctx pipeline.Context) {
	keystore := &pipeline.Keystore{}

	keystoreSecret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetKeystoreSecretName(), keystoreSecret, pipeline.RetryOnErr); err != nil {
		return
	}

	if i.IsEncryptionCertFromService() {
		if strings.Contains(i.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			keystore.Path = consts.ServerOperatorSecurity + "/" + EncryptPemKeystoreName
			keystore.PemFile = append(keystoreSecret.Data["tls.key"], keystoreSecret.Data["tls.crt"]...)
		}
	} else {
		isUserProvidedPrivateKey := func() bool {
			for _, k := range []string{corev1.TLSPrivateKeyKey, corev1.TLSCertKey} {
				if _, ok := keystoreSecret.Data[k]; !ok {
					return false
				}
			}
			return true
		}

		if userKeystore, exists := keystoreSecret.Data[EncryptPkcs12KeystoreName]; exists {
			// If the user provides a keystore in secret then use it ...
			keystore.Path = fmt.Sprintf("%s/%s", consts.ServerEncryptKeystoreRoot, EncryptPkcs12KeystoreName)
			keystore.Alias = string(keystoreSecret.Data["alias"])
			keystore.Password = string(keystoreSecret.Data["password"])
			keystore.File = userKeystore
		} else if isUserProvidedPrivateKey() {
			keystore.Path = consts.ServerOperatorSecurity + "/" + EncryptPemKeystoreName
			keystore.PemFile = append(keystoreSecret.Data["tls.key"], keystoreSecret.Data["tls.crt"]...)
		} else {
			errMsg := fmt.Sprintf("Failed to setup TLS keystore from secret %s", keystoreSecret.Name)
			_ = ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionTLSSecretValid, metav1.ConditionFalse, errMsg)
			})
			ctx.Requeue(fmt.Errorf(errMsg))
			return
		}
	}
	_ = ctx.UpdateInfinispan(func() {
		i.SetCondition(ispnv1.ConditionTLSSecretValid, metav1.ConditionTrue, "")
	})
	ctx.ConfigFiles().Keystore = keystore
}

func Truststore(i *ispnv1.Infinispan, ctx pipeline.Context) {
	trustSecret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetTruststoreSecretName(), trustSecret, pipeline.RetryOnErr); err != nil {
		return
	}

	passwordBytes, passwordProvided := trustSecret.Data[consts.EncryptTruststorePasswordKey]
	password := string(passwordBytes)

	// If Truststore and password already exist, nothing to do
	if truststore, exists := trustSecret.Data[consts.EncryptTruststoreKey]; exists {
		if !passwordProvided {
			ctx.Requeue(fmt.Errorf("the '%s' key must be provided when configuring an existing Truststore", consts.EncryptTruststorePasswordKey))
			return
		}
		ctx.ConfigFiles().Truststore = &pipeline.Truststore{
			File:     truststore,
			Password: password,
		}
		return
	}

	if !passwordProvided {
		password = "password"
	}

	// Generate Truststore from provided ca and cert files
	caPem := trustSecret.Data["trust.ca"]
	certs := [][]byte{caPem}

	for certKey := range trustSecret.Data {
		if strings.HasPrefix(certKey, "trust.cert.") {
			certs = append(certs, trustSecret.Data[certKey])
		}
	}
	truststore, err := security.GenerateTruststore(certs, password)
	if err != nil {
		ctx.Requeue(err)
		return
	}
	ctx.ConfigFiles().Truststore = &pipeline.Truststore{
		File:     truststore,
		Password: password,
	}
}
