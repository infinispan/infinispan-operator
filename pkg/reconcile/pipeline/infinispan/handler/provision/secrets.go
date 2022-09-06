package provision

import (
	"bytes"
	_ "embed"
	"fmt"
	"path/filepath"
	"text/template"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed init-fips.sh
var initFipsTpl string

func UserAuthenticationSecret(i *ispnv1.Infinispan, ctx pipeline.Context) {
	secret := newSecret(i, i.GetSecretName())
	mutateFn := func() error {
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{consts.ServerIdentitiesFilename: ctx.ConfigFiles().UserIdentities}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(secret, true, mutateFn, pipeline.RetryOnErr)
}

func AdminSecret(i *ispnv1.Infinispan, ctx pipeline.Context) {
	configFiles := ctx.ConfigFiles()

	secret := newSecret(i, i.GetAdminSecretName())
	mutateFn := func() error {
		secret.Labels = i.Labels("infinispan-secret-admin-identities")
		secret.Data = map[string][]byte{
			consts.AdminUsernameKey:         []byte(configFiles.AdminIdentities.Username),
			consts.AdminPasswordKey:         []byte(configFiles.AdminIdentities.Password),
			consts.CliPropertiesFilename:    []byte(configFiles.AdminIdentities.CliProperties),
			consts.ServerIdentitiesFilename: configFiles.AdminIdentities.IdentitiesFile,
		}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(secret, true, mutateFn, pipeline.RetryOnErr)
}

func InfinispanSecuritySecret(i *ispnv1.Infinispan, ctx pipeline.Context) {
	configFiles := ctx.ConfigFiles()

	secret := newSecret(i, i.GetInfinispanSecuritySecretName())
	mutateFn := func() error {
		secret.Labels = i.Labels("infinispan-secret-server-security")
		secret.Data = map[string][]byte{
			consts.ServerIdentitiesBatchFilename: []byte(configFiles.IdentitiesBatch),
		}
		if i.IsEncryptionEnabled() {
			if len(configFiles.Keystore.PemFile) > 0 {
				secret.Data["keystore.pem"] = configFiles.Keystore.PemFile
			}

			if ctx.FIPS() {
				// Add FIPS startup script to secret so that it can be executed before server startup
				script, err := createFipsScript(i, ctx)
				if err != nil {
					return fmt.Errorf("unable to create init-fips.sh: %w", err)
				}

				secret.StringData = map[string]string{
					"init-fips.sh": script,
				}
			}
		}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(secret, true, mutateFn, pipeline.RetryOnErr)
}

func createFipsScript(i *ispnv1.Infinispan, ctx pipeline.Context) (string, error) {
	tpl, err := template.New("fips").Parse(initFipsTpl)
	if err != nil {
		return "", fmt.Errorf("unable to parse template: %w", err)
	}

	type Keystore struct {
		Path   string
		Secret string
	}
	cf := ctx.ConfigFiles()
	ks := cf.Keystore
	keystores := []Keystore{{
		Path:   filepath.Dir(ks.Path),
		Secret: ks.Password,
	}}

	if i.IsEncryptionCertFromService() {
		keystores[0].Secret = ""
	}

	if i.IsSiteTLSEnabled() {
		keystores = append(keystores, Keystore{
			Path:   consts.SiteTransportKeyStoreRoot,
			Secret: cf.Transport.Keystore.Password,
		})
	}

	buff := new(bytes.Buffer)
	if err := tpl.Execute(buff, keystores); err != nil {
		return "", fmt.Errorf("unable to execute template: %w", err)
	}
	return buff.String(), nil
}

func TruststoreSecret(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if !i.IsClientCertEnabled() {
		return
	}

	truststore := ctx.ConfigFiles().Truststore
	secret := newSecret(i, i.GetTruststoreSecretName())
	mutateFn := func() error {
		_, truststoreExists := secret.Data[consts.EncryptTruststoreKey]
		if !truststoreExists {
			secret.Data = map[string][]byte{
				consts.EncryptTruststoreKey:         truststore.File,
				consts.EncryptTruststorePasswordKey: []byte(truststore.Password),
			}
		}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(secret, false, mutateFn, pipeline.RetryOnErr)
}

func newSecret(i *ispnv1.Infinispan, name string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: i.Namespace,
		},
	}
}
