package infinispan

import (
	"context"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func SiteService(siteServiceName string, infinispan *ispnv1.Infinispan, scheme *runtime.Scheme) (*corev1.Service, error) {
	lsPodSelector := PodLabels(infinispan.ObjectMeta.Name)
	lsPodSelector["coordinator"] = "true"
	lsService := LabelsResource(infinispan.ObjectMeta.Name, "infinispan-service-xsite")

	exposeSpec := corev1.ServiceSpec{}
	exposeConf := infinispan.Spec.Service.Sites.Local.Expose
	exposeSpec.Selector = lsPodSelector
	nodePort := int32(0)
	if exposeConf.NodePort != 0 {
		nodePort = exposeConf.NodePort
	}
	switch exposeConf.Type {
	case ispnv1.ExposeTypeNodePort:
		exposeSpec.Type = corev1.ServiceTypeNodePort
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port:       consts.CrossSitePort,
				NodePort:   nodePort,
				TargetPort: intstr.IntOrString{IntVal: consts.CrossSitePort},
			},
		}
	case ispnv1.ExposeTypeLoadBalancer:
		exposeSpec.Type = corev1.ServiceTypeLoadBalancer
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port:       consts.CrossSitePort,
				TargetPort: intstr.IntOrString{IntVal: consts.CrossSitePort},
			},
		}
	}

	objectMeta := metav1.ObjectMeta{
		Name:      siteServiceName,
		Namespace: infinispan.ObjectMeta.Namespace,
		Annotations: map[string]string{
			"service.beta.kubernetes.io/aws-load-balancer-backend-protocol": "tcp",
		},
		Labels: lsService,
	}
	if exposeConf.Annotations != nil && len(exposeConf.Annotations) > 0 {
		objectMeta.Annotations = exposeConf.Annotations
	}

	siteService := corev1.Service{
		ObjectMeta: objectMeta,
		Spec:       exposeSpec,
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(infinispan, &siteService, scheme)
	return &siteService, nil
}

func GetOrCreateSiteService(siteServiceName string, infinispan *ispnv1.Infinispan, client client.Client, scheme *runtime.Scheme, logger logr.Logger) (*corev1.Service, error) {
	siteService := &corev1.Service{}
	err := client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: siteServiceName}, siteService)

	if errors.IsNotFound(err) {
		siteService, err := SiteService(siteServiceName, infinispan, scheme)
		if err != nil && !errors.IsAlreadyExists(err) {
			return nil, err
		}

		err = client.Create(context.TODO(), siteService)
		if err != nil && !errors.IsAlreadyExists(err) {
			return nil, err
		}
		logger.Info("create exposed site service", "configuration", siteService.Spec)
	}

	if err != nil {
		return nil, err
	}

	return siteService, nil
}
