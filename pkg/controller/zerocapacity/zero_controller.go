package zerocapacity

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnCtrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	ispnclient "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http/curl"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

// Adapter interface that allows the zero-capacity controller to interact with the underlying k8 resource
type Resource interface {
	// Returns the name of the Infinispan cluster CR that the zero-pod should join
	Cluster() string
	// The current execution phase of the controller
	Phase() Phase
	// Update the current state of the resource to reflect the most recent Phase
	UpdatePhase(phase Phase, phaseErr error) error
	// Transform any CR resources, adding defaults or updating fields for backwards-compatibility if required
	Transform() (bool, error)
	// Ensure that all prerequisite resources are available and create any required resources before returning the zero spec
	Init() (*Spec, error)
	// Perform the operation(s) that are required on the zero-capacity pod
	Exec(client http.HttpClient) error
	// Return true when the operation(s) have completed, otherwise false
	ExecStatus(client http.HttpClient) (Phase, error)
	// Utility method to return a metav1.Object in order to set the controller reference
	AsMeta() metav1.Object
}

type Reconciler interface {
	// The k8 struct being handled by this controller
	Type() runtime.Object
	// Create a new instance of the zero Resource wrapping the actual k8 type
	ResourceInstance(name types.NamespacedName, ctrl *Controller) (Resource, error)
}

type Spec struct {
	// The VolumeSpec to utilise on the zero-capacity pod
	Volume VolumeSpec
	// The spec to be used by the zero-capacity pod
	Container v1.InfinispanContainerSpec
	// The labels to apply to the zero-capacity pod
	PodLabels map[string]string
}

type VolumeSpec struct {
	// If true a chmod initContainer is added to the pod to update the permissions of the MountPath
	UpdatePermissions bool
	// Path within the container at which the volume should be mounted.
	MountPath string
	// The VolumeSource to utilise on the zero-capacity pod
	VolumeSource corev1.VolumeSource
}

type Controller struct {
	client.Client
	Name       string
	Reconciler Reconciler
	Kube       *kube.Kubernetes
	Log        logr.Logger
	Scheme     *runtime.Scheme
}

type Phase string

const (
	// ZeroInitializing means the request has been accepted by the system, but the underlying resources are still
	// being initialized.
	ZeroInitializing Phase = "Initializing"
	// ZeroInitialized means that all required resources have been initialized
	ZeroInitialized Phase = "Initialized"
	// ZeroRunning means that the required action has been initiated on the infinispan server.
	ZeroRunning Phase = "Running"
	// ZeroSucceeded means that the action on the server has completed and the zero pod has been terminated.
	ZeroSucceeded Phase = "Succeeded"
	// ZeroFailed means that the action failed on the infinispan server and the zero pod has terminated.
	ZeroFailed Phase = "Failed"
	// ZeroUnknown means that for some reason the state of the action could not be obtained, typically due
	// to an error in communicating with the underlying zero pod.
	ZeroUnknown Phase = "Unknown"
)

func CreateController(name string, reconciler Reconciler, mgr manager.Manager) error {
	r := &Controller{
		Name:       name,
		Client:     mgr.GetClient(),
		Reconciler: reconciler,
		Kube:       kube.NewKubernetesFromController(mgr),
		Log:        logf.Log.WithName(name),
		Scheme:     mgr.GetScheme(),
	}

	// Create a new controller
	c, err := controller.New(name, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource
	err = c.Watch(&source.Kind{Type: reconciler.Type()}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    reconciler.Type(),
	})
}

func (z *Controller) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reconciler := z.Reconciler
	resource := reflect.TypeOf(reconciler.Type()).Elem().Name()
	namespace := request.Namespace

	reqLogger := z.Log.WithValues("Request.Namespace", namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling " + resource)
	defer reqLogger.Info("----- End Reconciling " + resource)

	instance, err := reconciler.ResourceInstance(request.NamespacedName, z)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("Unable to fetch %s CR '%s': %w", resource, request.Name, err)
	}

	phase := instance.Phase()

	switch phase {
	case "":
		// Perform any transformations required on the CR for backwards-compatibility. Returning if a tranformation or error occurs
		if transformed, err := instance.Transform(); transformed || err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, instance.UpdatePhase(ZeroInitializing, nil)
	case ZeroInitializing:
		return z.initializeResources(request, instance)
	case ZeroSucceeded, ZeroFailed:
		return z.cleanupResources(request)
	}

	infinispan := &v1.Infinispan{}
	clusterName := instance.Cluster()
	clusterObjKey := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}
	if err := z.Get(context.Background(), clusterObjKey, infinispan); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("CR '%s' not found", clusterName)
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, fmt.Errorf("Unable to fetch CR '%s': %w", clusterName, err)
	}

	var credentials *ispnclient.Credentials
	if infinispan.IsAuthenticationEnabled() {
		user := consts.DefaultOperatorUser
		pass, err := users.PasswordFromSecret(user, infinispan.GetSecretName(), namespace, z.Kube)
		if err != nil {
			return reconcile.Result{}, err
		}

		credentials = &ispnclient.Credentials{
			Username: user,
			Password: pass,
		}
	}
	httpConfig := ispnclient.HttpConfig{
		Credentials: credentials,
		Namespace:   namespace,
		Protocol:    infinispan.GetEndpointScheme(),
	}
	httpClient := curl.New(httpConfig, z.Kube)

	if phase == ZeroInitialized {
		return z.execute(httpClient, request, instance)
	}
	// Phase must be ZeroRunning, so wait for execution to complete
	return z.waitForExecutionToComplete(httpClient, request, instance)
}

func (z *Controller) initializeResources(request reconcile.Request, instance Resource) (reconcile.Result, error) {
	ctx := context.Background()
	name := request.Name
	namespace := request.Namespace
	clusterName := instance.Cluster()
	clusterKey := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}

	infinispan := &v1.Infinispan{}
	if err := z.Client.Get(ctx, clusterKey, infinispan); err != nil {
		z.Log.Info(fmt.Sprintf("Unable to load Infinispan Cluster '%s': %w", clusterName, err))
		if errors.IsNotFound(err) {
			return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
		}
		return reconcile.Result{}, err
	}

	if err := infinispan.EnsureClusterStability(); err != nil {
		z.Log.Info(fmt.Sprintf("Infinispan '%s' not ready: %s", clusterName, err.Error()))
		return reconcile.Result{RequeueAfter: consts.DefaultWaitOnCluster}, nil
	}

	spec, err := instance.Init()
	if err != nil {
		return reconcile.Result{}, err
	}

	configMap, err := z.configureServer(name, namespace, infinispan, instance)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("Unable to create zero-capacity configuration: %w", err)
	}

	err = z.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &corev1.Pod{})
	if errors.IsNotFound(err) {
		pod := z.zeroPodSpec(name, namespace, configMap, infinispan, spec)
		if err := controllerutil.SetControllerReference(instance.AsMeta(), pod, z.Scheme); err != nil {
			return reconcile.Result{}, fmt.Errorf("Unable to setControllerReference for zero-capacity pod: %w", err)
		}

		if err := z.Create(ctx, pod); err != nil {
			return reconcile.Result{}, fmt.Errorf("Unable to create zero-capacity pod: %w", err)
		}
	}

	// Update status
	return reconcile.Result{}, instance.UpdatePhase(ZeroInitialized, nil)
}

func (z *Controller) execute(httpClient http.HttpClient, request reconcile.Request, instance Resource) (reconcile.Result, error) {
	if !z.isZeroPodReady(request) {
		// Don't requeue as reconcile request is received when the zero pod becomes ready
		return reconcile.Result{}, nil
	}

	if err := instance.Exec(httpClient); err != nil {
		z.Log.Error(err, "Unable to execute action on zero-capacity pod", "request.Name", request.Name)
		return reconcile.Result{}, instance.UpdatePhase(ZeroFailed, err)
	}

	return reconcile.Result{}, instance.UpdatePhase(ZeroRunning, nil)
}

func (z *Controller) waitForExecutionToComplete(httpClient http.HttpClient, request reconcile.Request, instance Resource) (reconcile.Result, error) {
	phase, err := instance.ExecStatus(httpClient)

	if err != nil || phase == ZeroFailed {
		z.Log.Error(err, "Execution failed", "request.Name", request.Name)
		return reconcile.Result{}, instance.UpdatePhase(ZeroFailed, err)
	}

	if phase == ZeroSucceeded {
		return reconcile.Result{}, instance.UpdatePhase(ZeroSucceeded, nil)
	}

	// Execution has not completed, or it's state is unknown, wait 1 second before retrying
	return reconcile.Result{RequeueAfter: 1 * time.Second}, nil
}

func (z *Controller) cleanupResources(request reconcile.Request) (reconcile.Result, error) {
	ctx := context.Background()
	meta := metav1.ObjectMeta{
		Namespace: request.Namespace,
		Name:      request.Name,
	}
	var allErrors error

	// Delete the zero-capacity pod so that it leaves the Infinispan cluster
	if err := z.Delete(ctx, &corev1.Pod{ObjectMeta: meta}); err != nil && !errors.IsNotFound(err) {
		allErrors = fmt.Errorf("Unable to delete zero-capacity pod: %w", err)
	}

	// Delete the configmap as it's no longer required
	if err := z.Delete(ctx, &corev1.ConfigMap{ObjectMeta: meta}); err != nil && !errors.IsNotFound(err) {
		allErrors = wrapErr(allErrors, fmt.Errorf("Unable to delete configMap: %w", err))
	}
	return reconcile.Result{}, nil
}

func wrapErr(old, new error) error {
	if old != nil {
		return fmt.Errorf("%w; %s", old, new.Error())
	}
	return new
}

func (z *Controller) isZeroPodReady(request reconcile.Request) bool {
	pod := &corev1.Pod{}
	key := types.NamespacedName{
		Name:      request.Name,
		Namespace: request.Namespace,
	}

	if err := z.Get(context.Background(), key, pod); err != nil {
		return false
	}
	return kube.IsPodReady(*pod)
}

func (z *Controller) zeroPodSpec(name, namespace string, configMap *corev1.ConfigMap, ispn *v1.Infinispan, zeroSpec *Spec) *corev1.Pod {
	dataVolName := name + "-data"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    zeroSpec.PodLabels,
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: pointer.Int64Ptr(1000600000),
			},
			Containers: []corev1.Container{{
				Image:          ispn.ImageName(),
				Name:           name,
				Env:            ispnCtrl.PodEnv(ispn, nil),
				LivenessProbe:  ispnCtrl.PodLivenessProbe(ispn),
				Ports:          ispnCtrl.PodPorts(ispn),
				ReadinessProbe: ispnCtrl.PodReadinessProbe(ispn),
				Resources:      ispnCtrl.PodResources(zeroSpec.Container),
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      ispnCtrl.ConfigVolumeName,
						MountPath: consts.ServerConfigRoot,
					},
					// Utilise Ephemeral vol as we're only interested in data related to CR
					{
						Name:      dataVolName,
						MountPath: ispnCtrl.DataMountPath,
					},
					// Mount configured volume at /zero path so that any created content is stored independent of server data
					{
						Name:      name,
						MountPath: zeroSpec.Volume.MountPath,
					},
				},
			}},
			Volumes: []corev1.Volume{
				// Volume for mounting zero-capacity yaml configmap
				{
					Name: ispnCtrl.ConfigVolumeName,
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: configMap.Name},
						},
					}},
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
				},
			},
		},
	}

	if zeroSpec.Volume.UpdatePermissions {
		ispnCtrl.AddVolumeChmodInitContainer("backup-chmod-pv", name, zeroSpec.Volume.MountPath, &pod.Spec)
	}

	ispnCtrl.AddVolumeForAuthentication(ispn, &pod.Spec)
	ispnCtrl.AddVolumeForEncryption(ispn, &pod.Spec)
	return pod
}

func (z *Controller) configureServer(name, namespace string, infinispan *v1.Infinispan, instance Resource) (*corev1.ConfigMap, error) {
	clusterConfig := &corev1.ConfigMap{}
	clusterConfigName := infinispan.GetConfigName()
	clusterKey := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterConfigName,
	}
	ctx := context.Background()
	if err := z.Client.Get(ctx, clusterKey, clusterConfig); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("Unable to load ConfigMap: %s", clusterConfigName)
		}
		return nil, err
	}

	yaml := clusterConfig.Data[consts.ServerConfigFilename]
	config, err := configuration.FromYaml(yaml)
	if err != nil {
		return nil, fmt.Errorf("Unable to parse existing config: %s", clusterConfigName)
	}

	config.Infinispan.ZeroCapacityNode = true

	if err := ispnCtrl.ConfigureServerEncryption(infinispan, config, z.Client); err != nil {
		return nil, fmt.Errorf("Unable to configure zero-capacity encryption: %w", err)
	}

	yaml, err = config.Yaml()
	if err != nil {
		return nil, fmt.Errorf("Unable to convert config to yaml: %w", err)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, z.Client, configMap, func() error {
		configMap.Data = map[string]string{consts.ServerConfigFilename: yaml}
		return controllerutil.SetControllerReference(instance.AsMeta(), configMap, z.Scheme)
	})

	if err != nil {
		return nil, fmt.Errorf("Unable to create ConfigMap '%s': %w", clusterConfigName, err)
	}
	return configMap, nil
}
