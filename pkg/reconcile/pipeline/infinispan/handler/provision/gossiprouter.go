package provision

import (
	"fmt"
	"strconv"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func GossipRouter(i *ispnv1.Infinispan, ctx pipeline.Context) {
	r := ctx.Resources()
	log := ctx.Log().WithName("GossipRouter")

	if !i.HasSites() {
		_ = ctx.Resources().Delete(i.GetGossipRouterDeploymentName(), &appsv1.Deployment{}, pipeline.RetryOnErr)
		return
	}

	// Remove old deployment to change the deployment name, required for upgrades
	oldRouterDeployment := fmt.Sprintf("%s-tunnel", i.Name)
	if err := r.Delete(oldRouterDeployment, &appsv1.Deployment{}, pipeline.RetryOnErr); err != nil {
		return
	}
	router := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetGossipRouterDeploymentName(),
			Namespace: i.Namespace,
		},
	}
	mutateFn := func() error {
		routerLabels := i.GossipRouterPodLabels()

		// if the user configures 0 replicas, shutdown the gossip router pod too.
		var replicas *int32
		if i.Spec.Replicas <= 0 {
			replicas = pointer.Int32(0)
		} else {
			replicas = pointer.Int32(i.Spec.Replicas)
		}

		args := []string{
			"-port", strconv.Itoa(consts.CrossSitePort),
			"-dump_msgs", "registration",
		}

		var addKeystoreVolume, addTruststoreVolume bool
		if i.IsSiteTLSEnabled() {
			ks := ctx.ConfigFiles().XSite.GossipRouter.Keystore
			args = append(args, []string{
				"-nio", "false", // NIO does not work with TLS
				"-tls_protocol", i.GetSiteTLSProtocol(),
				"-tls_keystore_password", ks.Password,
				"-tls_keystore_type", ks.Type,
				"-tls_keystore_alias", ks.Alias,
				"-tls_keystore_path", ks.Path,
			}...)
			addKeystoreVolume = true

			ts := ctx.ConfigFiles().XSite.GossipRouter.Truststore
			if ts != nil {
				args = append(args, []string{
					"-tls_truststore_password", ts.Password,
					"-tls_truststore_type", ts.Type,
					"-tls_truststore_path", ts.Path,
				}...)
				addTruststoreVolume = true
			}
		} else {
			log.Info("No TLS configured")
		}

		router.Labels = routerLabels
		router.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: routerLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      router.Name,
					Namespace: router.Namespace,
					Labels:    routerLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    GossipRouterContainer,
						Image:   i.ImageName(),
						Command: []string{"/opt/gossiprouter/bin/launch.sh"},
						Args:    args,
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: consts.CrossSitePort,
								Name:          "tunnel",
								Protocol:      corev1.ProtocolTCP,
							},
						},
						LivenessProbe:  TcpProbe(consts.CrossSitePort, 5, 5, 0, 1, 60),
						ReadinessProbe: TcpProbe(consts.CrossSitePort, 5, 5, 0, 1, 60),
						StartupProbe:   TcpProbe(consts.CrossSitePort, 5, 1, 1, 1, 60),
					}},
				},
			},
			Replicas: replicas,
		}
		if addKeystoreVolume {
			AddSecretVolume(i.GetSiteRouterSecretName(), SiteRouterKeystoreVolumeName, consts.SiteRouterKeyStoreRoot, &router.Spec.Template.Spec, GossipRouterContainer)
		}
		if addTruststoreVolume {
			AddSecretVolume(i.GetSiteTrustoreSecretName(), SiteTruststoreVolumeName, consts.SiteTrustStoreRoot, &router.Spec.Template.Spec, GossipRouterContainer)
		}
		return nil
	}

	result, err := r.CreateOrUpdate(router, true, mutateFn, pipeline.RetryOnErr)
	if err != nil {
		log.Error(err, "Failed to configure Cross-Site Deployment")
		return
	}
	if result != pipeline.OperationResultNone {
		log.Info(fmt.Sprintf("Cross-site deployment '%s' %s", router.Name, string(result)))
	}

	pods := &corev1.PodList{}
	if err := r.List(i.GossipRouterPodSelectorLabels(), pods, pipeline.RetryOnErr); err != nil {
		log.Error(err, "Failed to fetch Gossip Router pod")
		return
	}

	if len(pods.Items) == 0 || !kube.AreAllPodsReady(pods) {
		msg := "Gossip Router pod is not ready"
		log.Info(msg)
		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionGossipRouterReady, metav1.ConditionFalse, msg)
			}),
		)
		return
	}

	_ = ctx.UpdateInfinispan(func() {
		i.SetCondition(ispnv1.ConditionGossipRouterReady, metav1.ConditionTrue, "")
	})
}
