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
	v2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnCtrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	zero "github.com/infinispan/infinispan-operator/pkg/controller/zerocapacity"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/backup"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	k8sctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	backupKubernetes *kube.Kubernetes
	backupEventRec   record.EventRecorder
	backupCtx        = context.Background()
)

const (
	BackupControllerName = "backup-controller"
	BackupDataMountPath  = "/opt/infinispan/backups"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type backupResource struct {
	instance *v2alpha1.Backup
	client   client.Client
	scheme   *runtime.Scheme
}

// +kubebuilder:rbac:groups=infinispan.org,resources=backups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infinispan.org,resources=backups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infinispan.org,resources=backups/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Backup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *BackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	// your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	backupEventRec = mgr.GetEventRecorderFor(BackupControllerName)
	return zero.CreateController(BackupControllerName, &BackupReconciler{mgr.GetClient(), mgr.GetLogger(), mgr.GetScheme()}, mgr, backupEventRec)
}

func (r *BackupReconciler) ResourceInstance(key types.NamespacedName, ctrl *zero.Controller) (zero.Resource, error) {
	instance := &v2alpha1.Backup{}
	if err := ctrl.Get(backupCtx, key, instance); err != nil {
		return nil, err
	}

	return &backupResource{
		instance: instance,
		client:   r.Client,
		scheme:   ctrl.Scheme,
	}, nil
}

func (r *BackupReconciler) Type() client.Object {
	return &v2alpha1.Backup{}
}

func (r *backupResource) AsMeta() metav1.Object {
	return r.instance
}

func (r *backupResource) Cluster() string {
	return r.instance.Spec.Cluster
}

func (r *backupResource) Phase() zero.Phase {
	return zero.Phase(r.instance.Status.Phase)
}

func (r *backupResource) UpdatePhase(phase zero.Phase, phaseErr error) error {
	_, err := r.update(func() {
		backup := r.instance
		var reason string
		if phaseErr != nil {
			reason = phaseErr.Error()
		}
		backup.Status.Phase = v2alpha1.BackupPhase(phase)
		backup.Status.Reason = reason
	})
	return err
}

func (r *backupResource) Transform() (bool, error) {
	return r.update(func() {
		backup := r.instance
		backup.Spec.ApplyDefaults()
		resources := backup.Spec.Resources
		if resources == nil {
			return
		}

		if len(resources.CacheConfigs) > 0 {
			resources.Templates = resources.CacheConfigs
			resources.CacheConfigs = nil
		}

		if len(resources.Scripts) > 0 {
			resources.Tasks = resources.Scripts
			resources.Scripts = nil
		}
	})
}

func (r *backupResource) update(mutate func()) (bool, error) {
	backup := r.instance
	res, err := k8sctrlutil.CreateOrPatch(backupCtx, r.client, backup, func() error {
		if backup.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("backup.infinispan.org"), backup.Name)
		}
		mutate()
		return nil
	})
	return res != controllerutil.OperationResultNone, err
}

func (r *backupResource) Init() (*zero.Spec, error) {
	err := r.getOrCreatePvc()
	if err != nil {
		return nil, err
	}

	// Status is updated in the zero_controller when UpdatePhase is called
	r.instance.Status.PVC = fmt.Sprintf("pvc/%s", r.instance.Name)
	return &zero.Spec{
		Volume: zero.VolumeSpec{
			UpdatePermissions: true,
			MountPath:         BackupDataMountPath,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: r.instance.Name,
				},
			},
		},
		Container: r.instance.Spec.Container,
		PodLabels: BackupPodLabels(r.instance.Name, r.instance.Spec.Cluster),
	}, nil
}

func (r *backupResource) getOrCreatePvc() error {
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.client.Get(backupCtx, types.NamespacedName{
		Name:      r.instance.Name,
		Namespace: r.instance.Namespace,
	}, pvc)

	// If the pvc already exists simply return
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		return err
	}

	volumeSpec := r.instance.Spec.Volume
	var storage resource.Quantity
	if volumeSpec.Storage == nil {
		// TODO calculate based upon number of Pods in cluster
		// ISPN- Utilise backup size estimate
		storage = constants.DefaultPVSize
	} else {
		storage, err = resource.ParseQuantity(*volumeSpec.Storage)
		if err != nil {
			return err
		}
	}

	// TODO add labels
	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.instance.Name,
			Namespace: r.instance.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storage,
				},
			},
			StorageClassName: volumeSpec.StorageClassName,
		},
	}
	if err = controllerutil.SetControllerReference(r.instance, pvc, r.scheme); err != nil {
		return err
	}
	if err = r.client.Create(backupCtx, pvc); err != nil {
		return fmt.Errorf("Unable to create pvc: %w", err)
	}
	return nil
}

func (r *backupResource) Exec(client http.HttpClient) error {
	instance := r.instance
	backupManager := backup.NewManager(instance.Name, client)
	var resources backup.Resources
	if instance.Spec.Resources == nil {
		resources = backup.Resources{}
	} else {
		resources = backup.Resources{
			Caches:       instance.Spec.Resources.Caches,
			Counters:     instance.Spec.Resources.Counters,
			ProtoSchemas: instance.Spec.Resources.ProtoSchemas,
			Templates:    instance.Spec.Resources.Templates,
			Tasks:        instance.Spec.Resources.Tasks,
		}
	}
	config := &backup.BackupConfig{
		Directory: BackupDataMountPath,
		Resources: resources,
	}
	return backupManager.Backup(instance.Name, config)
}

func (r *backupResource) ExecStatus(client http.HttpClient) (zero.Phase, error) {
	name := r.instance.Name
	backupManager := backup.NewManager(name, client)

	status, err := backupManager.BackupStatus(name)
	return zero.Phase(status), err
}

func BackupPodLabels(backup, cluster string) map[string]string {
	m := ispnCtrl.ServiceLabels(cluster)
	m["backup_cr"] = backup
	return m
}