package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/http/curl"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *infinispanRequest) GetCrossSiteViewCondition(podList *corev1.PodList, siteLocations []string, curl *curl.Client) (*ispnv1.InfinispanCondition, error) {
	for _, item := range podList.Items {
		cacheManager, err := InfinispanForPod(item.Name, curl).Container().Info()
		if err == nil {
			if cacheManager.Coordinator {
				// Perform cross-site view validation
				crossSiteViewFormed := &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionTrue}
				sitesView := make(map[string]bool)
				var err error
				if cacheManager.SitesView == nil {
					err = fmt.Errorf("retrieving the cross-site view is not supported with the server image you are using")
				}
				for _, site := range *cacheManager.SitesView {
					sitesView[site.(string)] = true
				}
				if err == nil {
					for _, location := range siteLocations {
						if !sitesView[location] {
							crossSiteViewFormed.Status = metav1.ConditionFalse
							crossSiteViewFormed.Message = fmt.Sprintf("Site '%s' not ready", location)
							break
						}
					}
					if crossSiteViewFormed.Status == metav1.ConditionTrue {
						crossSiteViewFormed.Message = fmt.Sprintf("Cross-Site view: %s", strings.Join(siteLocations, ","))
					}
				} else {
					crossSiteViewFormed.Status = metav1.ConditionUnknown
					crossSiteViewFormed.Message = fmt.Sprintf("Error: %s", err.Error())
				}
				return crossSiteViewFormed, nil
			}
		}
	}
	return &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionFalse, Message: "Coordinator not ready"}, nil
}

// GetGossipRouterDeployment returns the deployment for the Gossip Router pod
func (r *infinispanRequest) GetGossipRouterDeployment(m *ispnv1.Infinispan, keystoreSecret *corev1.Secret) (*appsv1.Deployment, error) {
	lsTunnel := GossipRouterPodLabels(m.Name)
	replicas := int32(1)

	// if the user configures 0 replicas, shutdown the gossip router pod too.
	if m.Spec.Replicas <= 0 {
		replicas = 0
	}

	reqLogger := r.log.WithValues("Request.Namespace", m.Namespace, "Request.Name", m.GetGossipRouterDeploymentName())
	args := make([]string, 0)
	args = append(args, "-port", strconv.Itoa(consts.CrossSitePort))
	args = append(args, "-dump_msgs", "registration")

	var addKeystoreVolume, addTruststoreVolume bool

	if m.IsSiteTLSEnabled() {
		// Configure KeyStore
		if err := configureKeyStore(&args, m, keystoreSecret, reqLogger); err != nil {
			return nil, err
		}
		addKeystoreVolume = true

		// Configure Truststore (optional)
		truststoreSecret, err := FindSiteTrustStoreSecret(m, r.Client, r.ctx)
		if err != nil {
			return nil, err
		}
		if truststoreSecret != nil {
			if err := configureTrustStore(&args, m, truststoreSecret, reqLogger); err != nil {
				return nil, err
			}
			addTruststoreVolume = true
		} else {
			reqLogger.Info("No TrustStore secret found.", "Secret Name", m.GetSiteTrustoreSecretName())
		}
	} else {
		reqLogger.Info("No TLS configured")
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.GetGossipRouterDeploymentName(),
			Namespace: m.Namespace,
			Labels:    lsTunnel,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: lsTunnel,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      m.ObjectMeta.Name,
					Namespace: m.ObjectMeta.Namespace,
					Labels:    lsTunnel,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    GossipRouterContainer,
						Image:   m.ImageName(),
						Command: []string{"/opt/gossiprouter/bin/launch.sh"},
						Args:    args,
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: consts.CrossSitePort,
								Name:          "tunnel",
								Protocol:      corev1.ProtocolTCP,
							},
						},
						LivenessProbe:  GossipRouterLivenessProbe(),
						ReadinessProbe: GossipRouterLivenessProbe(),
						StartupProbe:   GossipRouterStartupProbe(),
					}},
				},
			},
			Replicas: pointer.Int32Ptr(replicas),
		},
	}

	if addKeystoreVolume {
		AddSecretVolume(m.GetSiteRouterSecretName(), SiteRouterKeystoreVolumeName, consts.SiteRouterKeyStoreRoot, &deployment.Spec.Template.Spec, GossipRouterContainer)
	}
	if addTruststoreVolume {
		AddSecretVolume(m.GetSiteTrustoreSecretName(), SiteTruststoreVolumeName, consts.SiteTrustStoreRoot, &deployment.Spec.Template.Spec, GossipRouterContainer)
	}

	return deployment, nil
}

// FindSiteTrustStoreSecret searches for the truststore secret. Returns nil if not found.
func FindSiteTrustStoreSecret(ispn *ispnv1.Infinispan, client client.Client, ctx context.Context) (*corev1.Secret, error) {
	truststoreSecret := &corev1.Secret{}
	err := client.Get(ctx, types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetSiteTrustoreSecretName()}, truststoreSecret)
	if err != nil && errors.IsNotFound(err) {
		// not found!
		return nil, nil
	} else if err != nil {
		// got an error
		return nil, err
	} else {
		// found it!
		return truststoreSecret, nil
	}
}

func configureKeyStore(args *[]string, infinispan *ispnv1.Infinispan, keystoreSecret *corev1.Secret, log logr.Logger) error {
	// NIO does not work with TLS
	*args = append(*args, "-nio", "false")

	filename := infinispan.GetSiteRouterKeyStoreFileName()
	password := string(keystoreSecret.Data["password"])
	alias := infinispan.GetSiteRouterKeyStoreAlias()

	if err := ValidaXSiteTLSKeyStore(keystoreSecret.Name, filename, password, alias); err != nil {
		return err
	}

	log.Info("TLS Configured.", "Keystore", filename, "Secret Name", keystoreSecret.Name)

	*args = append(*args, "-tls_protocol", infinispan.GetSiteTLSProtocol())
	*args = append(*args, "-tls_keystore_password", password)
	*args = append(*args, "-tls_keystore_type", consts.GetWithDefault(string(keystoreSecret.Data["type"]), "pkcs12"))
	*args = append(*args, "-tls_keystore_alias", alias)
	*args = append(*args, "-tls_keystore_path", fmt.Sprintf("%s/%s", consts.SiteRouterKeyStoreRoot, filename))
	return nil
}

func configureTrustStore(args *[]string, infinispan *ispnv1.Infinispan, trustStoreSecret *corev1.Secret, log logr.Logger) error {
	filename := infinispan.GetSiteTrustStoreFileName()
	password := string(trustStoreSecret.Data["password"])

	if err := ValidaXSiteTLSTrustStore(trustStoreSecret.Name, filename, password); err != nil {
		return err
	}

	log.Info("Found Truststore.", "Truststore", filename, "Secret Name", trustStoreSecret.Name)

	*args = append(*args, "-tls_truststore_password", password)
	*args = append(*args, "-tls_truststore_type", consts.GetWithDefault(string(trustStoreSecret.Data["type"]), "pkcs12"))
	*args = append(*args, "-tls_truststore_path", fmt.Sprintf("%s/%s", consts.SiteTrustStoreRoot, filename))
	return nil
}

func ValidaXSiteTLSKeyStore(secretName, filename, password, alias string) error {
	if len(filename) == 0 {
		return fmt.Errorf("filename is required for Keystore stored in Secret %s", secretName)
	}
	if len(password) == 0 {
		return fmt.Errorf("password is required for Keystore stored in Secret %s", secretName)
	}
	if len(alias) == 0 {
		return fmt.Errorf("alias is required for Keystore stored in Secret %s", secretName)
	}
	return nil
}

func ValidaXSiteTLSTrustStore(secretName, filename, password string) error {
	if len(filename) == 0 {
		return fmt.Errorf("filename is required for KeyStore stored in Secret %s", secretName)
	}
	if len(password) == 0 {
		return fmt.Errorf("password is required for Keystore stored in Secret %s", secretName)
	}
	return nil
}
