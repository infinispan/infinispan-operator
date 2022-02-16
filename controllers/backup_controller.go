package controllers

import (
	"context"
	"fmt"

	v2alpha1 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// +kubebuilder:rbac:groups=infinispan.org,namespace=infinispan-operator-system,resources=backups;backups/status;backups/finalizers,verbs=get;list;watch;create;update;patch

const (
	BackupDataMountPath = "/opt/infinispan/backups"
)

// BackupReconciler reconciles a Backup object
type BackupReconciler struct {
	client.Client
}

type backupResource struct {
	instance *v2alpha1.Backup
	client   client.Client
	scheme   *runtime.Scheme
	ctx      context.Context
}

// SetupWithManager sets up the controller with the Manager.
func (r *BackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return newZeroCapacityController("Backup", &BackupReconciler{mgr.GetClient()}, mgr)
}

func (r *BackupReconciler) ResourceInstance(ctx context.Context, key types.NamespacedName, ctrl *zeroCapacityController) (zeroCapacityResource, error) {
	instance := &v2alpha1.Backup{}
	if err := ctrl.Get(ctx, key, instance); err != nil {
		return nil, err
	}

	return &backupResource{
		instance: instance,
		client:   r.Client,
		scheme:   ctrl.Scheme,
		ctx:      ctx,
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

func (r *backupResource) Phase() zeroCapacityPhase {
	return zeroCapacityPhase(r.instance.Status.Phase)
}

func (r *backupResource) UpdatePhase(phase zeroCapacityPhase, phaseErr error) error {
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
		if backup.Spec.Container.Memory == "" {
			backup.Spec.Container.Memory = constants.DefaultMemorySize.String()
		}
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
	res, err := kube.CreateOrPatch(r.ctx, r.client, backup, func() error {
		if backup.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("backup.infinispan.org"), backup.Name)
		}
		mutate()
		return nil
	})
	return res != controllerutil.OperationResultNone, err
}

func (r *backupResource) Init() (*zeroCapacitySpec, error) {
	err := r.getOrCreatePvc()
	if err != nil {
		return nil, err
	}

	// Status is updated in the zero_controller when UpdatePhase is called
	r.instance.Status.PVC = fmt.Sprintf("pvc/%s", r.instance.Name)
	return &zeroCapacitySpec{
		Volume: zeroCapacityVolumeSpec{
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
	err := r.client.Get(r.ctx, types.NamespacedName{
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
	if err = r.client.Create(r.ctx, pvc); err != nil {
		return fmt.Errorf("unable to create pvc: %w", err)
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

func (r *backupResource) ExecStatus(client http.HttpClient) (zeroCapacityPhase, error) {
	name := r.instance.Name
	backupManager := backup.NewManager(name, client)

	status, err := backupManager.BackupStatus(name)
	return zeroCapacityPhase(status), err
}
