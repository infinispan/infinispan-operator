package infinispan

import (
	"context"
	"reflect"

	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"

	appsv1beta1 "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_infinispan")

// Folder to map external config files
const ConfigMapping = "custom"
const CustomConfigPath = "/opt/jboss/infinispan-server/standalone/configuration/" + ConfigMapping
const DefaultConfig = "cloud.xml"
const DefaultUser = "infinispan"
const DefaultPass = "infinispan"

// DefaultImageName is used if a specific image name is not provided
const DefaultImageName = "jboss/infinispan-server:latest"

// Add creates a new Infinispan Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileInfinispan{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("infinispan-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Infinispan
	err = c.Watch(&source.Kind{Type: &infinispanv1.Infinispan{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Infinispan
	err = c.Watch(&source.Kind{Type: &appsv1beta1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &infinispanv1.Infinispan{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileInfinispan{}

// ReconcileInfinispan reconciles a Infinispan object
type ReconcileInfinispan struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a Infinispan object and makes changes based on the state read
// and what is in the Infinispan.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileInfinispan) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Infinispan")

	// Fetch the Infinispan instance
	infinispan := &infinispanv1.Infinispan{}
	err := r.client.Get(context.TODO(), request.NamespacedName, infinispan)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Infinispan resource not found. Ignoring since object must be deleted")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1beta1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		// Define a new deployment
		dep := r.deploymentForInfinispan(infinispan)
		reqLogger.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		err = r.client.Create(context.TODO(), dep)
		if err != nil {
			reqLogger.Error(err, "failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return reconcile.Result{}, err
		}

		ser := r.serviceForInfinispan(infinispan)
		err = r.client.Create(context.TODO(), ser)
		if err != nil && !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, "failed to create Service", "Service", ser)
			return reconcile.Result{}, err
		}

		serDNS := r.serviceForDNSPing(infinispan)
		err = r.client.Create(context.TODO(), serDNS)
		if err != nil && !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, "failed to create Service", "Service", serDNS)
			return reconcile.Result{}, err
		}
		// Deployment created successfully - return and requeue
		return reconcile.Result{Requeue: true}, nil
	} else if err != nil {
		reqLogger.Error(err, "failed to get Deployment")
		return reconcile.Result{}, err
	}

	// Ensure the deployment size is the same as the spec
	size := infinispan.Spec.Size
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		err = r.client.Update(context.TODO(), found)
		if err != nil {
			reqLogger.Error(err, "failed to update Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
			return reconcile.Result{}, err
		}
		// Spec updated - return and requeue
		return reconcile.Result{Requeue: true}, nil
	}

	// Update the Infinispan status with the pod names
	// List the pods for this infinispan's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labelsForInfinispanSelector(infinispan.Name))
	listOps := &client.ListOptions{Namespace: infinispan.Namespace, LabelSelector: labelSelector}
	err = r.client.List(context.TODO(), listOps, podList)
	if err != nil {
		reqLogger.Error(err, "failed to list pods", "Infinispan.Namespace", infinispan.Namespace, "Infinispan.Name", infinispan.Name)
		return reconcile.Result{}, err
	}
	podNames := getPodNames(podList.Items)

	// Update status.Nodes if needed
	if !reflect.DeepEqual(podNames, infinispan.Status.Nodes) {
		infinispan.Status.Nodes = podNames
		err := r.client.Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan status")
			return reconcile.Result{}, err
		}
	}

	// Check if pods container runs the right image
	for _, pod := range podList.Items {
		if len(pod.Spec.Containers) == 1 {
			if pod.Spec.Containers[0].Image != infinispan.Spec.Image {
				// TODO: invent a reconciliation policy if images doesn't match
				// reqLogger.Info("Pod " + pod.ObjectMeta.Name + " runs wrong image " + pod.Spec.Containers[0].Image + " != " + infinispan.Spec.Image)
			}
		}
	}

	return reconcile.Result{}, nil
}

// deploymentForInfinispan returns an infinispan Deployment object
func (r *ReconcileInfinispan) deploymentForInfinispan(m *infinispanv1.Infinispan) *appsv1beta1.StatefulSet {
	ls := labelsForInfinispan(m.ObjectMeta.Name)

	infinispanConfig := m.Config
	infinispanCloud := m.Cloud
	var configPath string

	switch infinispanConfig.SourceType {
	case infinispanv1.ConfigMap:
		configPath = ConfigMapping + "/" + infinispanConfig.Name
	case infinispanv1.Internal:
		if infinispanConfig.Name != "" {
			configPath = infinispanConfig.Name
		} else {
			configPath = DefaultConfig
		}
	default:
		configPath = DefaultConfig
	}

	var imageName string
	if m.Spec.Image != "" {
		imageName = m.Spec.Image
	} else {
		imageName = DefaultImageName
	}

	falseVal := false
	var appUser, appPass, mgmtUser, mgmtPass corev1.EnvVar
	if infinispanCloud.Secret != "" {
		appUser = corev1.EnvVar{
			Name: "APP_USER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: infinispanCloud.Secret},
					Key:                  "APP_USER",
					Optional:             &falseVal,
				},
			},
		}
		appPass = corev1.EnvVar{
			Name: "APP_PASS",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: infinispanCloud.Secret},
					Key:                  "APP_PASS",
					Optional:             &falseVal,
				},
			},
		}

		mgmtUser = corev1.EnvVar{
			Name: "MGMT_USER",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: infinispanCloud.Secret},
					Key:                  "MGMT_USER",
					Optional:             &falseVal,
				},
			},
		}
		mgmtPass = corev1.EnvVar{
			Name: "MGMT_PASS",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: infinispanCloud.Secret},
					Key:                  "MGMT_PASS",
					Optional:             &falseVal,
				},
			},
		}
	} else {
		appUser = corev1.EnvVar{
			Name:  "APP_USER",
			Value: "infinispan",
		}

		appPass = corev1.EnvVar{
			Name:  "APP_PASS",
			Value: "infinispan",
		}
		mgmtUser = corev1.EnvVar{
			Name:  "MGMT_USER",
			Value: "infinispan",
		}
		mgmtPass = corev1.EnvVar{
			Name:  "MGMT_PASS",
			Value: "infinispan",
		}
	}

	containerEnv := []corev1.EnvVar{
		{Name: "KUBERNETES_NAMESPACE", Value: m.ObjectMeta.Namespace}, // TODO this is the right place for namespace?
		{Name: "KUBERNETES_LABELS", Value: "clusterName=" + m.ObjectMeta.Name},
		appUser,
		appPass,
		mgmtUser,
		mgmtPass,
	}
	dep := &appsv1beta1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1beta1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name,
			Namespace: m.ObjectMeta.Namespace,
			Annotations: map[string]string{"description": "Infinispan 9 (Ephemeral)",
				"iconClass":                      "icon-infinispan",
				"openshift.io/display-name":      "Infinispan 9 (Ephemeral)",
				"openshift.io/documentation-url": "http://infinispan.org/documentation/",
			},
			Labels: map[string]string{"template": "infinispan-ephemeral"},
		},
		Spec: appsv1beta1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: imageName,
						Name:  "infinispan",
						Args: []string{configPath, "-Djboss.default.jgroups.stack=dns-ping",
							"-Djgroups.dns_ping.dns_query=" + m.ObjectMeta.Name + "-ping." + m.ObjectMeta.Namespace + ".svc.cluster.local"},
						Env: containerEnv,
						LivenessProbe: &corev1.Probe{Handler: corev1.Handler{Exec: &corev1.ExecAction{Command: []string{"/usr/local/bin/is_running.sh"}}},
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       60,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Ports: []corev1.ContainerPort{{ContainerPort: 8080, Name: "http", Protocol: corev1.ProtocolTCP},
							{ContainerPort: 9990, Name: "management", Protocol: corev1.ProtocolTCP},
							{ContainerPort: 8888, Name: "ping", Protocol: corev1.ProtocolTCP},
							{ContainerPort: 11222, Name: "hotrod", Protocol: corev1.ProtocolTCP},
						},
						ReadinessProbe: &corev1.Probe{Handler: corev1.Handler{Exec: &corev1.ExecAction{Command: []string{"/usr/local/bin/is_healthy.sh"}}},
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{"cpu": resource.MustParse("0.5"),
							"memory": resource.MustParse("512Mi")}},
					}},
					ServiceAccountName: "infinispan-operator",
				},
			},
		},
	}

	// If using a config map, attach a volume to the container and mount it under 'custom' dir inside the configuration folder
	if infinispanConfig.SourceType == infinispanv1.ConfigMap {
		dep.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				MountPath: CustomConfigPath, Name: infinispanConfig.SourceRef,
			},
		}

		dep.Spec.Template.Spec.Volumes = []corev1.Volume{{VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: infinispanConfig.SourceRef}}}, Name: infinispanConfig.SourceRef}}
	}
	// // If config provides a non empty cluster-secrets field mount the volume
	// if infinispanCloud.Secret != "" {
	// 	dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{MountPath: ClusterSecretsPath, Name: "cluster-secrets"})
	// 	dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
	// 		SecretName: infinispanConfig.ClusterSecrets}}, Name: infinispanConfig.ClusterSecrets})
	// }
	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, dep, r.scheme)
	return dep
}

// labelsForInfinispan returns the labels that must me applied to the pod
func labelsForInfinispan(name string) map[string]string {
	return map[string]string{"app": "infinispan-pod", "infinispan_cr": name, "clusterName": name}
}

// labelsForInfinispanSelector returns the labels for selecting the resources
// belonging to the given infinispan CR name.
func labelsForInfinispanSelector(name string) map[string]string {
	return map[string]string{"app": "infinispan-pod", "infinispan_cr": name}
}

// getPodNames returns the pod names of the array of pods passed in
func getPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

func (r *ReconcileInfinispan) serviceForInfinispan(m *infinispanv1.Infinispan) *corev1.Service {
	ls := labelsForInfinispan(m.ObjectMeta.Name)
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name,
			Namespace: m.ObjectMeta.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: ls,
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: 8080,
				},
				{
					Name: "hotrod",
					Port: 11222,
				},
			},
		},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, service, r.scheme)

	return service
}

func (r *ReconcileInfinispan) serviceForDNSPing(m *infinispanv1.Infinispan) *corev1.Service {
	ls := labelsForInfinispan(m.ObjectMeta.Name)
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name + "-ping",
			Namespace: m.ObjectMeta.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector:  ls,
			Ports: []corev1.ServicePort{
				{
					Name: "ping",
					Port: 8888,
				},
			},
		},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, service, r.scheme)

	return service
}
