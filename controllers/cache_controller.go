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
	"fmt"

	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/caches"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"honnef.co/go/tools/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	infinispanv2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
)

var kubernetes *kube.Kubernetes

// CacheReconciler reconciles a Cache object
type CacheReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infinispan.org,resources=caches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infinispan.org,resources=caches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infinispan.org,resources=caches/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Cache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *CacheReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	reqLogger := r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info(fmt.Sprintf("+++++ Reconciling Cache. Operator Version: %s", version.Version))
	defer reqLogger.Info("----- End Reconciling Cache.")

	// Fetch the Cache instance
	instance := &infinispanv2alpha1.Cache{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Cache resource not found. Ignoring it since cache deletion is not supported")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Spec.AdminAuth != nil {
		reqLogger.Info("Ignoring and removing 'spec.AdminAuth' field. The operator's admin credentials are now used to perform cache operations")
		_, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, instance, func() error {
			instance.Spec.AdminAuth = nil
			return nil
		})
		return reconcile.Result{}, err
	}

	// Reconcile cache
	reqLogger.Info("Identify the target cluster")
	// Fetch the Infinispan cluster info
	ispnInstance := &infinispanv1.Infinispan{}
	nsName := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.ClusterName}
	err = r.Client.Get(context.TODO(), nsName, ispnInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, fmt.Sprintf("Infinispan cluster %s not found", ispnInstance.Name))
			return reconcile.Result{RequeueAfter: constants.DefaultWaitOnCluster}, err
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Cluster must be well formed
	if !ispnInstance.IsWellFormed() {
		reqLogger.Info(fmt.Sprintf("Infinispan cluster %s not well formed", ispnInstance.Name))
		return reconcile.Result{RequeueAfter: constants.DefaultWaitOnCluster}, err
	}
	// List the pods for this infinispan's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(ispnctrl.LabelsResource(ispnInstance.Name, ""))
	listOps := &client.ListOptions{Namespace: ispnInstance.GetClusterName(), LabelSelector: labelSelector}
	err = r.Client.List(context.TODO(), podList, listOps)
	if err != nil || (len(podList.Items) == 0) {
		reqLogger.Error(err, "failed to list pods")
		return reconcile.Result{}, err
	} else if len(podList.Items) == 0 {
		reqLogger.Error(err, "No Infinispan pods found")
		return reconcile.Result{}, nil
	}

	cluster, err := ispnctrl.NewCluster(ispnInstance, kubernetes)
	if err != nil {
		return reconcile.Result{}, err
	}
	existsCache, err := cluster.ExistsCache(instance.GetCacheName(), podList.Items[0].Name)
	if err == nil {
		if existsCache {
			reqLogger.Info(fmt.Sprintf("Cache %s already exists", instance.GetCacheName()))
			// Check if template matches?
		} else {
			reqLogger.Info(fmt.Sprintf("Cache %s doesn't exist, create it", instance.GetCacheName()))
			podName := podList.Items[0].Name
			templateName := instance.Spec.TemplateName
			if ispnInstance.Spec.Service.Type == infinispanv1.ServiceTypeCache && (templateName != "" || instance.Spec.Template != "") {
				errTemplate := fmt.Errorf("Cannot create a cache with a template in a CacheService cluster")
				reqLogger.Error(errTemplate, "Error creating cache")
				return reconcile.Result{}, err
			}
			if templateName != "" {
				err = cluster.CreateCacheWithTemplateName(instance.Spec.Name, templateName, podName)
				if err != nil {
					reqLogger.Error(err, "Error creating cache with template name")
					return reconcile.Result{}, err
				}
			} else {
				xmlTemplate := instance.Spec.Template
				if xmlTemplate == "" {
					xmlTemplate, err = caches.DefaultCacheTemplateXML(podName, ispnInstance, cluster, reqLogger)
				}
				if err != nil {
					reqLogger.Error(err, "Error getting default XML")
					return reconcile.Result{}, err
				}
				reqLogger.Info(xmlTemplate)
				err = cluster.CreateCacheWithTemplate(instance.Spec.Name, xmlTemplate, podName)
				if err != nil {
					reqLogger.Error(err, "Error in creating cache")
					return reconcile.Result{}, err
				}
			}
		}
	} else {
		reqLogger.Error(err, "Error validating cache exist")
		return reconcile.Result{}, err
	}

	// Search the service associated to the cluster
	serviceList := &corev1.ServiceList{}
	labelSelector = labels.SelectorFromSet(ispnctrl.LabelsResource(ispnInstance.Name, "infinispan-service"))
	listOps = &client.ListOptions{Namespace: ispnInstance.Namespace, LabelSelector: labelSelector}
	err = r.Client.List(context.TODO(), serviceList, listOps)
	if err != nil {
		reqLogger.Error(err, "failed to select cluster service")
		return reconcile.Result{}, err
	}

	statusUpdate := false
	if instance.Status.ServiceName != serviceList.Items[0].Name {
		instance.Status.ServiceName = serviceList.Items[0].Name
		statusUpdate = true
	}
	statusUpdate = instance.SetCondition("Ready", metav1.ConditionTrue, "") || statusUpdate
	if statusUpdate {
		reqLogger.Info("Update CR status with connection info")
		err = r.Client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to update Cache %s status", instance.Name))
			return reconcile.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	kubernetes = kube.NewKubernetesFromController(mgr)
	return ctrl.NewControllerManagedBy(mgr).
		For(&infinispanv2alpha1.Cache{}).
		Complete(r)
}
