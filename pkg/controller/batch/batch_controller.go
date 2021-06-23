package batch

import (
	"context"
	"fmt"

	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	v2 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnCtrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	ControllerName  = "batch-controller"
	BatchFilename   = "batch"
	BatchVolumeName = "batch-volume"
	BatchVolumeRoot = "/etc/batch"
)

var (
	kubernetes *kube.Kubernetes
	log        = logf.Log.WithName(ControllerName)
	eventRec   record.EventRecorder
	ctx        = context.Background()
)

func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	kubernetes = kube.NewKubernetesFromController(mgr)
	eventRec = mgr.GetEventRecorderFor(ControllerName)
	return &reconcileBatch{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(ControllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Batch
	err = c.Watch(&source.Kind{Type: &v2.Batch{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to created jobs
	err = c.Watch(&source.Kind{Type: &batchv1.Job{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &v2.Batch{},
	})
	return err
}

// blank assignment to verify that ReconcileBatch implements reconcile.Reconciler
var _ reconcile.Reconciler = &reconcileBatch{}

// reconcileBatch reconciles a Batch object
type reconcileBatch struct {
	client.Client
	scheme *runtime.Scheme
}

type batchResource struct {
	instance    *v2.Batch
	client      client.Client
	scheme      *runtime.Scheme
	requestName types.NamespacedName
}

func (r *reconcileBatch) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Batch")

	// Fetch the Batch instance
	instance := &v2.Batch{}
	err := r.Get(ctx, request.NamespacedName, instance)
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
		scheme:      r.scheme,
		requestName: request.NamespacedName,
	}

	phase := instance.Status.Phase
	switch phase {
	case "":
		return batch.validate()
	case v2.BatchInitializing:
		return batch.initializeResources()
	case v2.BatchInitialized:
		return batch.execute()
	case v2.BatchRunning:
		return batch.waitToComplete()
	default:
		// Backup either succeeded or failed
		return reconcile.Result{}, nil
	}
}

func (r *batchResource) validate() (reconcile.Result, error) {
	spec := r.instance.Spec

	if spec.ConfigMap == nil && spec.Config == nil {
		return reconcile.Result{},
			r.UpdatePhase(v2.BatchFailed, fmt.Errorf("'Spec.config' OR 'spec.ConfigMap' must be configured"))
	}

	if spec.ConfigMap != nil && spec.Config != nil {
		return reconcile.Result{},
			r.UpdatePhase(v2.BatchFailed, fmt.Errorf("At most one of ['Spec.config', 'spec.ConfigMap'] must be configured"))
	}
	return reconcile.Result{}, r.UpdatePhase(v2.BatchInitializing, nil)
}

func (r *batchResource) initializeResources() (reconcile.Result, error) {
	batch := r.instance
	spec := batch.Spec
	// Ensure the Infinispan cluster exists
	infinispan := &v1.Infinispan{}
	if result, err := kube.LookupResource(spec.Cluster, batch.Namespace, infinispan, batch, r.client, log, eventRec); result != nil {
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
		_, err := controllerutil.CreateOrUpdate(ctx, r.client, configMap, func() error {
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
		batch.Status.Phase = v2.BatchInitialized
		return nil
	})
	return reconcile.Result{}, err
}

func (r *batchResource) execute() (reconcile.Result, error) {
	batch := r.instance
	infinispan := &v1.Infinispan{}
	if result, err := kube.LookupResource(batch.Spec.Cluster, batch.Namespace, infinispan, batch, r.client, log, eventRec); result != nil {
		return *result, err
	}

	expectedUid := *batch.Status.ClusterUID
	if infinispan.GetUID() != expectedUid {
		err := fmt.Errorf("Unable to execute Batch. Infinispan CR UUID has changed, expected '%s' observed '%s'", expectedUid, infinispan.GetUID())
		return reconcile.Result{}, r.UpdatePhase(v2.BatchFailed, err)
	}

	cliArgs := fmt.Sprintf("--properties '%s/%s' --file '%s/%s'", consts.ServerAdminIdentitiesRoot, consts.CliPropertiesFilename, BatchVolumeRoot, BatchFilename)

	labels := batchLabels(batch.Name)
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

	_, err := controllerutil.CreateOrUpdate(ctx, r.client, job, func() error {
		return controllerutil.SetControllerReference(batch, job, r.scheme)
	})

	if err != nil {
		return reconcile.Result{}, fmt.Errorf("Unable to create batch job '%s': %w", batch.Name, err)
	}
	return reconcile.Result{}, r.UpdatePhase(v2.BatchRunning, nil)
}

func (r *batchResource) waitToComplete() (reconcile.Result, error) {
	batch := r.instance
	job := &batchv1.Job{}
	if result, err := kube.LookupResource(batch.Name, batch.Namespace, job, batch, r.client, log, eventRec); result != nil {
		return *result, err
	}

	status := job.Status
	if status.Succeeded > 0 {
		return reconcile.Result{}, r.UpdatePhase(v2.BatchSucceeded, nil)
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
					reason, err = kubernetes.Logs(podName, batch.Namespace)
					if err != nil {
						reason = fmt.Errorf("Unable to retrive logs for batch %s: %w", batch.Name, err).Error()
					}
				}

				_, err = r.update(func() error {
					r.instance.Status.Phase = v2.BatchFailed
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

func (r *batchResource) UpdatePhase(phase v2.BatchPhase, phaseErr error) error {
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
	res, err := kube.CreateOrPatch(ctx, r.client, batch, func() error {
		if batch.CreationTimestamp.IsZero() {
			return errors.NewNotFound(v2.Resource("batch"), batch.Name)
		}
		return mutate()
	})
	return res != controllerutil.OperationResultNone, err
}

func batchLabels(name string) map[string]string {
	return map[string]string{
		"infinispan_batch": name,
	}
}

func GetJobPodName(name, namespace string, c client.Client) (string, error) {
	labelSelector := labels.SelectorFromSet(batchLabels(name))
	podList := &corev1.PodList{}
	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	if err := c.List(ctx, podList, listOps); err != nil {
		return "", fmt.Errorf("Unable to retrieve pod name for batch %s: %w", name, err)
	}

	if len(podList.Items) == 0 {
		return "", fmt.Errorf("No Batch job pods found")
	}
	return podList.Items[0].Name, nil
}
