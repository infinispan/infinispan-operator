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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	infinispanv2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnCtrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	BatchControllerName = "batch-controller"
	BatchFilename       = "batch"
	BatchVolumeName     = "batch-volume"
	BatchVolumeRoot     = "/etc/batch"
)

var (
	batchKubernetes *kube.Kubernetes
	batchEventRec   record.EventRecorder
	batchCtx        = context.Background()
)

// ReconcileBatch reconciles a Batch object
type ReconcileBatch struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type batchResource struct {
	instance    *v2alpha1.Batch
	client      client.Client
	scheme      *runtime.Scheme
	requestName types.NamespacedName
}

// +kubebuilder:rbac:groups=infinispan.org,resources=batches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infinispan.org,resources=batches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infinispan.org,resources=batches/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Batch object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *ReconcileBatch) Reconcile(batchCtx context.Context, request ctrl.Request) (ctrl.Result, error) {

	reqLogger := r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Batch")

	// Fetch the Batch instance
	instance := &infinispanv2alpha1.Batch{}
	err := r.Get(batchCtx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	batch := &batchResource{
		instance:    instance,
		client:      r.Client,
		scheme:      r.Scheme,
		requestName: request.NamespacedName,
	}

	phase := instance.Status.Phase
	switch phase {
	case "":
		return batch.validate()
	case infinispanv2alpha1.BatchInitializing:
		return batch.initializeResources(reqLogger)
	case infinispanv2alpha1.BatchInitialized:
		return batch.execute(reqLogger)
	case infinispanv2alpha1.BatchRunning:
		return batch.waitToComplete(reqLogger)
	default:
		// Backup either succeeded or failed
		return ctrl.Result{}, nil
	}

}

func (r *batchResource) validate() (reconcile.Result, error) {
	spec := r.instance.Spec

	if spec.ConfigMap == nil && spec.Config == nil {
		return reconcile.Result{},
			r.UpdatePhase(v2alpha1.BatchFailed, fmt.Errorf("'Spec.config' OR 'spec.ConfigMap' must be configured"))
	}

	if spec.ConfigMap != nil && spec.Config != nil {
		return reconcile.Result{},
			r.UpdatePhase(v2alpha1.BatchFailed, fmt.Errorf("At most one of ['Spec.config', 'spec.ConfigMap'] must be configured"))
	}
	return reconcile.Result{}, r.UpdatePhase(v2alpha1.BatchInitializing, nil)
}

func (r *batchResource) initializeResources(log logr.Logger) (reconcile.Result, error) {
	batch := r.instance
	spec := batch.Spec
	// Ensure the Infinispan cluster exists
	infinispan := &v1.Infinispan{}
	if result, err := kube.LookupResource(spec.Cluster, batch.Namespace, infinispan, r.client, log, batchEventRec); result != nil {
		return *result, err
	}

	if err := infinispan.EnsureClusterStability(); err != nil {
		log.Info(fmt.Sprintf("Infinispan '%s' not ready: %s", spec.Cluster, err.Error()))
		return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
	}

	if spec.ConfigMap == nil {
		// Create configMap
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      batch.Name,
				Namespace: batch.Namespace,
			},
		}
		_, err := controllerutil.CreateOrUpdate(batchCtx, r.client, configMap, func() error {
			configMap.Data = map[string]string{BatchFilename: *spec.Config}
			return controllerutil.SetControllerReference(batch, configMap, r.scheme)
		})

		if err != nil {
			return reconcile.Result{}, fmt.Errorf("Unable to create ConfigMap '%s': %w", configMap.Name, err)
		}

		updated, err := r.update(func() error {
			batch.Spec.ConfigMap = &batch.Name
			return nil
		})
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("Unable to update Batch .Spec: %w", err)
		}
		if updated {
			return reconcile.Result{}, nil
		}
	}

	// We update the phase separately to the spec as the status update is ignored when in the update mutate function
	_, err := r.update(func() error {
		batch.Status.ClusterUID = &infinispan.UID
		batch.Status.Phase = v2alpha1.BatchInitialized
		return nil
	})
	return reconcile.Result{}, err
}

func (r *batchResource) execute(log logr.Logger) (reconcile.Result, error) {
	batch := r.instance
	infinispan := &v1.Infinispan{}
	if result, err := kube.LookupResource(batch.Spec.Cluster, batch.Namespace, infinispan, r.client, log, batchEventRec); result != nil {
		return *result, err
	}

	expectedUid := *batch.Status.ClusterUID
	if infinispan.GetUID() != expectedUid {
		err := fmt.Errorf("Unable to execute Batch. Infinispan CR UUID has changed, expected '%s' observed '%s'", expectedUid, infinispan.GetUID())
		return reconcile.Result{}, r.UpdatePhase(v2alpha1.BatchFailed, err)
	}

	cliArgs := fmt.Sprintf("--properties '%s/%s' --file '%s/%s'", consts.ServerAdminIdentitiesRoot, consts.CliPropertiesFilename, BatchVolumeRoot, BatchFilename)

	labels := BatchLabels(batch.Name)
	infinispan.AddLabelsForPods(labels)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      batch.Name,
			Namespace: batch.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: pointer.Int32Ptr(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    batch.Name,
						Image:   infinispan.ImageName(),
						Command: []string{"/opt/infinispan/bin/cli.sh", cliArgs},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      BatchVolumeName,
								MountPath: BatchVolumeRoot,
							},
							{
								Name:      ispnCtrl.AdminIdentitiesVolumeName,
								MountPath: consts.ServerAdminIdentitiesRoot,
							},
						},
					}},
					RestartPolicy: corev1.RestartPolicyNever,
					Volumes: []corev1.Volume{
						// Volume for Batch ConfigMap
						{
							Name: BatchVolumeName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: *batch.Spec.ConfigMap},
								},
							},
						},
						// Volume for cli.properties
						{
							Name: ispnCtrl.AdminIdentitiesVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: infinispan.GetAdminSecretName(),
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := controllerutil.CreateOrUpdate(batchCtx, r.client, job, func() error {
		return controllerutil.SetControllerReference(batch, job, r.scheme)
	})

	if err != nil {
		return reconcile.Result{}, fmt.Errorf("Unable to create batch job '%s': %w", batch.Name, err)
	}
	return reconcile.Result{}, r.UpdatePhase(v2alpha1.BatchRunning, nil)
}

func (r *batchResource) waitToComplete(log logr.Logger) (reconcile.Result, error) {
	batch := r.instance
	job := &batchv1.Job{}
	if result, err := kube.LookupResource(batch.Name, batch.Namespace, job, r.client, log, batchEventRec); result != nil {
		return *result, err
	}

	status := job.Status
	if status.Succeeded > 0 {
		return reconcile.Result{}, r.UpdatePhase(v2alpha1.BatchSucceeded, nil)
	}

	if status.Failed > 0 {
		numConditions := len(status.Conditions)
		if numConditions > 0 {
			condition := status.Conditions[numConditions-1]

			if condition.Type == batchv1.JobFailed {
				var reason string
				podName, err := GetJobPodName(batch.Name, batch.Namespace, r.client)
				if err != nil {
					reason = err.Error()
				} else {
					reason, err = batchKubernetes.Logs(podName, batch.Namespace)
					if err != nil {
						reason = fmt.Errorf("Unable to retrive logs for batch %s: %w", batch.Name, err).Error()
					}
				}

				_, err = r.update(func() error {
					r.instance.Status.Phase = v2alpha1.BatchFailed
					r.instance.Status.Reason = reason
					return nil
				})
				return reconcile.Result{}, err
			}
		}
	}
	// The job has not completed, wait 1 second before retrying
	return reconcile.Result{}, nil
}

func (r *batchResource) UpdatePhase(phase v2alpha1.BatchPhase, phaseErr error) error {
	_, err := r.update(func() error {
		batch := r.instance
		var reason string
		if phaseErr != nil {
			reason = phaseErr.Error()
		}
		batch.Status.Phase = phase
		batch.Status.Reason = reason
		return nil
	})
	return err
}

func (r *batchResource) update(mutate func() error) (bool, error) {
	batch := r.instance
	res, err := k8sctrlutil.CreateOrPatch(batchCtx, r.client, batch, func() error {
		if batch.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("batch.infinispan.org"), batch.Name)
		}
		return mutate()
	})
	return res != controllerutil.OperationResultNone, err
}

func BatchLabels(name string) map[string]string {
	return map[string]string{
		"infinispan_batch": name,
		"app":              "infinispan-batch-pod",
	}
}

func GetJobPodName(name, namespace string, c client.Client) (string, error) {
	labelSelector := labels.SelectorFromSet(BatchLabels(name))
	podList := &corev1.PodList{}
	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	if err := c.List(batchCtx, podList, listOps); err != nil {
		return "", fmt.Errorf("Unable to retrieve pod name for batch %s: %w", name, err)
	}

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("No Batch job pods found")
	}
	return podList.Items[0].Name, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReconcileBatch) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infinispanv2alpha1.Batch{}).Owns(&batchv1.Job{}).
		Complete(r)
}
