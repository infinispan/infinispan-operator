package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	hash "github.com/infinispan/infinispan-operator/pkg/hash"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	SecretHashAnnotation = "infinispan.org/secret-hash"
)

// reconcileConfig reconciles a Service,Route and Ingress objects
type ServiceReconciler struct {
	client.Client
	log            logr.Logger
	scheme         *runtime.Scheme
	kube           *kube.Kubernetes
	eventRec       record.EventRecorder
	supportedTypes map[string]*reconcileType
}

type serviceRequest struct {
	*ServiceReconciler
	ctx        context.Context
	infinispan *ispnv1.Infinispan
	reqLogger  logr.Logger
}

func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "service"
	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName(strings.Title(name))
	r.scheme = mgr.GetScheme()
	r.kube = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor(name + "-controller")
	r.supportedTypes = map[string]*reconcileType{
		consts.ExternalTypeService: {ObjectType: &corev1.Service{}, GroupVersion: corev1.SchemeGroupVersion, GroupVersionSupported: true},
		consts.ExternalTypeRoute:   {ObjectType: &routev1.Route{}, GroupVersion: routev1.SchemeGroupVersion, GroupVersionSupported: false},
		consts.ExternalTypeIngress: {ObjectType: &ingressv1.Ingress{}, GroupVersion: schema.GroupVersion{Group: "networking.k8s.io", Version: "v1"}, GroupVersionSupported: false},
		consts.ServiceMonitorType:  {ObjectType: &monitoringv1.ServiceMonitor{}, GroupVersion: monitoringv1.SchemeGroupVersion, GroupVersionSupported: false, TypeWatchDisable: true},
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&ispnv1.Infinispan{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				switch e.Object.(type) {
				case *ispnv1.Infinispan:
					return true
				}
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				switch e.ObjectNew.(type) {
				case *ispnv1.Infinispan:
					return true
				}
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				switch e.Object.(type) {
				case *ispnv1.Infinispan:
					return false
				}
				return true
			},
		})

	for _, obj := range r.supportedTypes {
		if !obj.GroupVersionSupported {
			// Validate that GroupVersion is supported on runtime platform
			ok, err := r.kube.IsGroupVersionSupported(obj.GroupVersion.String(), obj.Kind())
			if err != nil {
				r.log.Error(err, fmt.Sprintf("failed to check if GVK '%s' is supported", obj.GroupVersionKind()))
				continue
			}
			obj.GroupVersionSupported = ok
			if !ok || obj.TypeWatchDisable {
				continue
			}
		}
		builder.Owns(obj.ObjectType)
	}
	return builder.Complete(r)
}

func (reconciler *ServiceReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := reconciler.log.WithValues("Reconciling", "Service", "Request.Namespace", request.Namespace, "Request.Name", request.Name)
	infinispan := &ispnv1.Infinispan{}
	if err := reconciler.Get(ctx, request.NamespacedName, infinispan); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Infinispan CR not found")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("unable to fetch Infinispan CR %w", err)
	}

	// Don't reconcile Infinispan CRs marked for deletion
	if infinispan.GetDeletionTimestamp() != nil {
		reqLogger.Info(fmt.Sprintf("Ignoring Infinispan CR '%s:%s' marked for deletion", infinispan.Namespace, infinispan.Name))
		return reconcile.Result{}, nil
	}

	// Validate that Infinispan CR passed all preliminary checks
	if !infinispan.IsConditionTrue(ispnv1.ConditionPrelimChecksPassed) {
		reqLogger.Info("Infinispan CR not ready")
		return reconcile.Result{}, nil
	}

	s := &serviceRequest{
		ServiceReconciler: reconciler,
		ctx:               ctx,
		infinispan:        infinispan,
		reqLogger:         reqLogger,
	}

	if s.infinispan.HasSites() {
		switch s.infinispan.GetCrossSiteExposeType() {
		case ispnv1.CrossSiteExposeTypeClusterIP, ispnv1.CrossSiteExposeTypeNodePort, ispnv1.CrossSiteExposeTypeLoadBalancer:
			// just a single external service
			if err := s.reconcileResource(computeSiteService(s.infinispan, s.infinispan.GetCrossSiteExposeType())); err != nil {
				return reconcile.Result{}, err
			}
		case ispnv1.CrossSiteExposeTypeRoute:
			if s.isTypeSupported(consts.ExternalTypeRoute) {
				// create internal service
				if err := s.reconcileResource(computeSiteService(s.infinispan, ispnv1.CrossSiteExposeTypeClusterIP)); err != nil {
					return reconcile.Result{}, err
				}
				if err := s.reconcileResource(computeSiteRoute(s.infinispan)); err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, fmt.Errorf("route cross-site expose type is not supported")
			}
		}
	} else {
		siteService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.infinispan.GetSiteServiceName(),
				Namespace: s.infinispan.Namespace,
			},
		}
		if err := s.Client.Delete(s.ctx, siteService); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
	}

	service := computeService(s.infinispan)
	setupServiceForEncryption(s.infinispan, service)
	if err := s.reconcileResource(service); err != nil {
		return reconcile.Result{}, err
	}

	if err := s.reconcileResource(computeAdminService(s.infinispan)); err != nil {
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
			externalExposeType = consts.ExternalTypeService
		case ispnv1.ExposeTypeRoute:
			if reconciler.isTypeSupported(consts.ExternalTypeRoute) {
				if err := s.reconcileResource(computeRoute(s.infinispan)); err != nil {
					return reconcile.Result{}, err
				}
				externalExposeType = consts.ExternalTypeRoute
			} else if reconciler.isTypeSupported(consts.ExternalTypeIngress) {
				if err := s.reconcileResource(computeIngress(s.infinispan)); err != nil {
					return reconcile.Result{}, err
				}
				externalExposeType = consts.ExternalTypeIngress
			}
		}
	}
	if err := s.cleanupExternalExpose(externalExposeType); err != nil {
		return reconcile.Result{}, err
	}

	return s.reconcileServiceMonitor(service)
}

// reconcileResource creates the resource (Service, Route or Ingress) for Infinispan if needed
func (s serviceRequest) reconcileResource(resource client.Object) error {
	unstructuredResource, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
	if err != nil {
		return err
	}
	key := client.ObjectKeyFromObject(resource)
	findResource := &unstructured.Unstructured{}
	findResource.SetGroupVersionKind(resource.GetObjectKind().GroupVersionKind())
	findResource.SetName(key.Name)
	findResource.SetNamespace(key.Namespace)

	result, err := controllerutil.CreateOrUpdate(s.ctx, s.Client, findResource, func() error {
		creationTimestamp := findResource.GetCreationTimestamp()
		metadata := unstructuredResource["metadata"].(map[string]interface{})
		spec := unstructuredResource["spec"].(map[string]interface{})
		if creationTimestamp.IsZero() {
			if err = controllerutil.SetControllerReference(s.infinispan, findResource, s.scheme); err != nil {
				return err
			}
			_ = unstructured.SetNestedField(findResource.UnstructuredContent(), spec, "spec")
			_ = unstructured.SetNestedField(findResource.UnstructuredContent(), metadata["annotations"], "metadata", "annotations")
			_ = unstructured.SetNestedField(findResource.UnstructuredContent(), metadata["labels"], "metadata", "labels")
		} else {
			findResourceMetadata := findResource.Object["metadata"].(map[string]interface{})
			findResourceSpec := findResource.Object["spec"].(map[string]interface{})
			if !reflect.DeepEqual(findResourceMetadata["annotations"], metadata["annotations"]) && resource.GetObjectKind().GroupVersionKind().Kind == consts.ExternalTypeService {
				_ = unstructured.SetNestedField(findResource.UnstructuredContent(), metadata["annotations"], "metadata", "annotations")
			}
			if !reflect.DeepEqual(findResourceMetadata["labels"], metadata["labels"]) {
				_ = unstructured.SetNestedField(findResource.UnstructuredContent(), metadata["labels"], "metadata", "labels")
			}
			if resource.GetObjectKind().GroupVersionKind().Kind != consts.ExternalTypeService && !reflect.DeepEqual(findResourceSpec["tls"], spec["tls"]) {
				_ = unstructured.SetNestedField(findResource.UnstructuredContent(), spec["tls"], "spec", "tls")
			}
			if resource.GetObjectKind().GroupVersionKind().Kind == consts.ExternalTypeService {
				if !reflect.DeepEqual(findResourceSpec["type"], spec["type"]) {
					_ = unstructured.SetNestedField(findResource.UnstructuredContent(), spec["type"], "spec", "type")
				}
				if spec["type"] == string(corev1.ServiceTypeNodePort) {
					specPort := spec["ports"].([]interface{})[0].(map[string]interface{})
					findResourceSpecPort := findResourceSpec["ports"].([]interface{})[0].(map[string]interface{})
					if specPort["nodePort"] != nil && specPort["nodePort"] != findResourceSpecPort["nodePort"] {
						_ = unstructured.SetNestedField(findResourceSpecPort, specPort["nodePort"], "nodePort")
						_ = unstructured.SetNestedSlice(findResource.UnstructuredContent(), []interface{}{findResourceSpecPort}, "spec", "ports")
					}
				} else if spec["type"] == string(corev1.ServiceTypeLoadBalancer) {
					specPort := spec["ports"].([]interface{})[0].(map[string]interface{})
					findResourceSpecPort := findResourceSpec["ports"].([]interface{})[0].(map[string]interface{})
					if specPort["port"] != findResourceSpecPort["port"] {
						_ = unstructured.SetNestedField(findResourceSpecPort, specPort["port"], "port")
						_ = unstructured.SetNestedSlice(findResource.UnstructuredContent(), []interface{}{findResourceSpecPort}, "spec", "ports")
					}
				}

			}
		}

		return nil
	})
	if err != nil {
		s.log.Error(err, fmt.Sprintf("failed to create or update %s", findResource.GetKind()), findResource.GetKind(), findResource)
		return err
	}
	if result != controllerutil.OperationResultNone {
		s.log.Info(fmt.Sprintf("%s %s %s", strings.Title(string(result)), findResource.GetKind(), findResource.GetName()))
	}
	return runtime.DefaultUnstructuredConverter.FromUnstructured(findResource.UnstructuredContent(), resource)
}

func (s serviceRequest) cleanupExternalExpose(excludeKind string) error {
	for _, obj := range s.supportedTypes {
		if obj.GroupVersionSupported && obj.Kind() != excludeKind {
			switch obj.Kind() {
			case consts.ExternalTypeService:
				//TODO DeleteAllOf for Service objects not yet implemented. See https://github.com/kubernetes/kubernetes/issues/68468#issuecomment-419981870
				serviceList := &corev1.ServiceList{}
				if err := s.kube.ResourcesList(s.infinispan.Namespace, ExternalServiceLabels(s.infinispan.Name), serviceList, s.ctx); err != nil {
					return nil
				}
				for _, service := range serviceList.Items {
					if err := s.Client.Delete(s.ctx, &service); err != nil {
						return nil
					}
				}
			case consts.ExternalTypeIngress, consts.ExternalTypeRoute:
				deleteOptions := []client.DeleteAllOfOption{client.MatchingLabels(ExternalServiceLabels(s.infinispan.Name)), client.InNamespace(s.infinispan.Namespace)}
				if err := s.Client.DeleteAllOf(s.ctx, obj.ObjectType, deleteOptions...); err != nil && !errors.IsNotFound(err) {
					return err
				}
			}
		}
	}
	return nil
}

func (s serviceRequest) reconcileServiceMonitor(service *corev1.Service) (reconcile.Result, error) {
	if !s.isTypeSupported(consts.ServiceMonitorType) {
		return reconcile.Result{}, nil
	}

	if s.infinispan.IsServiceMonitorEnabled() {
		secret := &corev1.Secret{}
		if result, err := kube.LookupResource(s.infinispan.GetAdminSecretName(), s.infinispan.Namespace, secret, s.infinispan, s.Client, s.log, s.eventRec, s.ctx); result != nil {
			return *result, err
		}

		serviceMonitor := computeServiceMonitor(s.infinispan)
		if _, err := controllerutil.CreateOrUpdate(s.ctx, s.Client, serviceMonitor, func() error {
			creationTimestamp := serviceMonitor.GetCreationTimestamp()
			if creationTimestamp.IsZero() {
				return controllerutil.SetControllerReference(service, serviceMonitor, s.scheme)
			}
			if serviceMonitor.Annotations == nil {
				serviceMonitor.Annotations = map[string]string{}
			}
			// Annotation to force ServiceMonitor update when operator admin password has been changed
			serviceMonitor.Annotations[SecretHashAnnotation] = hash.HashByte(secret.Data[consts.AdminPasswordKey])
			return nil
		}); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		if err := s.Client.Delete(s.ctx,
			&monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s.infinispan.GetServiceMonitorName(),
					Namespace: s.infinispan.Namespace,
				},
			}); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func setupServiceForEncryption(ispn *ispnv1.Infinispan, service *corev1.Service) {
	if ispn.IsEncryptionCertFromService() {
		if strings.Contains(ispn.Spec.Security.EndpointEncryption.CertServiceName, "openshift.io") {
			// Using platform service. Only OpenShift is integrated atm
			secretName := ispn.GetKeystoreSecretName()
			if service.Annotations == nil {
				service.Annotations = map[string]string{}
			}
			service.Annotations[ispn.Spec.Security.EndpointEncryption.CertServiceName+"/serving-cert-secret-name"] = secretName
		}
	}
}

func computeService(ispn *ispnv1.Infinispan) *corev1.Service {
	service := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceName(),
			Namespace: ispn.Namespace,
			Labels:    LabelsResource(ispn.Name, "infinispan-service"),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: ServiceLabels(ispn.Name),
			Ports: []corev1.ServicePort{
				{
					Name: consts.InfinispanUserPortName,
					Port: consts.InfinispanUserPort,
				},
			},
		},
	}
	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(service.Labels)
	ispn.AddLabelsForServices(service.Labels)
	return &service
}

func computeAdminService(ispn *ispnv1.Infinispan) *corev1.Service {
	service := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetAdminServiceName(),
			Namespace: ispn.Namespace,
			Labels:    LabelsResource(ispn.Name, "infinispan-service-admin"),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: ServiceLabels(ispn.Name),
			Ports: []corev1.ServicePort{
				{
					Name: consts.InfinispanAdminPortName,
					Port: consts.InfinispanAdminPort,
				},
			},
		},
	}
	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(service.Labels)
	ispn.AddLabelsForServices(service.Labels)
	return &service
}

func computePingService(ispn *ispnv1.Infinispan) *corev1.Service {
	pingService := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetPingServiceName(),
			Namespace: ispn.Namespace,
			Labels:    LabelsResource(ispn.Name, "infinispan-service-ping"),
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector:  ServiceLabels(ispn.Name),
			Ports: []corev1.ServicePort{
				{
					Name: consts.InfinispanPingPortName,
					Port: consts.InfinispanPingPort,
				},
			},
		},
	}
	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(pingService.Labels)
	ispn.AddLabelsForServices(pingService.Labels)
	return &pingService
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
		Selector: ServiceLabels(ispn.Name),
		Ports: []corev1.ServicePort{
			{
				Port:       int32(consts.InfinispanUserPort),
				TargetPort: intstr.FromInt(consts.InfinispanUserPort),
			},
		},
	}
	if exposeConf.NodePort > 0 && exposeConf.Type == ispnv1.ExposeTypeNodePort {
		exposeSpec.Ports[0].NodePort = exposeConf.NodePort
	}
	if exposeConf.Port > 0 && exposeConf.Type == ispnv1.ExposeTypeLoadBalancer {
		exposeSpec.Ports[0].Port = exposeConf.Port
	}

	externalService := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metadata,
		Spec:       exposeSpec,
	}
	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(externalService.Labels)
	ispn.AddLabelsForServices(externalService.Labels)
	return &externalService
}

// computeSiteService compute the XSite service
func computeSiteService(ispn *ispnv1.Infinispan, exposeType ispnv1.CrossSiteExposeType) *corev1.Service {
	lsPodSelector := GossipRouterPodLabels(ispn.Name)

	exposeSpec := corev1.ServiceSpec{}
	exposeConf := ispn.Spec.Service.Sites.Local.Expose
	exposeSpec.Selector = lsPodSelector

	switch exposeType {
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
		if exposeConf.Port > 0 {
			exposeSpec.Ports[0].Port = exposeConf.Port
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
		Labels: LabelsResource(ispn.Name, "infinispan-service-xsite"),
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
	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(siteService.Labels)
	ispn.AddLabelsForServices(siteService.Labels)
	return &siteService
}

func computeSiteRoute(ispn *ispnv1.Infinispan) *routev1.Route {
	host := strings.TrimSpace(ispn.Spec.Service.Sites.Local.Expose.RouteHostName)
	route := routev1.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "route.openshift.io/v1",
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetSiteRouteName(),
			Namespace: ispn.Namespace,
			Labels:    LabelsResource(ispn.Name, "infinispan-route-xsite"),
		},
		Spec: routev1.RouteSpec{
			Host: host,
			Port: &routev1.RoutePort{
				TargetPort: intstr.IntOrString{IntVal: consts.CrossSitePort},
			},
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: ispn.GetSiteServiceName(),
			},
			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationPassthrough,
			},
		},
	}

	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(route.Labels)
	ispn.AddLabelsForServices(route.Labels)
	return &route
}

// computeRoute compute the Route object
func computeRoute(ispn *ispnv1.Infinispan) *routev1.Route {
	route := routev1.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "route.openshift.io/v1",
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    ExternalServiceLabels(ispn.Name),
		},
		Spec: routev1.RouteSpec{
			Host: ispn.Spec.Expose.Host,
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(consts.InfinispanUserPort),
			},
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: ispn.Name,
			},
		},
	}
	if ispn.IsEncryptionEnabled() {
		route.Spec.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough}
	}

	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(route.Labels)
	ispn.AddLabelsForServices(route.Labels)
	return &route
}

// computeIngress compute the Ingress object
func computeIngress(ispn *ispnv1.Infinispan) *ingressv1.Ingress {
	pathTypePrefix := ingressv1.PathTypePrefix
	ingress := ingressv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceExternalName(),
			Namespace: ispn.Namespace,
			Labels:    ExternalServiceLabels(ispn.Name),
		},
		Spec: ingressv1.IngressSpec{
			TLS: []ingressv1.IngressTLS{},
			Rules: []ingressv1.IngressRule{
				{
					Host: ispn.Spec.Expose.Host,
					IngressRuleValue: ingressv1.IngressRuleValue{
						HTTP: &ingressv1.HTTPIngressRuleValue{
							Paths: []ingressv1.HTTPIngressPath{
								{
									PathType: &pathTypePrefix,
									Path:     "/",
									Backend: ingressv1.IngressBackend{
										Service: &ingressv1.IngressServiceBackend{
											Name: ispn.Name,
											Port: ingressv1.ServiceBackendPort{Number: consts.InfinispanUserPort},
										},
									}}},
						}}}}}}
	if ispn.IsEncryptionEnabled() {
		ingress.Spec.TLS = []ingressv1.IngressTLS{
			{
				Hosts: []string{ispn.Spec.Expose.Host},
			},
		}
	}
	// This way CR labels will override operator labels with same name
	ispn.AddOperatorLabelsForServices(ingress.Labels)
	ispn.AddLabelsForServices(ingress.Labels)
	return &ingress
}

func computeServiceMonitor(ispn *ispnv1.Infinispan) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "ServiceMonitor",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ispn.GetServiceMonitorName(),
			Namespace: ispn.Namespace,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:          consts.InfinispanAdminPortName,
					Path:          "/metrics",
					Scheme:        "http",
					Interval:      "30s",
					ScrapeTimeout: "10s",
					HonorLabels:   true,
					BasicAuth: &monitoringv1.BasicAuth{
						Username: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: ispn.GetAdminSecretName(),
							},
							Key: consts.AdminUsernameKey,
						},
						Password: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: ispn.GetAdminSecretName(),
							},
							Key: consts.AdminPasswordKey,
						},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: LabelsResource(ispn.Name, "infinispan-service-admin"),
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{ispn.Namespace},
			},
		},
	}
}

func (reconciler *ServiceReconciler) isTypeSupported(kind string) bool {
	return reconciler.supportedTypes[kind].GroupVersionSupported
}
