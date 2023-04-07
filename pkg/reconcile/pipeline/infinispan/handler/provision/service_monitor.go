package provision

import (
	"fmt"

	iv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SecretHashAnnotation = "infinispan.org/secret-hash"
)

func ServiceMonitor(i *iv1.Infinispan, ctx pipeline.Context) {
	if !ctx.IsTypeSupported(pipeline.ServiceMonitorGVK) {
		return
	}

	if !i.IsServiceMonitorEnabled() {
		if err := ctx.Resources().Delete(i.GetServiceMonitorName(), &monitoringv1.ServiceMonitor{}, pipeline.IgnoreNotFound, pipeline.RetryOnErr); err != nil {
			return
		}
		return
	}

	serviceMonitor := &monitoringv1.ServiceMonitor{
		TypeMeta: metav1.TypeMeta{
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
			Kind:       monitoringv1.ServiceMonitorsKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      i.GetServiceMonitorName(),
			Namespace: i.Namespace,
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
								Name: i.GetAdminSecretName(),
							},
							Key: consts.AdminUsernameKey,
						},
						Password: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: i.GetAdminSecretName(),
							},
							Key: consts.AdminPasswordKey,
						},
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: i.Labels("infinispan-service-admin"),
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{i.Namespace},
			},
			TargetLabels: i.ServiceMonitorTargetLabels(),
		},
	}

	mutateFn := func() error {
		if serviceMonitor.Annotations == nil {
			serviceMonitor.Annotations = map[string]string{}
		}
		// Annotation to force ServiceMonitor update when operator admin password has been changed
		serviceMonitor.Annotations[SecretHashAnnotation] = hash.HashString(ctx.ConfigFiles().AdminIdentities.Password)

		// Operator 8.3.x incorrectly sets the ServiceMonitor owner to be the Infinispan service, not the Infinispan CR
		// Therefore we must explicitly remove the old owner reference if it exists, so that we can successfully upgrade
		// from the 8.3.x -> 8.4.0 Operator
		creationTimestamp := serviceMonitor.GetCreationTimestamp()
		if !creationTimestamp.IsZero() {
			retained := 0
			for _, ref := range serviceMonitor.OwnerReferences {
				if !*ref.Controller || ref.Kind != pipeline.ServiceGVK.Kind {
					serviceMonitor.OwnerReferences[retained] = ref
					retained++
				}
			}
			serviceMonitor.OwnerReferences = serviceMonitor.OwnerReferences[:retained]
		}

		serviceMonitor.Spec.TargetLabels = i.ServiceMonitorTargetLabels()

		return nil
	}

	if _, err := ctx.Resources().CreateOrUpdate(serviceMonitor, true, mutateFn); err != nil {
		ctx.Requeue(fmt.Errorf("unable to createOrUpdate ServiceMonitor: %w", err))
	}
}
