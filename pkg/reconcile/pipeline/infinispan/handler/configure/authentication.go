package configure

import (
	"fmt"
	"net/url"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

func UserAuthenticationSecret(i *ispnv1.Infinispan, ctx pipeline.Context) {
	secret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetSecretName(), secret); err != nil {
		if !i.IsGeneratedSecret() {
			ctx.Requeue(fmt.Errorf("unable to load user credential secret: %w", err))
		}
		return
	}

	userIdentities, ok := secret.Data[consts.ServerIdentitiesFilename]
	if !ok {
		ctx.Requeue(fmt.Errorf("authentiation secret '%s' missing required file '%s'", secret.Name, consts.ServerIdentitiesBatchFilename))
		return
	}
	ctx.ConfigFiles().UserIdentities = userIdentities
}

func AdminSecret(i *ispnv1.Infinispan, ctx pipeline.Context) {
	secret := &corev1.Secret{}
	if err := ctx.Resources().Load(i.GetAdminSecretName(), secret, pipeline.SkipEventRec); err != nil {
		if !errors.IsNotFound(err) {
			ctx.Requeue(err)
		}
		return
	}
	ctx.ConfigFiles().AdminIdentities = &pipeline.AdminIdentities{
		Username:       string(secret.Data[consts.AdminUsernameKey]),
		Password:       string(secret.Data[consts.AdminPasswordKey]),
		IdentitiesFile: secret.Data[consts.ServerIdentitiesFilename],
	}
}

func AdminIdentities(i *ispnv1.Infinispan, ctx pipeline.Context) {
	configFiles := ctx.ConfigFiles()

	user := i.GetOperatorUser()
	if configFiles.AdminIdentities == nil {
		// An existing secret was not found in the collect stage, so generate new credentials and define in the context
		identities, err := security.GetAdminCredentials(user)
		if err != nil {
			ctx.Requeue(err)
			return
		}

		password, err := security.FindPassword(user, identities)
		if err != nil {
			ctx.Requeue(err)
			return
		}

		configFiles.AdminIdentities = &pipeline.AdminIdentities{
			Username:       user,
			Password:       password,
			IdentitiesFile: identities,
		}
	} else {
		password := configFiles.AdminIdentities.Password
		if password == "" {
			var usrErr error
			if password, usrErr = security.FindPassword(user, configFiles.AdminIdentities.IdentitiesFile); usrErr != nil {
				ctx.Requeue(usrErr)
				return
			}
		}
		identities, err := security.CreateIdentitiesFor(user, password)
		if err != nil {
			ctx.Requeue(err)
			return
		}
		configFiles.AdminIdentities.IdentitiesFile = identities
	}

	autoconnectUrl := fmt.Sprintf("http://%s:%s@%s:%d",
		user,
		url.QueryEscape(configFiles.AdminIdentities.Password),
		i.GetAdminServiceName(),
		consts.InfinispanAdminPort,
	)
	configFiles.AdminIdentities.CliProperties = fmt.Sprintf("autoconnect-url=%s", autoconnectUrl)
}

func UserIdentities(_ *ispnv1.Infinispan, ctx pipeline.Context) {
	configFiles := ctx.ConfigFiles()
	if configFiles.UserIdentities == nil {
		identities, err := security.GetUserCredentials()
		if err != nil {
			ctx.Requeue(err)
			return
		}
		configFiles.UserIdentities = identities
	}
}

func IdentitiesBatch(i *ispnv1.Infinispan, ctx pipeline.Context) {
	configFiles := ctx.ConfigFiles()

	// Define admin identities on the server
	batch, err := security.IdentitiesCliFileFromSecret(configFiles.AdminIdentities.IdentitiesFile, "admin", "cli-admin-users.properties", "cli-admin-groups.properties")
	if err != nil {
		ctx.Requeue(fmt.Errorf("unable to read admin credentials: %w", err))
		return
	}

	// Add user identities only if authentication enabled
	if i.IsAuthenticationEnabled() {
		usersCliBatch, err := security.IdentitiesCliFileFromSecret(configFiles.UserIdentities, "default", "cli-users.properties", "cli-groups.properties")
		if err != nil {
			ctx.Requeue(fmt.Errorf("unable to read user credentials: %w", err))
			return
		}
		batch += usersCliBatch
	}

	if i.IsEncryptionEnabled() && !ctx.FIPS() {
		configFiles := ctx.ConfigFiles()

		// Add the keystore credential if the user has provided their own keystore
		if configFiles.Keystore.Password != "" {
			batch += fmt.Sprintf("credentials add keystore -c \"%s\" -p secret\n", configFiles.Keystore.Password)
		}

		if i.IsClientCertEnabled() {
			batch += fmt.Sprintf("credentials add truststore -c \"%s\" -p secret\n", configFiles.Truststore.Password)
		}
	}

	// Add user provided credentials to credential-store
	if i.IsCredentialStoreSecretDefined() {
		for alias, cred := range ctx.ConfigFiles().CredentialStoreEntries {
			batch += fmt.Sprintf("credentials add \"%s\" -c \"%s\" -p \"secret\"\n", alias, cred)
		}
	}

	configFiles.IdentitiesBatch = batch
}
