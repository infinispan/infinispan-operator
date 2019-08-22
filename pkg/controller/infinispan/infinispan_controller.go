package infinispan

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"

	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
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
const defaultJGroupsPingProtocol = "openshift.DNS_PING"

// DefaultImageName is used if a specific image name is not provided
var DefaultImageName = ispnutil.GetEnvWithDefault("DEFAULT_IMAGE", "jboss/infinispan-server:latest")

var ispnCliHelper *ispnutil.IspnCliHelper

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
		// Define or locate application user secret
		appSecret, err := r.secretForUser("developer", "app", infinispan, infinispan.Spec.Connector.Authentication, reqLogger)
		if err != nil {
			reqLogger.Error(err, "could not find or create application Secret")
			return reconcile.Result{}, err
		}

		// Define or locate management user secret
		mgmtSecret, err := r.secretForUser("admin", "mgmt", infinispan, infinispan.Spec.Management.Authentication, reqLogger)
		if err != nil {
			reqLogger.Error(err, "could not find or create management Secret")
			return reconcile.Result{}, err
		}

		// Define a new deployment
		dep := r.deploymentForInfinispan(infinispan, appSecret, mgmtSecret)
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
	replicas := infinispan.Spec.Replicas
	if *found.Spec.Replicas != replicas {
		found.Spec.Replicas = &replicas
		err = r.client.Update(context.TODO(), found)
		if err != nil {
			reqLogger.Error(err, "failed to update Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
			return reconcile.Result{}, err
		}
		// Spec updated - return and requeue
		return reconcile.Result{Requeue: true}, nil
	}

	// Update the Infinispan status with the pod status
	// List the pods for this infinispan's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labelsForInfinispanSelector(infinispan.Name))
	listOps := &client.ListOptions{Namespace: infinispan.Namespace, LabelSelector: labelSelector}
	err = r.client.List(context.TODO(), listOps, podList)
	if err != nil {
		reqLogger.Error(err, "failed to list pods", "Infinispan.Namespace", infinispan.Namespace, "Infinispan.Name", infinispan.Name)
		return reconcile.Result{}, err
	}

	if ispnCliHelper == nil {
		ispnCliHelper = ispnutil.NewIspnCliHelper()
	}

	currConds := getInfinispanConditions(ispnCliHelper, podList.Items)
	infinispan.Status.StatefulSetName = found.ObjectMeta.Name
	// Update status.Nodes if needed
	if !reflect.DeepEqual(currConds, infinispan.Status.Conditions) {
		infinispan.Status.Conditions = currConds
		err := r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan status")
			return reconcile.Result{}, err
		}
	}

	// Check if pods container runs the right image
	for _, item := range podList.Items {
		if len(item.Spec.Containers) == 1 {
			if !strings.HasSuffix(item.Spec.Containers[0].Image, infinispan.Spec.Image) {
				// TODO: invent a reconciliation policy if images doesn't match
				// Attention: this is something that can conflict with StatefulSet rolling upgrade
				reqLogger.Info("Pod " + item.Name + " runs wrong image " + item.Spec.Containers[0].Image + " != " + infinispan.Spec.Image)
			}
		}
	}

	return reconcile.Result{}, nil
}

// deploymentForInfinispan returns an infinispan Deployment object
func (r *ReconcileInfinispan) deploymentForInfinispan(m *infinispanv1.Infinispan, appSecret *corev1.Secret, mgmtSecret *corev1.Secret) *appsv1beta1.StatefulSet {
	// This field specifies the flavor of the
	// Infinispan cluster. "" is plain community edition (vanilla)
	ls := labelsForInfinispan(m.ObjectMeta.Name)

	var imageName string
	if m.Spec.Image != "" {
		imageName = m.Spec.Image
	} else {
		imageName = DefaultImageName
	}

	var mgmtUserRef = envVarFromSecret("username", mgmtSecret)
	var mgmtPassRef = envVarFromSecret("password", mgmtSecret)
	var appUserRef = envVarFromSecret("username", appSecret)
	var appPassRef = envVarFromSecret("password", appSecret)

	memory := "512Mi"

	if m.Spec.Container.Memory != "" {
		memory = m.Spec.Container.Memory
	}

	cpu := "0.5"

	if m.Spec.Container.CPU != "" {
		cpu = m.Spec.Container.CPU
	}

	envVars := []corev1.EnvVar{{Name: getImageVarNameFromOperatorEnv("MGMT_USER"), ValueFrom: mgmtUserRef},
		{Name: getImageVarNameFromOperatorEnv("MGMT_PASS"), ValueFrom: mgmtPassRef},
		{Name: getImageVarNameFromOperatorEnv("APP_USER"), ValueFrom: appUserRef},
		{Name: getImageVarNameFromOperatorEnv("APP_PASS"), ValueFrom: appPassRef},
		{Name: "IMAGE", Value: m.Spec.Image},
		{Name: "JGROUPS_PING_PROTOCOL", Value: ispnutil.GetEnvWithDefault("JGROUPS_PING_PROTOCOL", defaultJGroupsPingProtocol)},
		{Name: "OPENSHIFT_DNS_PING_SERVICE_NAME", Value: m.ObjectMeta.Name + "-ping"},
		{Name: getImageVarNameFromOperatorEnv("NUMBER_OF_INSTANCE"), Value: string(m.Spec.Replicas)},
		{Name: getImageVarNameFromOperatorEnv("JAVA_OPTS_VARNAME"), Value: m.Spec.Container.JvmOptionsAppend}}
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
						Args:  getEntryPointArgs(m),
						Env:   envVars,
						LivenessProbe: &corev1.Probe{Handler: corev1.Handler{Exec: &corev1.ExecAction{Command: []string{getProbes("liveness")}}},
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
						ReadinessProbe: &corev1.Probe{Handler: corev1.Handler{Exec: &corev1.ExecAction{Command: []string{getProbes("readiness")}}},
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{"cpu": resource.MustParse(cpu),
							"memory": resource.MustParse(memory)}},
					}},
				},
			},
		},
	}

	// If using a config map, attach a volume to the container and mount it under 'custom' dir inside the configuration folder
	if m.Config.SourceType == infinispanv1.ConfigMap {
		dep.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				MountPath: CustomConfigPath, Name: m.Config.SourceRef,
			},
		}

		dep.Spec.Template.Spec.Volumes = []corev1.Volume{{VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: m.Config.SourceRef}}}, Name: m.Config.SourceRef}}
	}

	appendVolumes(m, dep)

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, dep, r.scheme)
	return dep
}

func envVarFromSecret(key string, secret *corev1.Secret) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secret.Name},
			Key:                  key,
		},
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

var acceptedChars = []byte(",-./=@\\abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var alphaChars = acceptedChars[8:]

// getRandomStringForAuth generate a random string that can be used as a
// user or pass for Infinispan
func getRandomStringForAuth(size int) string {
	b := make([]byte, size)
	for i := range b {
		if i == 0 {
			b[0] = alphaChars[rand.Intn(len(alphaChars))]
		} else {
			b[i] = acceptedChars[rand.Intn(len(acceptedChars))]
		}
	}
	return string(b)
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

type clusterInterface interface {
	GetClusterMembers(ns, name string) (string, error)
}

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(ispnCliHelper clusterInterface, pods []corev1.Pod) []infinispanv1.InfinispanCondition {
	var status []infinispanv1.InfinispanCondition
	var wellFormedErr error
	clusterViews := make(map[string]bool)
	var errors []string
	for _, pod := range pods {
		var clusterView string
		clusterView, wellFormedErr = ispnCliHelper.GetClusterMembers(pod.Namespace, pod.Name)
		if wellFormedErr == nil {
			clusterViews[clusterView] = true
		} else {
			errors = append(errors, pod.Name+": "+wellFormedErr.Error())
		}
	}
	// Evaluating WellFormed condition
	wellformed := infinispanv1.InfinispanCondition{Type: "wellFormed"}
	views := make([]string, len(clusterViews))
	i := 0
	for k := range clusterViews {
		views[i] = k
		i++
	}
	sort.Strings(views)
	if len(errors) == 0 {
		if len(views) == 1 {
			wellformed.Status = "True"
			wellformed.Message = "View: " + views[0]
		} else {
			wellformed.Status = "False"
			wellformed.Message = "Views: " + strings.Join(views, ",")
		}
	} else {
		wellformed.Status = "Unknown"
		wellformed.Message = "Errors: " + strings.Join(errors, ",") + " Views: " + strings.Join(views, ",")
	}
	status = append(status, wellformed)
	return status
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
	volumeSecretName, defined := os.LookupEnv("VOLUME_SECRET_NAME")
	if defined {
		service.ObjectMeta.SetAnnotations(map[string]string{"service.alpha.openshift.io/serving-cert-secret-name": volumeSecretName})
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, service, r.scheme)

	return service
}

func (r *ReconcileInfinispan) secretForUser(user string, realm string, m *infinispanv1.Infinispan, authInfo infinispanv1.InfinispanAuthInfo, reqLogger logr.Logger) (*corev1.Secret, error) {
	if authInfo.Type == "Credentials" &&
		authInfo.SecretName != "" {
		secretFound := &corev1.Secret{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: authInfo.SecretName, Namespace: m.ObjectMeta.Namespace}, secretFound)
		if err == nil {
			return secretFound, nil
		}
		return nil, err
	}
	return r.createSecretForAuthentication(user, realm, m, reqLogger)
}

func (r *ReconcileInfinispan) createSecretForAuthentication(user, realm string, m *infinispanv1.Infinispan, reqLogger logr.Logger) (*corev1.Secret, error) {
	pass := getRandomStringForAuth(16)
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name + "-" + realm + "-generated-secret",
			Namespace: m.ObjectMeta.Namespace,
		},
		Type:       corev1.SecretType("Opaque"),
		StringData: map[string]string{"username": user, "password": pass},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, secret, r.scheme)

	reqLogger.Info("Creating a new Secret", "Secret.Name", secret.Name)
	err := r.client.Create(context.TODO(), secret)
	if err != nil {
		reqLogger.Error(err, "failed to create new Secret", "Secret.Name", secret.Name)
		return nil, err
	}
	return secret, nil
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

var envVarName = map[string]string{"JAVA_OPTS_VARNAME": "JAVA_OPTS"}

// getImageVarNameFromOperatorEnv maps default env var name to fit non default Infinispan image
// The Infinispan operator is supposed to work with different
// infinispan-based datagrid: these different flavors may need
// different environment variables set. This function will do the operator/image mapping
// relying on the environment:
// if "propName" env var is defined its value is used env var name in place of "propName"
// elseif envVarName[propName] exists then it's returned
// else propName is returned
func getImageVarNameFromOperatorEnv(propName string) string {
	envVar, defined := os.LookupEnv(propName)
	if defined {
		return envVar
	}
	if val, ok := envVarName[propName]; ok {
		return val
	}
	return propName
}

// getEntryPointArgs returns the arguments for the image entrypoint command
// Returns ENTRY_POINT_ARGS env var if defined otherwise the default value.
// Dafault value is not an empty string, so if an empty string is needed it must
// be explicitly passed to the operator (i.e. ENTRY_POINT_ARGS="")
func getEntryPointArgs(m *infinispanv1.Infinispan) []string {
	envVar, defined := os.LookupEnv("ENTRY_POINT_ARGS")
	if defined {
		var arr []string
		err := json.Unmarshal([]byte(envVar), &arr)
		if err == nil {
			return arr
		}
		// In case of error return default entry args
		logf.Log.Error(err, "Using default for ENTRY_POINT_ARGS. Error in parsing user value: (%s)", envVar)
	}
	var configPath string
	switch m.Config.SourceType {
	case infinispanv1.ConfigMap:
		configPath = ConfigMapping + "/" + m.Config.Name
	case infinispanv1.Internal:
		if m.Config.Name != "" {
			configPath = m.Config.Name
		} else {
			configPath = DefaultConfig
		}
	default:
		configPath = DefaultConfig
	}
	return []string{configPath, "-Djboss.default.jgroups.stack=dns-ping",
		"-Djgroups.dns_ping.dns_query=" + m.ObjectMeta.Name + "-ping." + m.ObjectMeta.Namespace + ".svc.cluster.local"}
}

// Specific definitions for different subkind of Infinispan cluster
var defaultProbesMap = map[string]string{"readiness": "/usr/local/bin/is_healthy.sh", "liveness": "/usr/local/bin/is_running.sh"}

func getProbes(probeName string) string {
	envVar, defined := os.LookupEnv("PROBES")
	if defined {
		var probesMap map[string]string
		err := json.Unmarshal([]byte(envVar), &probesMap)
		if err == nil {
			return probesMap[probeName]
		}
	}
	return defaultProbesMap[probeName]
}

func appendVolumes(m *infinispanv1.Infinispan, dep *appsv1beta1.StatefulSet) {
	var volumeMounts []corev1.VolumeMount
	envVar, defined := os.LookupEnv("VOLUME_MOUNTS")
	if defined {
		err := json.Unmarshal([]byte(envVar), &volumeMounts)
		if err != nil {
			// In case of error return default add nothing and exit
			logf.Log.Error(err, "No volume mounts added. Error in parsing user VOLUME_MOUNTS value: (%s)", envVar)
			return
		}
	}

	v := &dep.Spec.Template.Spec.Volumes

	volumeKeystore, defined := os.LookupEnv("VOLUME_KEYSTORE_NAME")
	if defined {
		*v = append(*v, corev1.Volume{
			Name:         volumeKeystore,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	volumeSecretName, defined := os.LookupEnv("VOLUME_SECRET_NAME")
	if defined {
		*v = append(*v, corev1.Volume{
			Name:         volumeSecretName,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: volumeSecretName}},
		})
	}

	var volumeClaims []corev1.PersistentVolumeClaim
	envVar, defined = os.LookupEnv("VOLUME_CLAIMS")
	if defined {
		err := json.Unmarshal([]byte(envVar), &volumeClaims)
		if err != nil {
			// In case of error return default add nothing
			logf.Log.Error(err, "No volume mounts added. Error in parsing user VOLUME_CLAIMS value: (%s)", envVar)
			return
		}
	}

	vm := &dep.Spec.Template.Spec.Containers[0].VolumeMounts
	for _, vol := range volumeMounts {
		*vm = append(*vm, vol)
	}

	vc := &dep.Spec.VolumeClaimTemplates
	for _, volClaim := range volumeClaims {
		*vc = append(*vc, volClaim)
	}

}
