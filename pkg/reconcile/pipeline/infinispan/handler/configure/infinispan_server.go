package configure

import (
	"fmt"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/logging"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/server"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
)

func UserConfigMap(i *ispnv1.Infinispan, ctx pipeline.Context) {
	overlayConfigMap := &corev1.ConfigMap{}
	if err := ctx.Resources().Load(i.Spec.ConfigMapName, overlayConfigMap, pipeline.RetryOnErr); err != nil {
		return
	}

	var overlayConfigMapKey string
	var overlayLog4jConfig bool
	// Loop through the data looking for something like xml, json or yaml
	for configMapKey := range overlayConfigMap.Data {
		if configMapKey == "infinispan-config.xml" || configMapKey == "infinispan-config.json" || configMapKey == "infinispan-config.yaml" {
			overlayConfigMapKey = configMapKey
			break
		}
	}

	// Check if the user added a custom log4j.xml config
	userLog4j, overlayLog4jConfig := overlayConfigMap.Data["log4j.xml"]

	if overlayConfigMapKey == "" && !overlayLog4jConfig {
		err := fmt.Errorf("one of infinispan-config.[xml|yaml|json] or log4j.xml must be present in the provided ConfigMap: %s", overlayConfigMap.Name)
		ctx.Requeue(err)
	}

	configFiles := ctx.ConfigFiles()
	configFiles.UserConfig = pipeline.UserConfig{
		Log4j:                userLog4j,
		ServerConfig:         overlayConfigMap.Data[overlayConfigMapKey],
		ServerConfigFileName: overlayConfigMapKey,
	}
}

func InfinispanServer(i *ispnv1.Infinispan, ctx pipeline.Context) {
	configFiles := ctx.ConfigFiles()

	var roleMapper string
	if i.IsClientCertEnabled() && i.Spec.Security.EndpointEncryption.ClientCert == ispnv1.ClientCertAuthenticate {
		roleMapper = "commonName"
	} else {
		roleMapper = "cluster"
	}

	configSpec := &config.Spec{
		ClusterName:     i.Name,
		Namespace:       i.Namespace,
		StatefulSetName: i.GetStatefulSetName(),
		Infinispan: config.Infinispan{
			Authorization: &config.Authorization{
				Enabled:    i.IsAuthorizationEnabled(),
				RoleMapper: roleMapper,
			},
		},
		JGroups: config.JGroups{
			Diagnostics: consts.JGroupsDiagnosticsFlag == "TRUE",
			FastMerge:   consts.JGroupsFastMerge,
		},
		Endpoints: config.Endpoints{
			Authenticate: i.IsAuthenticationEnabled(),
			ClientCert:   string(ispnv1.ClientCertNone),
		},
	}
	// Save the spec for later so that we can reuse it for HR rolling upgrades
	ctx.ConfigFiles().ConfigSpec = *configSpec

	if i.HasSites() {
		// Convert the pipeline ConfigFiles to the config struct
		xSite := &config.XSite{
			MaxRelayNodes: configFiles.XSite.MaxRelayNodes,
		}
		configSpec.XSite = xSite
		xSite.Sites = make([]config.BackupSite, len(configFiles.XSite.Sites))
		for i, site := range configFiles.XSite.Sites {
			xSite.Sites[i] = config.BackupSite(site)
		}

		if i.IsSiteTLSEnabled() {
			// Configure Transport TLS
			ks := configFiles.Transport.Keystore
			tlsConfig := config.TransportTLS{
				Enabled: true,
				KeyStore: config.Keystore{
					Alias:    ks.Alias,
					NSS:      ctx.FIPS(),
					Password: ks.Password,
					Path:     ks.Path,
				},
			}
			ts := configFiles.Transport.Truststore
			if ts != nil {
				tlsConfig.TrustStore = config.Truststore{
					NSS:      ctx.FIPS(),
					Path:     ts.Path,
					Password: ts.Password,
				}
			}
			configSpec.Transport.TLS = tlsConfig
		}
	}

	// Apply settings for authentication and roles
	specRoles := i.GetAuthorizationRoles()
	if len(specRoles) > 0 {
		confRoles := make([]config.AuthorizationRole, len(specRoles))
		for i, role := range specRoles {
			confRoles[i] = config.AuthorizationRole{
				Name:        role.Name,
				Permissions: strings.Join(role.Permissions, " "),
			}
		}
		configSpec.Infinispan.Authorization.Roles = confRoles
	}

	if i.Spec.CloudEvents != nil {
		configSpec.CloudEvents = &config.CloudEvents{
			Acks:              i.Spec.CloudEvents.Acks,
			BootstrapServers:  i.Spec.CloudEvents.BootstrapServers,
			CacheEntriesTopic: i.Spec.CloudEvents.CacheEntriesTopic,
		}
	}
	if i.IsEncryptionEnabled() {
		ks := configFiles.Keystore
		configSpec.Keystore = config.Keystore{
			Alias: ks.Alias,
			NSS:   ctx.FIPS(),
			// Actual value is not used by template, but required to show that a credential ref is required
			Password: ks.Password,
			Path:     ks.Path,
		}

		if i.IsClientCertEnabled() {
			configSpec.Endpoints.ClientCert = string(i.Spec.Security.EndpointEncryption.ClientCert)
			configSpec.Truststore.NSS = ctx.FIPS()
			configSpec.Truststore.Path = fmt.Sprintf("%s/%s", consts.ServerEncryptTruststoreRoot, consts.EncryptTruststoreKey)
		}
	}

	if baseCfg, adminCfg, err := config.Generate(ctx.Operand(), configSpec); err == nil {
		configFiles.ServerAdminConfig = adminCfg
		configFiles.ServerBaseConfig = baseCfg
	} else {
		ctx.Requeue(fmt.Errorf("unable to generate infinispan.xml: %w", err))
		return
	}

	if zeroConfig, err := config.GenerateZeroCapacity(ctx.Operand(), configSpec); err == nil {
		configFiles.ZeroConfig = zeroConfig
	} else {
		ctx.Requeue(fmt.Errorf("unable to generate infinispan.xml: %w", err))
	}
}

func Logging(i *ispnv1.Infinispan, ctx pipeline.Context) {
	loggingSpec := &logging.Spec{
		Categories: i.GetLogCategoriesForConfig(),
	}
	log4jXml, err := logging.Generate(ctx.Operand(), loggingSpec)
	if err != nil {
		ctx.Requeue(fmt.Errorf("unable to generate log4j.xml: %w", err))
		return
	}
	ctx.ConfigFiles().Log4j = log4jXml
}
