package restore

import (
	"context"
	"fmt"

	v2 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	zero "github.com/infinispan/infinispan-operator/pkg/controller/zerocapacity"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/backup"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	ControllerName = "restore-controller"
	DataMountPath  = "/opt/infinispan/restores"
)

var ctx = context.Background()

// ReconcileRestore reconciles a Restore object
type reconcileRestore struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client.Client
}

type restore struct {
	instance *v2.Restore
	client   client.Client
	scheme   *runtime.Scheme
}

func Add(mgr manager.Manager) error {
	return zero.CreateController(ControllerName, &reconcileRestore{mgr.GetClient()}, mgr)
}

func (r *reconcileRestore) ResourceInstance(name types.NamespacedName, ctrl *zero.Controller) (zero.Resource, error) {
	instance := &v2.Restore{}
	if err := ctrl.Get(ctx, name, instance); err != nil {
		return nil, err
	}

	restore := &restore{
		instance: instance,
		client:   r.Client,
		scheme:   ctrl.Scheme,
	}
	return restore, nil
}

func (r *reconcileRestore) Type() runtime.Object {
	return &v2.Restore{}
}

func (r *restore) AsMeta() metav1.Object {
	return r.instance
}

func (r *restore) Cluster() string {
	return r.instance.Spec.Cluster
}

func (r *restore) Phase() zero.Phase {
	return zero.Phase(string(r.instance.Status.Phase))
}

func (r *restore) UpdatePhase(phase zero.Phase, phaseErr error) error {
	_, err := r.update(func() {
		restore := r.instance
		var reason string
		if phaseErr != nil {
			reason = phaseErr.Error()
		}
		restore.Status.Phase = v2.RestorePhase(phase)
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
	res, err := kube.CreateOrPatch(ctx, r.client, restore, func() error {
		if restore.CreationTimestamp.IsZero() {
			return errors.NewNotFound(v2.Resource("restore"), restore.Name)
		}
		mutate()
		return nil
	})
	return res != controllerutil.OperationResultNone, err
}

func (r *restore) Init() (*zero.Spec, error) {
	backup := &v2.Backup{}
	backupKey := types.NamespacedName{
		Namespace: r.instance.Namespace,
		Name:      r.instance.Spec.Backup,
	}

	if err := r.client.Get(ctx, backupKey, backup); err != nil {
		return nil, fmt.Errorf("Unable to load Infinispan Backup '%s': %w", backupKey.Name, err)
	}

	return &zero.Spec{
		Container: r.instance.Spec.Container,
		PodLabels: PodLabels(r.instance.Name, backup.Spec.Cluster),
		Volume: zero.VolumeSpec{
			MountPath: DataMountPath,
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
		Location:  fmt.Sprintf("%[1]s/%[2]s/%[2]s.zip", DataMountPath, instance.Spec.Backup),
		Resources: resources,
	}
	return backupManager.Restore(instance.Name, config)
}

func (r *restore) ExecStatus(client http.HttpClient) (zero.Phase, error) {
	name := r.instance.Name
	backupManager := backup.NewManager(name, client)

	status, err := backupManager.RestoreStatus(name)
	if err != nil {
		return zero.ZeroUnknown, err
	}
	return zero.Phase(status), nil
}

func PodLabels(backup, cluster string) map[string]string {
	m := ispnctrl.ServiceLabels(cluster)
	m["restore_cr"] = backup
	return m
}
