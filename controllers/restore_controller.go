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
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	zero "github.com/infinispan/infinispan-operator/pkg/controller/zerocapacity"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/backup"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	restoreKubernetes *kube.Kubernetes
	restoreEventRec   record.EventRecorder
	restoreCtx        = context.Background()
)

// RestoreReconciler reconciles a Restore object
type RestoreReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

type restore struct {
	instance *v2alpha1.Restore
	client   client.Client
	scheme   *runtime.Scheme
}

func (r *RestoreReconciler) ResourceInstance(name types.NamespacedName, ctrl *zero.Controller) (zero.Resource, error) {
	instance := &v2alpha1.Restore{}
	if err := ctrl.Get(restoreCtx, name, instance); err != nil {
		return nil, err
	}

	restore := &restore{
		instance: instance,
		client:   r.Client,
		scheme:   ctrl.Scheme,
	}
	return restore, nil
}

func (r *RestoreReconciler) Type() client.Object {
	return &v2alpha1.Restore{}
}

// +kubebuilder:rbac:groups=infinispan.org,resources=restores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infinispan.org,resources=restores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infinispan.org,resources=restores/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Restore object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	// your logic here

	return ctrl.Result{}, nil
}

// // SetupWithManager sets up the controller with the Manager.
// func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
// 	return ctrl.NewControllerManagedBy(mgr).
// 		For(&infinispanv2alpha1.Restore{}).
// 		Complete(r)
// }

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
	res, err := k8sctrlutil.CreateOrPatch(restoreCtx, r.client, restore, func() error {
		if restore.CreationTimestamp.IsZero() {
			return errors.NewNotFound(schema.ParseGroupResource("restore.infinispan.org"), restore.Name)
		}
		mutate()
		return nil
	})
	return res != controllerutil.OperationResultNone, err
}

func (r *restore) Init() (*zero.Spec, error) {
	backup := &v2alpha1.Backup{}
	backupKey := types.NamespacedName{
		Namespace: r.instance.Namespace,
		Name:      r.instance.Spec.Backup,
	}

	if err := r.client.Get(restoreCtx, backupKey, backup); err != nil {
		return nil, fmt.Errorf("Unable to load Infinispan Backup '%s': %w", backupKey.Name, err)
	}

	return &zero.Spec{
		Container: r.instance.Spec.Container,
		PodLabels: PodLabels(r.instance.Name, backup.Spec.Cluster),
		Volume: zero.VolumeSpec{
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

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	backupEventRec = mgr.GetEventRecorderFor(BackupControllerName)
	return zero.CreateController(BackupControllerName, &RestoreReconciler{mgr.GetClient(), mgr.GetLogger(), mgr.GetScheme()}, mgr, backupEventRec)
}
