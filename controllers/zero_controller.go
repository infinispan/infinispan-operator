package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Adapter interface that allows the zero-capacity controller to interact with the underlying k8 resource
type zeroCapacityResource interface {
	// Returns the name of the Infinispan cluster CR that the zero-pod should join
	Cluster() string
	// The current execution phase of the controller
	Phase() zeroCapacityPhase
	// Update the current state of the resource to reflect the most recent Phase
	UpdatePhase(phase zeroCapacityPhase, phaseErr error) error
	// Ensure that all prerequisite resources are av¬ailable and create any required resources before returning the zero spec
	Init() (*zeroCapacitySpec, error)
	// Perform the operation(s) that are required on the zero-capacity pod
	Exec(client api.Infinispan) error
	// Return true when the operation(s) have completed, otherwise false
	ExecStatus(api api.Infinispan) (zeroCapacityPhase, error)
	// Utility method to return a metav1.Object in order to set the controller reference
	AsMeta() metav1.Object
}

type zeroCapacityReconciler interface {
	// The k8 struct being handled by this controller
	Type() client.Object
	// Create a new instance of the zero Resource wrapping the actual k8 type
	ResourceInstance(ctx context.Context, name types.NamespacedName, ctrl *zeroCapacityController) (zeroCapacityResource, error)
}

type zeroCapacitySpec struct {
	// The VolumeSpec to utilise on the zero-capacity pod
	Volume zeroCapacityVolumeSpec
	// The spec to be used by the zero-capacity pod
	Container v1.InfinispanContainerSpec
	// The labels to apply to the zero-capacity pod
	PodLabels map[string]string
}

type zeroCapacityVolumeSpec struct {
	// If true a chmod initContainer is added to the pod to update the permissions of the MountPath
	UpdatePermissions bool
	// Path within the container at which the volume should be mounted.
	MountPath string
	// The VolumeSource to utilise on the zero-capacity pod
	VolumeSource corev1.VolumeSource
}

type zeroCapacityController struct {
	client.Client
	Name       string
	Reconciler zeroCapacityReconciler
	Kube       *kube.Kubernetes
	Log        logr.Logger
	Scheme     *runtime.Scheme
	EventRec   record.EventRecorder
}

type zeroCapacityPhase string

const (
	// ZeroInitializing means the request has been accepted by the system, but the underlying resources are still
	// being initialized.
	ZeroInitializing zeroCapacityPhase = "Initializing"
	// ZeroInitialized means that all required resources have been initialized
	ZeroInitialized zeroCapacityPhase = "Initialized"
	// ZeroRunning means that the required action has been initiated on the infinispan server.
	ZeroRunning zeroCapacityPhase = "Running"
	// ZeroSucceeded means that the action on the server has completed and the zero pod has been terminated.
	ZeroSucceeded zeroCapacityPhase = "Succeeded"
	// ZeroFailed means that the action failed on the infinispan server and the zero pod has terminated.
	ZeroFailed zeroCapacityPhase = "Failed"
	// ZeroUnknown means that for some reason the state of the action could not be obtained, typically due
	// to an error in communicating with the underlying zero pod.
	ZeroUnknown zeroCapacityPhase = "Unknown"
)

func newZeroCapacityController(name string, reconciler zeroCapacityReconciler, mgr ctrl.Manager) error {
	r := &zeroCapacityController{
		Name:       name,
		Client:     mgr.GetClient(),
		Reconciler: reconciler,
		Kube:       kube.NewKubernetesFromController(mgr),
		Log:        ctrl.Log.WithName("controllers").WithName(name),
		Scheme:     mgr.GetScheme(),
		EventRec:   mgr.GetEventRecorderFor(strings.ToLower(name) + "-controller"),
	}

	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{Reconciler: r}).
		For(reconciler.Type()).
		Owns(&corev1.Pod{}).
		Complete(r)
}

func (z *zeroCapacityController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reconciler := z.Reconciler
	resource := reflect.TypeOf(reconciler.Type()).Elem().Name()
	namespace := request.Namespace

	reqLogger := z.Log.WithValues("Request.Namespace", namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling " + resource)
	defer reqLogger.Info("----- End Reconciling " + resource)

	instance, err := reconciler.ResourceInstance(ctx, request.NamespacedName, z)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("unable to fetch %s CR '%s': %w", resource, request.Name, err)
	}

	phase := instance.Phase()
	switch phase {
	case "":
		return reconcile.Result{}, instance.UpdatePhase(ZeroInitializing, nil)
	case ZeroInitializing:
		return z.initializeResources(request, instance, ctx)
	}

	infinispan := &v1.Infinispan{}
	clusterName := instance.Cluster()
	clusterObjKey := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}
	if err := z.Get(ctx, clusterObjKey, infinispan); err != nil {
		if errors.IsNotFound(err) {
			if phase == ZeroSucceeded || phase == ZeroFailed {
				// If the cluster no longer exists and the operation has failed or succeeded already, no need todo anything
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, fmt.Errorf("CR '%s' not found", clusterName)
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("unable to fetch CR '%s': %w", clusterName, err)
	}

	podName := instance.AsMeta().GetName()
	ispnClient, err := NewInfinispanForPod(ctx, podName, infinispan, z.Kube)
	if err != nil {
		return reconcile.Result{}, err
	}

	switch phase {
	case ZeroInitialized:
		return z.execute(ispnClient, request, instance, ctx)
	case ZeroSucceeded, ZeroFailed:
		return z.cleanupResources(ispnClient, request, ctx)
	default:
		// Phase must be ZeroRunning, so wait for execution to complete
		return z.waitForExecutionToComplete(ispnClient, request, instance)
	}
}

func (z *zeroCapacityController) initializeResources(request reconcile.Request, instance zeroCapacityResource, ctx context.Context) (reconcile.Result, error) {
	name := request.Name
	namespace := request.Namespace
	clusterName := instance.Cluster()
	clusterKey := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}

	infinispan := &v1.Infinispan{}
	if err := z.Client.Get(ctx, clusterKey, infinispan); err != nil {
		z.Log.Info(fmt.Sprintf("Unable to load Infinispan Cluster '%s': %s", clusterName, err))
		if errors.IsNotFound(err) {
			return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
		}
		return reconcile.Result{}, err
	}

	if err := infinispan.EnsureClusterStability(); err != nil {
		z.Log.Info(fmt.Sprintf("Infinispan '%s' not ready: %s", clusterName, err.Error()))
		return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
	}

	podList := &corev1.PodList{}
	podLabels := infinispan.PodSelectorLabels()
	if err := z.Kube.ResourcesList(infinispan.Namespace, podLabels, podList, ctx); err != nil {
		z.Log.Error(err, "Failed to list pods")
		return reconcile.Result{}, err
	}
	podSecurityCtx := podList.Items[0].Spec.SecurityContext

	spec, err := instance.Init()
	if err != nil {
		return reconcile.Result{}, err
	}

	err = z.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &corev1.Pod{})
	if errors.IsNotFound(err) {
		pod, err := z.zeroPodSpec(name, namespace, podSecurityCtx, infinispan, spec)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to compute Spec for zero-capacity pod: %w", err)
		}
		if err := controllerutil.SetControllerReference(instance.AsMeta(), pod, z.Scheme); err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to setControllerReference for zero-capacity pod: %w", err)
		}

		if err := z.Create(ctx, pod); err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to create zero-capacity pod: %w", err)
		}
	}

	// Update status
	return reconcile.Result{}, instance.UpdatePhase(ZeroInitialized, nil)
}

func (z *zeroCapacityController) execute(ispnClient api.Infinispan, request reconcile.Request, instance zeroCapacityResource, ctx context.Context) (reconcile.Result, error) {
	if !z.isZeroPodReady(request, ctx) {
		// Don't requeue as reconcile request is received when the zero pod becomes ready
		return reconcile.Result{}, nil
	}

	if err := instance.Exec(ispnClient); err != nil {
		z.Log.Error(err, "unable to execute action on zero-capacity pod", "request.Name", request.Name)
		return reconcile.Result{}, instance.UpdatePhase(ZeroFailed, err)
	}

	return reconcile.Result{}, instance.UpdatePhase(ZeroRunning, nil)
}

func (z *zeroCapacityController) waitForExecutionToComplete(ispnClient api.Infinispan, request reconcile.Request, instance zeroCapacityResource) (reconcile.Result, error) {
	phase, err := instance.ExecStatus(ispnClient)

	if err != nil || phase == ZeroFailed {
		z.Log.Error(err, "execution failed", "request.Name", request.Name)
		return reconcile.Result{}, instance.UpdatePhase(ZeroFailed, err)
	}

	if phase == ZeroSucceeded {
		return reconcile.Result{}, instance.UpdatePhase(ZeroSucceeded, nil)
	}

	// Execution has not completed, or it's state is unknown, wait 1 second before retrying
	return reconcile.Result{RequeueAfter: 1 * time.Second}, nil
}

func (z *zeroCapacityController) cleanupResources(ispnClient api.Infinispan, request reconcile.Request, ctx context.Context) (reconcile.Result, error) {
	// Stop the zero-capacity server so that it leaves the Infinispan cluster
	if z.isZeroPodReady(request, ctx) {
		if err := ispnClient.Server().Stop(); err != nil {
			err = fmt.Errorf("unable to stop zero-capacity server: %w", err)
			z.Log.Error(err, "error encountered when cleaning up zero-capacity pod")
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func (z *zeroCapacityController) isZeroPodReady(request reconcile.Request, ctx context.Context) bool {
	pod := &corev1.Pod{}
	if err := z.Get(ctx, request.NamespacedName, pod); err != nil {
		return false
	}
	return kube.IsPodReady(*pod)
}

func (z *zeroCapacityController) zeroPodSpec(name, namespace string, podSecurityCtx *corev1.PodSecurityContext, ispn *v1.Infinispan, zeroSpec *zeroCapacitySpec) (*corev1.Pod, error) {
	podResources, err := PodResources(zeroSpec.Container)
	if err != nil {
		return nil, err
	}
	dataVolName := name + "-data"
	labels := ispn.PodLabels()
	labels["app"] = "infinispan-zero-pod"
	for k, v := range zeroSpec.PodLabels {
		labels[k] = v
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: ispn.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			SecurityContext: podSecurityCtx,
			Containers: []corev1.Container{{
				Image:          ispn.ImageName(),
				Name:           InfinispanContainer,
				Env:            PodEnv(ispn, &[]corev1.EnvVar{{Name: "IDENTITIES_BATCH", Value: consts.ServerOperatorSecurity + "/" + consts.ServerIdentitiesCliFilename}}),
				LivenessProbe:  PodLivenessProbe(),
				Ports:          PodPorts(),
				ReadinessProbe: PodReadinessProbe(),
				Resources:      *podResources,
				StartupProbe:   PodStartupProbe(),
				Args:           buildStartupArgs(nil, "true"),
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      ConfigVolumeName,
						MountPath: OperatorConfMountPath,
					},
					{
						Name:      AdminIdentitiesVolumeName,
						MountPath: consts.ServerAdminIdentitiesRoot,
					},
					// Utilise Ephemeral vol as we're only interested in data related to CR
					{
						Name:      dataVolName,
						MountPath: DataMountPath,
					},
					// Mount configured volume at /zero path so that any created content is stored independent of server data
					{
						Name:      name,
						MountPath: zeroSpec.Volume.MountPath,
					}, {
						Name:      InfinispanSecurityVolumeName,
						MountPath: consts.ServerOperatorSecurity,
					},
				},
			}},
			RestartPolicy: corev1.RestartPolicyNever,
			Volumes: []corev1.Volume{
				// Volume for mounting zero-capacity yaml configmap
				{
					Name: ConfigVolumeName,
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: ispn.GetConfigName()},
						},
					}},
				// Volume for admin credentials
				{
					Name: AdminIdentitiesVolumeName,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: ispn.GetAdminSecretName(),
						},
					},
				},
				// Volume for reading/writing data
				{
					Name:         name,
					VolumeSource: zeroSpec.Volume.VolumeSource,
				},
				// EmptyDir for Infinispan data volume as the zero-capacity node does not store traditional data
				{
					Name: dataVolName,
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				}, {
					Name: InfinispanSecurityVolumeName,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: ispn.GetInfinispanSecuritySecretName(),
						},
					},
				},
			},
		},
	}

	if zeroSpec.Volume.UpdatePermissions {
		AddVolumeChmodInitContainer("backup-chmod-pv", name, zeroSpec.Volume.MountPath, &pod.Spec)
	}

	AddVolumeForUserAuthentication(ispn, &pod.Spec)

	if ispn.IsEncryptionEnabled() {
		AddVolumesForEncryption(ispn, &pod.Spec)
	}
	return pod, nil
}
