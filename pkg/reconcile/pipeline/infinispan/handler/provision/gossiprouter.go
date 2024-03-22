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

	if !i.IsGossipRouterEnabled() {
		if i.Spec.Replicas == 0 {
			// shutdown request, ignore
			_ = ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionGossipRouterReady, metav1.ConditionFalse, "Shutdown Requested")
			})
		} else {
			_ = ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionGossipRouterReady, metav1.ConditionTrue, "Gossip Router disabled by user")
			})
		}
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
		routerAnnotations := i.GossipRouterAnnotations()
		upstreamVersion := ctx.Operand().UpstreamVersion
		// Gossip Router Diagnostics is only available for JGroups 5+ (ISPN 14)
		enableJgrpDiag := consts.JGroupsDiagnosticsFlag && upstreamVersion.Major > 13
		// Port to be used by k8s probe, by default the cross-site port
		var probePort int32 = consts.CrossSitePort

		// if the user configures 0 replicas, shutdown the gossip router pod too.
		var replicas *int32
		if i.Spec.Replicas <= 0 {
			replicas = pointer.Int32(0)
		} else {
			replicas = pointer.Int32(1)
		}

		args := []string{
			"-port", strconv.Itoa(consts.CrossSitePort),
			"-dump_msgs", "registration",
			"-suspect", strconv.FormatBool(i.Spec.Service.Sites.Local.Discovery.SuspectEvents),
		}

		// arguments available since 14.0.24.Final
		if upstreamVersion.GTE(consts.GossipRouterHeartBeatMinVersion) {
			if *i.Spec.Service.Sites.Local.Discovery.Heartbeats.Enabled {
				args = append(args, []string{
					"-expiry", strconv.FormatInt(*i.Spec.Service.Sites.Local.Discovery.Heartbeats.Timeout, 10),
					"-reaper_interval", strconv.FormatInt(*i.Spec.Service.Sites.Local.Discovery.Heartbeats.Interval, 10),
				}...)
			}
		}

		var addKeystoreVolume, addTruststoreVolume bool
		if i.IsSiteTLSEnabled() {
			// enable diagnostic for k8s probes
			if upstreamVersion.Major > 13 {
				// with JGroups 5 (ISPN 14) we can use the diagnostic probe to avoid the SSL handshake exceptions
				enableJgrpDiag = true
				probePort = consts.GossipRouterDiagPort
			}

			ks := ctx.ConfigFiles().XSite.GossipRouter.Keystore
			args = append(args, []string{
				"-nio", "false", // NIO does not work with TLS
				"-tls_protocol", i.GetSiteTLSProtocol(),
				"-tls_keystore_password", ks.Password,
				"-tls_keystore_type", ks.Type,
				"-tls_keystore_alias", ks.Alias,
				"-tls_keystore_path", ks.Path,
				"-tls_client_auth", "need",
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

		containerPorts := []corev1.ContainerPort{
			{
				ContainerPort: consts.CrossSitePort,
				Name:          "tunnel",
				Protocol:      corev1.ProtocolTCP,
			},
		}

		if enableJgrpDiag {
			args = append(args, []string{
				"-diag_enabled", "true",
				"-diag_enable_udp", "false",
				"-diag_enable_tcp", "true",
				"-diag_port", strconv.Itoa(consts.GossipRouterDiagPort),
				"-diag_port_range", "0",
			}...)
			containerPorts = append(containerPorts,
				corev1.ContainerPort{
					ContainerPort: consts.GossipRouterDiagPort,
					Name:          "tunnel-diag",
					Protocol:      corev1.ProtocolTCP,
				},
			)
		}

		router.Labels = routerLabels

		container := &corev1.Container{
			Name:           GossipRouterContainer,
			Image:          i.ImageName(),
			Command:        []string{"/opt/gossiprouter/bin/launch.sh"},
			Args:           args,
			Env:            []corev1.EnvVar{{Name: "ROUTER_JAVA_OPTIONS", Value: i.Spec.Container.RouterExtraJvmOpts}},
			Ports:          containerPorts,
			LivenessProbe:  TcpProbe(probePort, 5, 5, 0, 1, 60),
			ReadinessProbe: TcpProbe(probePort, 5, 5, 0, 1, 60),
			StartupProbe:   TcpProbe(probePort, 15, 1, 1, 1, 60),
		}

		if podResources, err := gossipRouterPodResources(i.Spec.Service.Sites.Local.Discovery); err != nil {
			return err
		} else if podResources != nil {
			container.Resources = *podResources
		}

		router.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: routerLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        router.Name,
					Namespace:   router.Namespace,
					Labels:      routerLabels,
					Annotations: routerAnnotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{*container},
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
		if i.Spec.Replicas == 0 {
			// shutdown request, ignore
			// retry on error set!
			_ = ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionGossipRouterReady, metav1.ConditionFalse, "Shutdown Requested")
			})
			return
		}
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

func gossipRouterPodResources(spec *ispnv1.DiscoverySiteSpec) (*corev1.ResourceRequirements, error) {
	if spec.CPU == "" && spec.Memory == "" {
		return nil, nil
	}

	req := &corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{},
		Requests: corev1.ResourceList{},
	}
	if spec.Memory != "" {
		memRequests, memLimits, err := spec.MemoryResources()
		if err != nil {
			return req, err
		}
		req.Requests[corev1.ResourceMemory] = memRequests
		req.Limits[corev1.ResourceMemory] = memLimits
	}

	if spec.CPU != "" {
		cpuRequests, cpuLimits, err := spec.CpuResources()
		if err != nil {
			return req, err
		}
		req.Requests[corev1.ResourceCPU] = cpuRequests
		req.Limits[corev1.ResourceCPU] = cpuLimits
	}
	return req, nil
}
