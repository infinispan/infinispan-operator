package manage

import (
	"fmt"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	"k8s.io/cloud-provider/service/helpers"
	"k8s.io/utils/pointer"
)

const (
	EventLoadBalancerUnsupported = "LoadBalancerUnsupported"
)

func ConsoleUrl(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if !i.IsExposed() {
		_ = ctx.UpdateInfinispan(func() {
			i.Status.ConsoleUrl = nil
		})
		return
	}

	log := ctx.Log()
	r := ctx.Resources()
	k := ctx.Kubernetes()

	var exposeAddress string
	switch i.GetExposeType() {
	case ispnv1.ExposeTypeLoadBalancer, ispnv1.ExposeTypeNodePort:
		// Wait for the cluster external Service to be created by service-controller
		externalService := &corev1.Service{}

		if err := r.Load(i.GetServiceExternalName(), externalService, pipeline.RetryOnErr); err != nil {
			return
		}
		if len(externalService.Spec.Ports) > 0 && i.GetExposeType() == ispnv1.ExposeTypeNodePort {
			if exposeHost, err := k.GetNodeHost(log, ctx.Ctx()); err != nil {
				ctx.Requeue(err)
				return
			} else {
				exposeAddress = fmt.Sprintf("%s:%d", exposeHost, externalService.Spec.Ports[0].NodePort)
			}
		} else if i.GetExposeType() == ispnv1.ExposeTypeLoadBalancer {
			// Waiting for LoadBalancer cloud provider to update the configured hostname inside Status field
			if exposeAddress = k.GetExternalAddress(externalService); exposeAddress == "" {
				if !helpers.HasLBFinalizer(externalService) {
					errMsg := "LoadBalancer expose type is not supported on the target platform"
					ctx.EventRecorder().Event(externalService, corev1.EventTypeWarning, EventLoadBalancerUnsupported, errMsg)
					log.Info(errMsg)
					ctx.RequeueAfter(consts.DefaultWaitOnCluster, nil)
					return
				}
				log.Info("LoadBalancer address not ready yet. Waiting on value in reconcile loop")
				ctx.RequeueAfter(consts.DefaultWaitOnCluster, nil)
				return
			}
		}
	case ispnv1.ExposeTypeRoute:
		if ctx.IsTypeSupported(pipeline.RouteGVK) {
			externalRoute := &routev1.Route{}
			if err := r.Load(i.GetServiceExternalName(), externalRoute, pipeline.RetryOnErr); err != nil {
				return
			}
			exposeAddress = externalRoute.Spec.Host
		} else if ctx.IsTypeSupported(pipeline.IngressGVK) {
			externalIngress := &ingressv1.Ingress{}
			if err := r.Load(i.GetServiceExternalName(), externalIngress, pipeline.RetryOnErr); err != nil {
				return
			}
			if len(externalIngress.Spec.Rules) > 0 {
				exposeAddress = externalIngress.Spec.Rules[0].Host
			}
		}
	}

	_ = ctx.UpdateInfinispan(func() {
		if exposeAddress == "" {
			i.Status.ConsoleUrl = nil
		} else {
			i.Status.ConsoleUrl = pointer.StringPtr(fmt.Sprintf("%s://%s/console", i.GetEndpointScheme(), exposeAddress))
		}
	})
}
