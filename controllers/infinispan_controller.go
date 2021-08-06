/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InfinispanReconciler reconciles a Infinispan object
type InfinispanReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infinispan.org,resources=infinispans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infinispan.org,resources=infinispans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infinispan.org,resources=infinispans/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Infinispan object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *InfinispanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("infinispan", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *InfinispanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infinispanv1.Infinispan{}).
		Complete(r)
}

// func ServiceLabels(name string) map[string]string {
// 	return map[string]string{
// 		"clusterName": name,
// 		"app":         "infinispan-pod",
// 	}
// }

// func PodLabels(name string) map[string]string {
// 	return LabelsResource(name, "infinispan-pod")
// }

// func PodResources(spec infinispanv1.InfinispanContainerSpec) (*corev1.ResourceRequirements, error) {
// 	memory, err := resource.ParseQuantity(spec.Memory)
// 	if err != nil {
// 		return nil, err
// 	}
// 	cpuRequests, cpuLimits, err := spec.GetCpuResources()
// 	if err != nil {
// 		return nil, err
// 	}
// 	return &corev1.ResourceRequirements{
// 		Requests: corev1.ResourceList{
// 			corev1.ResourceCPU:    *cpuRequests,
// 			corev1.ResourceMemory: memory,
// 		},
// 		Limits: corev1.ResourceList{
// 			corev1.ResourceCPU:    *cpuLimits,
// 			corev1.ResourceMemory: memory,
// 		},
// 	}, nil
// }

// // LabelsResource returns the labels that must me applied to the resource
// func LabelsResource(name, resourceType string) map[string]string {
// 	m := map[string]string{"infinispan_cr": name, "clusterName": name}
// 	if resourceType != "" {
// 		m["app"] = resourceType
// 	}
// 	return m
// }

// func PodEnv(i *infinispanv1.Infinispan, systemEnv *[]corev1.EnvVar) []corev1.EnvVar {
// 	envVars := []corev1.EnvVar{
// 		{Name: "CONFIG_PATH", Value: consts.ServerConfigPath},
// 		// Prevent the image from generating a user if authentication disabled
// 		{Name: "MANAGED_ENV", Value: "TRUE"},
// 		{Name: "JAVA_OPTIONS", Value: i.GetJavaOptions()},
// 		{Name: "EXTRA_JAVA_OPTIONS", Value: i.Spec.Container.ExtraJvmOpts},
// 		{Name: "DEFAULT_IMAGE", Value: consts.DefaultImageName},
// 		{Name: "ADMIN_IDENTITIES_PATH", Value: consts.ServerAdminIdentitiesPath},
// 	}

// 	// Adding additional variables listed in ADDITIONAL_VARS env var
// 	envVar, defined := os.LookupEnv("ADDITIONAL_VARS")
// 	if defined {
// 		var addVars []string
// 		err := json.Unmarshal([]byte(envVar), &addVars)
// 		if err == nil {
// 			for _, name := range addVars {
// 				value, defined := os.LookupEnv(name)
// 				if defined {
// 					envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
// 				}
// 			}
// 		}
// 	}

// 	if i.IsAuthenticationEnabled() {
// 		envVars = append(envVars, corev1.EnvVar{Name: "IDENTITIES_PATH", Value: consts.ServerUserIdentitiesPath})
// 	}

// 	if systemEnv != nil {
// 		envVars = append(envVars, *systemEnv...)
// 	}

// 	return envVars
// }

// func PodPorts() []corev1.ContainerPort {
// 	ports := []corev1.ContainerPort{
// 		{ContainerPort: consts.InfinispanAdminPort, Name: consts.InfinispanAdminPortName, Protocol: corev1.ProtocolTCP},
// 		{ContainerPort: consts.InfinispanPingPort, Name: consts.InfinispanPingPortName, Protocol: corev1.ProtocolTCP},
// 		{ContainerPort: consts.InfinispanUserPort, Name: consts.InfinispanUserPortName, Protocol: corev1.ProtocolTCP},
// 	}
// 	return ports
// }

// func PodPortsWithXsite(i *infinispanv1.Infinispan) []corev1.ContainerPort {
// 	ports := PodPorts()
// 	if i.HasSites() {
// 		ports = append(ports, corev1.ContainerPort{ContainerPort: consts.CrossSitePort, Name: consts.CrossSitePortName, Protocol: corev1.ProtocolTCP})
// 	}
// 	return ports
// }

// func PodLivenessProbe() *corev1.Probe {
// 	return probe(5, 10, 10, 1, 80)
// }

// func PodReadinessProbe() *corev1.Probe {
// 	return probe(5, 10, 10, 1, 80)
// }

// func probe(failureThreshold, initialDelay, period, successThreshold, timeout int32) *corev1.Probe {
// 	return &corev1.Probe{
// 		Handler: corev1.Handler{
// 			HTTPGet: &corev1.HTTPGetAction{
// 				Scheme: corev1.URISchemeHTTP,
// 				Path:   consts.ServerHTTPHealthStatusPath,
// 				Port:   intstr.FromInt(consts.InfinispanAdminPort)},
// 		},
// 		FailureThreshold:    failureThreshold,
// 		InitialDelaySeconds: initialDelay,
// 		PeriodSeconds:       period,
// 		SuccessThreshold:    successThreshold,
// 		TimeoutSeconds:      timeout,
// 	}
// }

// AddVolumeForUserAuthentication returns true if the volume has been added
