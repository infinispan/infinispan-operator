package controllers

import (
	"context"
	"fmt"

	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/backup"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// +kubebuilder:rbac:groups=infinispan.org,resources=restores;restores/status;restores/finalizers,verbs=get;list;watch;create;update;patch

// RestoreReconciler reconciles a Restore object
type RestoreReconciler struct {
	client.Client
}

type restore struct {
	instance *v2alpha1.Restore
	client   client.Client
	scheme   *runtime.Scheme
	ctx      context.Context
}

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return newZeroCapacityController("Restore", &RestoreReconciler{mgr.GetClient()}, mgr)
}

func (r *RestoreReconciler) ResourceInstance(ctx context.Context, name types.NamespacedName, ctrl *zeroCapacityController) (zeroCapacityResource, error) {
	instance := &v2alpha1.Restore{}
	if err := ctrl.Get(ctx, name, instance); err != nil {
		return nil, err
	}

	restore := &restore{
		instance: instance,
		client:   r.Client,
		scheme:   ctrl.Scheme,
		ctx:      ctx,
	}
	return restore, nil
}

func (r *RestoreReconciler) Type() client.Object {
	return &v2alpha1.Restore{}
}

func (r *restore) AsMeta() metav1.Object {
	return r.instance
}

func (r *restore) Cluster() string {
	return r.instance.Spec.Cluster
}

func (r *restore) Phase() zeroCapacityPhase {
	return zeroCapacityPhase(string(r.instance.Status.Phase))
}

func (r *restore) UpdatePhase(phase zeroCapacityPhase, phaseErr error) error {
	_, err := r.update(func() {
		restore := r.instance
		var reason string
		if phaseErr != nil {
			reason = phaseErr.Error()
		}
		restore.Status.Phase = v2alpha1.RestorePhase(phase)
		restore.Status.Reason = reason
	})
	return err
}

func (r *restore) Transform() (bool, error) {
	return r.update(func() {
		restore := r.instance
		restore.Spec.ApplyDefaults()
		resources := restore.Spec.Resources
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

func (r *restore) update(mutate func()) (bool, error) {
	restore := r.instance
	res, err := controllerutil.CreateOrPatch(r.ctx, r.client, restore, func() error {
		if restore.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("restore.infinispan.org"), restore.Name)
		}
		mutate()
		return nil
	})
	return res != controllerutil.OperationResultNone, err
}

func (r *restore) Init() (*zeroCapacitySpec, error) {
	backup := &v2alpha1.Backup{}
	backupKey := types.NamespacedName{
		Namespace: r.instance.Namespace,
		Name:      r.instance.Spec.Backup,
	}

	if err := r.client.Get(r.ctx, backupKey, backup); err != nil {
		return nil, fmt.Errorf("unable to load Infinispan Backup '%s': %w", backupKey.Name, err)
	}

	return &zeroCapacitySpec{
		Container: r.instance.Spec.Container,
		PodLabels: RestorePodLabels(r.instance.Name, backup.Spec.Cluster),
		Volume: zeroCapacityVolumeSpec{
			MountPath: BackupDataMountPath,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: backup.Name,
					ReadOnly:  true,
				},
			},
		},
	}, nil
}

func (r *restore) Exec(client http.HttpClient) error {
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
	config := &backup.RestoreConfig{
		Location:  fmt.Sprintf("%[1]s/%[2]s/%[2]s.zip", BackupDataMountPath, instance.Spec.Backup),
		Resources: resources,
	}
	return backupManager.Restore(instance.Name, config)
}

func (r *restore) ExecStatus(client http.HttpClient) (zeroCapacityPhase, error) {
	name := r.instance.Name
	backupManager := backup.NewManager(name, client)

	status, err := backupManager.RestoreStatus(name)
	if err != nil {
		return ZeroUnknown, err
	}
	return zeroCapacityPhase(status), nil
}
