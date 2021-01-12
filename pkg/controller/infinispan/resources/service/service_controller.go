package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/resources"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ControllerName = "service-controller"

	ExternalTypeService = "Service"
	ExternalTypeRoute   = "Route"
	ExternalTypeIngress = "Ingress"
)

var ctx = context.Background()

// reconcileConfig reconciles a Service,Route and Ingress objects
type reconcileService struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
}

type serviceResource struct {
	infinispan *ispnv1.Infinispan
	client     client.Client
	scheme     *runtime.Scheme
	kube       *kube.Kubernetes
	log        logr.Logger
}

func (r reconcileService) ResourceInstance(infinispan *ispnv1.Infinispan, ctrl *resources.Controller, kube *kube.Kubernetes, log logr.Logger) resources.Resource {
	return &serviceResource{
		infinispan: infinispan,
		client:     r.Client,
		scheme:     ctrl.Scheme,
		kube:       kube,
		log:        log,
	}
}

var reconcileTypes = []*resources.ReconcileType{
	{&corev1.Service{}, corev1.SchemeGroupVersion, true},
	{&routev1.Route{}, routev1.GroupVersion, false},
	{&networkingv1beta1.Ingress{}, networkingv1beta1.SchemeGroupVersion, false},
}

func (r reconcileService) Types() []*resources.ReconcileType {
	return reconcileTypes
}

func (r reconcileService) EventsPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
	}
}

func isTypeSupported(kind string) bool {
	for _, obj := range reconcileTypes {
		if obj.Kind() == kind {
			return obj.GroupVersionSupported
		}
	}
	return false
}

func Add(mgr manager.Manager) error {
	return resources.CreateController(ControllerName, &reconcileService{mgr.GetClient()}, mgr)
}

func (s serviceResource) Process() (reconcile.Result, error) {
	if s.infinispan.HasSites() {
		if err := s.reconcileResource(computeSiteService(s.infinispan)); err != nil {
			return reconcile.Result{}, err
		}
	}

	service := computeService(s.infinispan)
	setupServiceForEncryption(s.infinispan, service)
	if err := s.reconcileResource(service); err != nil {
		return reconcile.Result{}, err
	}

	if err := s.reconcileResource(computePingService(s.infinispan)); err != nil {
		return reconcile.Result{}, err
	}

	var externalExposeType = ""
	if s.infinispan.IsExposed() {
		switch s.infinispan.GetExposeType() {
		case ispnv1.ExposeTypeLoadBalancer, ispnv1.ExposeTypeNodePort:
			if err := s.reconcileResource(computeServiceExternal(s.infinispan)); err != nil {
				return reconcile.Result{}, err
			}
			externalExposeType = ExternalTypeService
		case ispnv1.ExposeTypeRoute:
			if isTypeSupported(ExternalTypeRoute) {
				if err := s.reconcileResource(computeRoute(s.infinispan)); err != nil {
					return reconcile.Result{}, err
				}
				externalExposeType = ExternalTypeRoute
			} else if isTypeSupported(ExternalTypeIngress) {
				if err := s.reconcileResource(computeIngress(s.infinispan)); err != nil {
					return reconcile.Result{}, err
				}
				externalExposeType = ExternalTypeIngress
			}
		}
	}
	s.cleanupExternalExpose(externalExposeType)

	return reconcile.Result{}, nil
}

// reconcileResource creates the resource (Service, Route or Ingress) for Infinispan if needed
func (s serviceResource) reconcileResource(resource metav1.Object) error {
	// Validates that resource not created yet
	foundResource := &unstructured.Unstructured{}
	ro, _ := resource.(runtime.Object)
	kind := ro.GetObjectKind().GroupVersionKind().Kind
	foundResource.SetGroupVersionKind(ro.GetObjectKind().GroupVersionKind())
	err := s.client.Get(context.TODO(), types.NamespacedName{Namespace: resource.GetNamespace(), Name: resource.GetName()}, resource.(runtime.Object))
	if errors.IsNotFound(err) {
		// Set Infinispan instance as the owner and controller
		controllerutil.SetControllerReference(s.infinispan, resource, s.scheme)
		err := s.client.Create(ctx, ro)
		if err != nil {
			s.log.Error(err, fmt.Sprintf("failed to create %s", kind), kind, resource)
			return err
		}
		s.log.Info(fmt.Sprintf("Created %s", kind), kind, resource.GetName())
		return nil
	}
	return err
}

func (s serviceResource) cleanupExternalExpose(excludeKind string) error {
	for _, obj := range reconcileTypes {
		if obj.GroupVersionSupported && obj.Kind() != excludeKind {
			externalObject := &unstructured.Unstructured{}
			externalObject.SetGroupVersionKind(obj.GroupVersionKind())
			externalObject.SetName(s.infinispan.GetServiceExternalName())
			externalObject.SetNamespace(s.infinispan.Namespace)
			err := s.client.Delete(ctx, externalObject)
			if err != nil && !errors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func setupServiceForEncryption(ispn *ispnv1.Infinispan, service *corev1.Service) {
	if ispn.IsEncryptionCertFromService() {
		if strings.Contains(ispn.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			// Using platform service. Only OpenShift is integrated atm
			secretName := ispn.GetEncryptionSecretName()
			if service.Annotations == nil {
				service.Annotations = map[string]string{}
			}
			service.Annotations[ispn.Spec.Security.EndpointEncryption.CertServiceName+"/serving-cert-secret-name"] = secretName
		}
	}
}

func computeService(ispn *ispnv1.Infinispan) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.Name,
			Namespace: ispn.Namespace,
			Labels:    infinispan.LabelsResource(ispn.Name, "infinispan-service"),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: infinispan.ServiceLabels(ispn.Name),
			Ports: []corev1.ServicePort{
				{
					Name: consts.InfinispanPortName,
					Port: consts.InfinispanPort,
				},
			},
		},
	}
}

func computePingService(ispn *ispnv1.Infinispan) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetPingServiceName(),
			Namespace: ispn.Namespace,
			Labels:    infinispan.LabelsResource(ispn.Name, "infinispan-service-ping"),
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector:  infinispan.ServiceLabels(ispn.Name),
			Ports: []corev1.ServicePort{
				{
					Name: consts.InfinispanPingPortName,
					Port: consts.InfinispanPingPort,
				},
			},
		},
	}
}

// computeServiceExternal compute the external service
func computeServiceExternal(ispn *ispnv1.Infinispan) *corev1.Service {
	externalServiceType := corev1.ServiceType(ispn.Spec.Expose.Type)
	exposeConf := ispn.Spec.Expose

	metadata := metav1.ObjectMeta{
		Name:      ispn.GetServiceExternalName(),
		Namespace: ispn.Namespace,
		Labels:    ExternalServiceLabels(ispn.Name),
	}
	if exposeConf.Annotations != nil && len(exposeConf.Annotations) > 0 {
		metadata.Annotations = exposeConf.Annotations
	}

	exposeSpec := corev1.ServiceSpec{
		Type:     externalServiceType,
		Selector: infinispan.ServiceLabels(ispn.Name),
		Ports: []corev1.ServicePort{
			{
				Port:       int32(consts.InfinispanPort),
				TargetPort: intstr.FromInt(consts.InfinispanPort),
			},
		},
	}
	if exposeConf.NodePort > 0 && exposeConf.Type == ispnv1.ExposeTypeNodePort {
		exposeSpec.Ports[0].NodePort = exposeConf.NodePort
	}

	externalService := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metadata,
		Spec:       exposeSpec,
	}
	return externalService
}

// computeSiteService compute the XSite service
func computeSiteService(ispn *ispnv1.Infinispan) *corev1.Service {
	lsPodSelector := infinispan.PodLabels(ispn.Name)
	lsPodSelector["coordinator"] = "true"

	exposeSpec := corev1.ServiceSpec{}
	exposeConf := ispn.Spec.Service.Sites.Local.Expose
	exposeSpec.Selector = lsPodSelector

	switch exposeConf.Type {
	case ispnv1.CrossSiteExposeTypeNodePort:
		exposeSpec.Type = corev1.ServiceTypeNodePort
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port:       consts.CrossSitePort,
				NodePort:   exposeConf.NodePort,
				TargetPort: intstr.IntOrString{IntVal: consts.CrossSitePort},
			},
		}
	case ispnv1.CrossSiteExposeTypeLoadBalancer:
		exposeSpec.Type = corev1.ServiceTypeLoadBalancer
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port:       consts.CrossSitePort,
				TargetPort: intstr.IntOrString{IntVal: consts.CrossSitePort},
			},
		}
	case ispnv1.CrossSiteExposeTypeClusterIP:
		exposeSpec.Type = corev1.ServiceTypeClusterIP
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port:       consts.CrossSitePort,
				TargetPort: intstr.IntOrString{IntVal: consts.CrossSitePort},
			},
		}
	}

	objectMeta := metav1.ObjectMeta{
		Name:      ispn.GetSiteServiceName(),
		Namespace: ispn.Namespace,
		Annotations: map[string]string{
			"service.beta.kubernetes.io/aws-load-balancer-backend-protocol": "tcp",
		},
		Labels: infinispan.LabelsResource(ispn.Name, "infinispan-service-xsite"),
	}
	if exposeConf.Annotations != nil && len(exposeConf.Annotations) > 0 {
		objectMeta.Annotations = exposeConf.Annotations
	}

	siteService := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: objectMeta,
		Spec:       exposeSpec,
	}

	return &siteService
}

// computeRoute compute the Route object
func computeRoute(ispn *ispnv1.Infinispan) *routev1.Route {
	route := &routev1.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    ExternalServiceLabels(ispn.Name),
		},
		Spec: routev1.RouteSpec{
			Host: ispn.Spec.Expose.Host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: ispn.Name},
		},
	}
	if ispn.GetEncryptionSecretName() != "" && !ispn.IsEncryptionDisabled() {
		route.Spec.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough}
	}
	return route
}

// computeIngress compute the Ingress object
func computeIngress(ispn *ispnv1.Infinispan) *networkingv1beta1.Ingress {
	ingress := networkingv1beta1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1beta1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    ExternalServiceLabels(ispn.Name),
		},
		Spec: networkingv1beta1.IngressSpec{
			TLS: []networkingv1beta1.IngressTLS{},
			Rules: []networkingv1beta1.IngressRule{
				{
					Host: ispn.Spec.Expose.Host,
					IngressRuleValue: networkingv1beta1.IngressRuleValue{
						HTTP: &networkingv1beta1.HTTPIngressRuleValue{
							Paths: []networkingv1beta1.HTTPIngressPath{
								{
									Path: "/",
									Backend: networkingv1beta1.IngressBackend{
										ServiceName: ispn.Name,
										ServicePort: intstr.IntOrString{IntVal: consts.InfinispanPort}}}}},
					}}},
		}}
	if ispn.GetEncryptionSecretName() != "" && !ispn.IsEncryptionDisabled() {
		ingress.Spec.TLS = []networkingv1beta1.IngressTLS{
			{
				Hosts: []string{ispn.Spec.Expose.Host},
			},
		}
	}
	return &ingress
}

func ExternalServiceLabels(name string) map[string]string {
	return infinispan.LabelsResource(name, "infinispan-service-external")
}
