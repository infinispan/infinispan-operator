package configuration

import (
	"context"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	comutil "github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func SiteService(siteServiceName string, infinispan *ispnv1.Infinispan, scheme *runtime.Scheme) (*corev1.Service, error) {
	lsPodSelector := comutil.LabelsResource(infinispan.Name, "infinispan-pod")
	lsService := comutil.LabelsResource(infinispan.Name, "infinispan-service-xsite")
	exposeSpec := infinispan.Spec.Service.Sites.Local.Expose
	exposeSpec.Selector = lsPodSelector

	switch exposeSpec.Type {
	case corev1.ServiceTypeNodePort:
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port:     7900,
				NodePort: 32660,
			},
		}
	case corev1.ServiceTypeLoadBalancer:
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port: 7900,
			},
		}
	}

	siteService := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      siteServiceName,
			Namespace: infinispan.ObjectMeta.Namespace,
			Annotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-backend-protocol": "tcp",
			},
			Labels: lsService,
		},
		Spec: exposeSpec,
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(infinispan, &siteService, scheme)
	return &siteService, nil
}

func GetOrCreateSiteService(siteServiceName string, infinispan *ispnv1.Infinispan, client client.Client, scheme *runtime.Scheme, logger logr.Logger) (*corev1.Service, error) {
	siteService := &corev1.Service{}
	ns := types.NamespacedName{
		Name:      siteServiceName,
		Namespace: infinispan.Namespace,
	}
	err := client.Get(context.TODO(), ns, siteService)

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
