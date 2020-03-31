package configuration

import (
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	comutil "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ServiceExternal creates an external service that's linked to the internal service
func ServiceExternal(m *infinispanv1.Infinispan, scheme *runtime.Scheme) *corev1.Service {
	lsPodSelector := comutil.LabelsResource(m.Name, "infinispan-pod")
	lsService := comutil.LabelsResource(m.Name, "infinispan-service-external")
	externalServiceType := m.Spec.Expose.Type
	// An external service can be simply achieved with a LoadBalancer
	// that has same selectors as original service.
	externalServiceName := m.GetServiceExternalName()
	externalService := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalServiceName,
			Namespace: m.Namespace,
			Labels:    lsService,
		},
		Spec: corev1.ServiceSpec{
			Type:     externalServiceType,
			Selector: lsPodSelector,
			Ports: []corev1.ServicePort{
				{
					Port:       int32(11222),
					TargetPort: intstr.FromInt(11222),
				},
			},
		},
	}

	if externalServiceType == corev1.ServiceTypeLoadBalancer {
		// Nothing to do here, keeping the if else struct
	} else if externalServiceType == corev1.ServiceTypeNodePort {
		var portNum int32
		if m.Spec.Expose.Ports != nil || len(m.Spec.Expose.Ports) > 0 {
			portNum = m.Spec.Expose.Ports[0].NodePort
			externalService.Spec.Ports[0].NodePort = portNum
		}
	} else {
		// Service type currently unsupported
		return nil
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, externalService, scheme)
	return externalService
}