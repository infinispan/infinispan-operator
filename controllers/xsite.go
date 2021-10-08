package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

func (r *infinispanRequest) GetCrossSiteViewCondition(podList *corev1.PodList, siteLocations []string, cluster ispn.ClusterInterface) (*ispnv1.InfinispanCondition, error) {
	for _, item := range podList.Items {
		cacheManagerInfo, err := cluster.GetCacheManagerInfo(consts.DefaultCacheManagerName, item.Name)
		if err == nil {
			if cacheManagerInfo.Coordinator {
				// Perform cross-site view validation
				crossSiteViewFormed := &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionTrue}
				sitesView, err := cacheManagerInfo.GetSitesView()
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
func (r *infinispanRequest) GetGossipRouterDeployment(m *ispnv1.Infinispan) *appsv1.Deployment {
	lsTunnel := GossipRouterPodLabels(m.Name)
	replicas := int32(1)

	// if the user configures 0 replicas, shutdown the gossip router pod too.
	if m.Spec.Replicas <= 0 {
		replicas = 0
	}

	reqLogger := r.log.WithValues("Request.Namespace", m.Namespace, "Request.Name", m.GetGossipRouterDeploymentName())
	isTLS := m.IsSiteTLSEnabled()
	args := make([]string, 0)
	args = append(args, "-port", strconv.Itoa(consts.CrossSitePort))
	args = append(args, "-dump_msgs", "registration")

	ignoreList := []string{"password", "type", "alias"}
	extractFileName := func(secret *corev1.Secret) (string, error) {
		keys := make([]string, 0)
		for k := range secret.Data {
			k = strings.TrimSpace(k)
			if !contains(ignoreList, k) && len(k) > 0 {
				keys = append(keys, k)
			}
		}
		if len(keys) != 1 {
			return "", fmt.Errorf("expected exactly one Data key in Secret %s but it contains %s", secret.ObjectMeta.Name, keys)
		}
		return keys[0], nil
	}

	addKeystoreVolume := false
	addTruststoreVolume := false

	if isTLS {
		// NIO does not work yet
		args = append(args, "-nio", "false")

		// Configure Keystore
		keystoreFileName, err := extractFileName(keystoreSecret)
		if err != nil {
			return nil, err
		}

		password := string(keystoreSecret.Data["password"])
		if len(password) == 0 {
			return nil, fmt.Errorf("Password is required for Keystore in Secret %s", keystoreSecret.ObjectMeta.Name)
		}

		reqLogger.Info("TLS Configured.", "Keystore", keystoreFileName, "Secret Name", keystoreSecret.ObjectMeta.Name)
		addKeystoreVolume = true

		args = append(args, "-tls_protocol", m.GetSiteTLSProtocol())
		args = append(args, "-tls_keystore_password", password)
		args = append(args, "-tls_keystore_type", consts.GetWithDefault(string(keystoreSecret.Data["type"]), "pkcs12"))
		args = append(args, "-tls_keystore_alias", consts.GetWithDefault(string(keystoreSecret.Data["alias"]), "gossip-router"))
		args = append(args, "-tls_keystore_path", fmt.Sprintf("%s/%s", consts.SiteRouterKeystoreRoot, keystoreFileName))

		// Configure Truststore (optional)
		truststoreSecret := &corev1.Secret{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Namespace: m.Namespace, Name: m.GetSiteTrustoreSecretName()}, truststoreSecret)
		if err != nil && !errors.IsNotFound(err) {
			return nil, err
		} else if err == nil || !errors.IsNotFound(err) {
			// truststore exists
			trustStoreFileName, err := extractFileName(truststoreSecret)
			if err != nil {
				return nil, err
			}

			password := string(truststoreSecret.Data["password"])
			if len(password) == 0 {
				return nil, fmt.Errorf("Password is required for Truststore in Secret %s", truststoreSecret.ObjectMeta.Name)
			}

			reqLogger.Info("Found Truststore.", "Truststore", trustStoreFileName, "Secret Name", truststoreSecret.ObjectMeta.Name)
			addTruststoreVolume = true

			args = append(args, "-tls_truststore_password", password)
			args = append(args, "-tls_truststore_type", consts.GetWithDefault(string(truststoreSecret.Data["type"]), "pkcs12"))
			args = append(args, "-tls_truststore_path", fmt.Sprintf("%s/%s", consts.SiteTruststoreRoot, trustStoreFileName))
		} else {
			reqLogger.Info("No Truststore secret found.", "Secret Name", m.GetSiteTrustoreSecretName())
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
						Name:    "gossiprouter",
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
		AddSecretVolume(m.GetSiteRouterSecretName(), SiteRouterKeystoreVolumeName, consts.SiteRouterKeystoreRoot, &deployment.Spec.Template.Spec)
	}
	if addTruststoreVolume {
		AddSecretVolume(m.GetSiteTrustoreSecretName(), SiteTruststoreVolumeName, consts.SiteTruststoreRoot, &deployment.Spec.Template.Spec)
	}

	return deployment, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
