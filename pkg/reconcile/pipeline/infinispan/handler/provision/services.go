package provision

import (
	"fmt"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func PingService(i *ispnv1.Infinispan, ctx pipeline.Context) {
	svc := newService(i, i.GetPingServiceName())

	mutateFn := func() error {
		svc.Annotations = i.ServiceAnnotations()
		svc.Labels = i.ServiceLabels("infinispan-service-ping")
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Selector = i.ServiceSelectorLabels()
		// We must utilise the existing ServicePort values if updating the service, to prevent the created ports being overwritten
		if svc.CreationTimestamp.IsZero() {
			svc.Spec.Ports = []corev1.ServicePort{{}}
		}
		servicePort := &svc.Spec.Ports[0]
		servicePort.Name = consts.InfinispanPingPortName
		servicePort.Port = consts.InfinispanPingPort
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(svc, true, mutateFn, pipeline.RetryOnErr)
}

func ClusterService(i *ispnv1.Infinispan, ctx pipeline.Context) {
	svc := newService(i, i.GetServiceName())
	mutateFn := func() error {
		svc.Annotations = i.ServiceAnnotations()
		svc.Labels = i.ServiceLabels("infinispan-service")
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = i.ServiceSelectorLabels()
		// We must utilise the existing ServicePort values if updating the service, to prevent the created ports being overwritten
		if svc.CreationTimestamp.IsZero() {
			svc.Spec.Ports = []corev1.ServicePort{{}}
		}
		servicePort := &svc.Spec.Ports[0]
		servicePort.Name = consts.InfinispanUserPortName
		servicePort.Port = consts.InfinispanUserPort

		if i.IsEncryptionCertFromService() {
			if strings.Contains(i.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
				// Using platform service. Only OpenShift is integrated atm
				secretName := i.GetKeystoreSecretName()
				svc.Annotations[i.Spec.Security.EndpointEncryption.CertServiceName+"/serving-cert-secret-name"] = secretName
			}
		}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(svc, true, mutateFn, pipeline.RetryOnErr)
}

func AdminService(i *ispnv1.Infinispan, ctx pipeline.Context) {
	svc := newService(i, i.GetAdminServiceName())

	mutateFn := func() error {
		svc.Annotations = i.ServiceAnnotations()
		svc.Labels = i.ServiceLabels("infinispan-service-admin")

		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = i.ServiceSelectorLabels()
		// We must utilise the existing ServicePort values if updating the service, to prevent the created ports being overwritten
		if svc.CreationTimestamp.IsZero() {
			svc.Spec.Ports = []corev1.ServicePort{{}}
			// If an upgrade is in progress, we wait for the GracefulShutdown to destroy the old admin service before
			// defining a new one with ClusterIpNone
			svc.Spec.ClusterIP = corev1.ClusterIPNone
		}
		servicePort := &svc.Spec.Ports[0]
		servicePort.Name = consts.InfinispanAdminPortName
		servicePort.Port = consts.InfinispanAdminPort
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(svc, true, mutateFn, pipeline.RetryOnErr)
}

func ExternalService(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if !i.IsExposed() {
		return
	}

	// If expose type has changed, ensure that we remove all existing expose definitions
	exposeType := i.GetExposeType()
	for _, gvk := range pipeline.ServiceTypes {
		if ctx.IsTypeSupported(gvk) && gvk.Kind != string(exposeType) {
			labels := i.ExternalServiceSelectorLabels()
			switch gvk {
			case pipeline.ServiceGVK:
				serviceList := &corev1.ServiceList{}
				if err := ctx.Resources().List(labels, serviceList); err != nil {
					ctx.Log().Error(err, "unable to list Services for deletion")
				}
				for _, svc := range serviceList.Items {
					if err := ctx.Resources().Delete(svc.Name, &svc, pipeline.RetryOnErr); err != nil {
						return
					}
				}
			case pipeline.RouteGVK:
				routeList := &routev1.RouteList{}
				if err := ctx.Resources().List(labels, routeList); err != nil {
					ctx.Log().Error(err, "unable to list Routes for deletion")
				}
				for _, route := range routeList.Items {
					if err := ctx.Resources().Delete(route.Name, &route, pipeline.RetryOnErr); err != nil {
						return
					}
				}
			case pipeline.IngressGVK:
				ingressList := &ingressv1.IngressList{}
				if err := ctx.Resources().List(labels, ingressList); err != nil {
					ctx.Log().Error(err, "unable to list Ingress' for deletion")
				}
				for _, route := range ingressList.Items {
					if err := ctx.Resources().Delete(route.Name, &route, pipeline.RetryOnErr); err != nil {
						return
					}
				}
			}
		}
	}

	switch exposeType {
	case ispnv1.ExposeTypeLoadBalancer, ispnv1.ExposeTypeNodePort:
		defineExternalService(i, ctx)
	case ispnv1.ExposeTypeRoute:
		if ctx.IsTypeSupported(pipeline.RouteGVK) {
			defineExternalRoute(i, ctx)
		} else if ctx.IsTypeSupported(pipeline.IngressGVK) {
			defineExternalIngress(i, ctx)
		} else {
			ctx.Stop(fmt.Errorf("unable to expose cluster with type Route, as no implementations are supported"))
		}
	}
}

func defineExternalService(i *ispnv1.Infinispan, ctx pipeline.Context) {
	externalServiceType := corev1.ServiceType(i.Spec.Expose.Type)

	svc := newService(i, i.GetServiceExternalName())
	mutateFn := func() error {
		svc.Annotations = i.ServiceAnnotations()
		for k, v := range i.Spec.Expose.Annotations {
			svc.Annotations[k] = v
		}
		svc.Labels = i.ExternalServiceLabels()
		svc.Spec.Type = externalServiceType
		svc.Spec.Selector = i.ServiceSelectorLabels()

		// We must utilise the existing ServicePort values if updating the service, to prevent the created ports being overwritten
		if svc.CreationTimestamp.IsZero() {
			svc.Spec.Ports = []corev1.ServicePort{{}}
		}
		servicePort := &svc.Spec.Ports[0]
		servicePort.Port = int32(consts.InfinispanUserPort)
		servicePort.TargetPort = intstr.FromInt(consts.InfinispanUserPort)

		exposeConf := i.Spec.Expose
		if exposeConf.NodePort > 0 && exposeConf.Type == ispnv1.ExposeTypeNodePort {
			servicePort.NodePort = exposeConf.NodePort
		}
		if exposeConf.Port > 0 && exposeConf.Type == ispnv1.ExposeTypeLoadBalancer {
			servicePort.Port = exposeConf.Port
		}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(svc, true, mutateFn, pipeline.RetryOnErr)
}

func defineExternalRoute(i *ispnv1.Infinispan, ctx pipeline.Context) {
	route := newRoute(i, i.GetServiceExternalName())
	mutateFn := func() error {
		route.Annotations = i.ServiceAnnotations()
		route.Labels = i.ExternalServiceLabels()
		route.Spec.Host = i.Spec.Expose.Host
		route.Spec.Port = &routev1.RoutePort{
			TargetPort: intstr.FromInt(consts.InfinispanUserPort),
		}
		route.Spec.To = routev1.RouteTargetReference{
			Kind: "Service",
			Name: i.Name,
		}

		if i.IsEncryptionEnabled() {
			route.Spec.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough}
		}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(route, true, mutateFn, pipeline.RetryOnErr)
}

func defineExternalIngress(i *ispnv1.Infinispan, ctx pipeline.Context) {
	pathTypePrefix := ingressv1.PathTypePrefix

	ingress := &ingressv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetServiceExternalName(),
			Namespace: i.Namespace,
		},
	}

	mutateFn := func() error {
		ingress.Annotations = i.ServiceAnnotations()
		ingress.Labels = i.ExternalServiceLabels()
		ingress.Spec.Rules = []ingressv1.IngressRule{
			{
				Host: i.Spec.Expose.Host,
				IngressRuleValue: ingressv1.IngressRuleValue{
					HTTP: &ingressv1.HTTPIngressRuleValue{
						Paths: []ingressv1.HTTPIngressPath{
							{
								PathType: &pathTypePrefix,
								Path:     "/",
								Backend: ingressv1.IngressBackend{
									Service: &ingressv1.IngressServiceBackend{
										Name: i.Name,
										Port: ingressv1.ServiceBackendPort{Number: consts.InfinispanUserPort},
									},
								}}},
					},
				},
			},
		}

		if i.IsEncryptionEnabled() {
			ingress.Spec.TLS = []ingressv1.IngressTLS{
				{
					Hosts: []string{i.Spec.Expose.Host},
				},
			}
		}
		return nil
	}
	_, _ = ctx.Resources().CreateOrUpdate(ingress, true, mutateFn, pipeline.RetryOnErr)
}

func XSiteService(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if !i.HasSites() || !i.IsGossipRouterEnabled() {
		_ = ctx.Resources().Delete(i.GetSiteServiceName(), &corev1.Service{}, pipeline.RetryOnErr, pipeline.IgnoreNotFound)
		ok, err := ctx.Kubernetes().IsGroupVersionKindSupported(pipeline.RouteGVK)
		if err != nil {
			ctx.Log().Error(err, fmt.Sprintf("failed to check if GVK '%s' is supported", pipeline.RouteGVK))
			return
		}
		if ok {
			_ = ctx.Resources().Delete(i.GetSiteRouteName(), &routev1.Route{}, pipeline.RetryOnErr, pipeline.IgnoreNotFound)
		}
		return
	}

	exposeConf := i.Spec.Service.Sites.Local.Expose
	exposeType := i.GetCrossSiteExposeType()
	var svcType corev1.ServiceType
	if exposeType == ispnv1.CrossSiteExposeTypeRoute {
		svcType = corev1.ServiceTypeClusterIP
	} else {
		svcType = corev1.ServiceType(exposeType)
	}

	if exposeType == ispnv1.CrossSiteExposeTypeRoute {
		if !ctx.IsTypeSupported(pipeline.RouteGVK) {
			ctx.Stop(fmt.Errorf("route cross-site expose type is not supported"))
			return
		}
	}

	var annotations map[string]string
	if exposeConf.Annotations != nil && len(exposeConf.Annotations) > 0 {
		annotations = exposeConf.Annotations
	} else {
		annotations = i.ServiceAnnotations()
		annotations["service.beta.kubernetes.io/aws-load-balancer-backend-protocol"] = "tcp"
	}

	svc := newService(i, i.GetSiteServiceName())
	mutateFn := func() error {
		svc.Annotations = annotations
		svc.Labels = i.ServiceLabels("infinispan-service-xsite")
		svc.Spec.Selector = i.GossipRouterPodSelectorLabels()
		svc.Spec.Type = svcType

		// We must utilise the existing ServicePort values if updating the service, to prevent the created ports being overwritten
		if svc.CreationTimestamp.IsZero() {
			svc.Spec.Ports = []corev1.ServicePort{{}}
		}
		servicePort := &svc.Spec.Ports[0]
		if exposeType == ispnv1.CrossSiteExposeTypeLoadBalancer && exposeConf.Port > 0 {
			servicePort.Port = exposeConf.Port
		} else {
			servicePort.Port = consts.CrossSitePort
		}
		servicePort.TargetPort = intstr.IntOrString{IntVal: consts.CrossSitePort}
		return nil
	}

	if _, err := ctx.Resources().CreateOrUpdate(svc, true, mutateFn, pipeline.RetryOnErr); err != nil {
		return
	}

	if exposeType == ispnv1.CrossSiteExposeTypeRoute {
		// Provision Route resource to expose the service just created
		route := newRoute(i, i.GetSiteRouteName())
		mutateFn = func() error {
			route.Annotations = i.ServiceAnnotations()
			route.Labels = i.ServiceLabels("infinispan-route-xsite")
			route.Spec.Host = strings.TrimSpace(exposeConf.RouteHostName)
			route.Spec.Port = &routev1.RoutePort{
				TargetPort: intstr.FromInt(consts.CrossSitePort),
			}
			route.Spec.To = routev1.RouteTargetReference{
				Kind: "Service",
				Name: i.GetSiteServiceName(),
			}
			route.Spec.TLS = &routev1.TLSConfig{
				Termination: routev1.TLSTerminationPassthrough,
			}
			return nil
		}
		_, _ = ctx.Resources().CreateOrUpdate(route, true, mutateFn, pipeline.RetryOnErr)
	}
}

func newService(i *ispnv1.Infinispan, name string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: i.Namespace,
		},
	}
}

func newRoute(i *ispnv1.Infinispan, name string) *routev1.Route {
	return &routev1.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "route.openshift.io/v1",
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: i.Namespace,
		},
	}
}
