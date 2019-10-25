package infinispan

import (
	"context"
	"encoding/json"
	"fmt"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"gopkg.in/yaml.v2"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"os"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sort"
	"strings"
	"time"
)

var log = logf.Log.WithName("controller_infinispan")

// DefaultImageName is used if a specific image name is not provided
var DefaultImageName = getEnvWithDefault("DEFAULT_IMAGE", "registry.hub.docker.com/infinispan/server")

// Kubernetes object
var kubernetes *ispnutil.Kubernetes

// Cluster object
var cluster *ispnutil.Cluster

// Add creates a new Infinispan Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	kubernetes = ispnutil.NewKubernetesFromController(mgr)
	cluster = ispnutil.NewCluster(kubernetes)
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

	if infinispan.Status.Conditions == nil {
		infinispan.Status.Conditions = []infinispanv1.InfinispanCondition{}
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1beta1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		identities, err := r.findCredentials(infinispan)
		if err != nil {
			reqLogger.Error(err, "could not find create identities")
			return reconcile.Result{}, err
		}

		secret := r.secretForInfinispan(identities, infinispan)
		reqLogger.Info("Creating a new Secret", "Secret.Namespace", secret.Namespace, "Secret.Name", secret.Name)
		err = r.client.Create(context.TODO(), secret)
		if err != nil {
			reqLogger.Error(err, "could not find or create identities Secret")
			return reconcile.Result{}, err
		}

		configMap, err := r.configMapForInfinispan(infinispan)
		if err != nil {
			reqLogger.Error(err, "could not create Infinispan configuration")
			return reconcile.Result{}, err
		}

		reqLogger.Info("Creating a new ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
		err = r.client.Create(context.TODO(), configMap)
		if err != nil {
			reqLogger.Error(err, "failed to create new ConfigMap", "ConfigMap.Namespace", configMap.Namespace, "ConfigMap.Name", configMap.Name)
			return reconcile.Result{}, err
		}

		// Define a new deployment
		dep := r.deploymentForInfinispan(infinispan, secret, configMap)
		reqLogger.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		err = r.client.Create(context.TODO(), dep)
		if err != nil {
			reqLogger.Error(err, "failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return reconcile.Result{}, err
		}

		ser := r.serviceForInfinispan(infinispan)
		setupServiceForEncryption(infinispan, ser)
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

		externalService := r.serviceExternal(ser, infinispan)
		err = r.client.Create(context.TODO(), externalService)
		if err != nil && !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, "failed to create external Service", "Service", ser)
			return reconcile.Result{}, err
		}

		// Deployment created successfully - return and requeue

		// Update Status.Security to match
		infinispan.Spec.Security.DeepCopyInto(&infinispan.Status.Security)
		err = r.client.Status().Update(context.TODO(), infinispan)

		return reconcile.Result{Requeue: true}, nil
	} else if err != nil {
		reqLogger.Error(err, "failed to get Deployment")
		return reconcile.Result{}, err
	}

	// Here where to reconcile with spec updates that not reflect into
	// changes to statefulset.spec.container

	// If secretName for identities has changed reprocess all the
	// identities secrets and then upgrade the cluster
	if infinispan.Spec.Security.EndpointSecret != infinispan.Status.Security.EndpointSecret {
		identities, err := r.findCredentials(infinispan)
		if err != nil {
			reqLogger.Error(err, "could not find create identities")
			return reconcile.Result{}, err
		}

		secret := r.secretForInfinispan(identities, infinispan)
		currSecret := &corev1.Secret{}
		r.client.Get(context.TODO(), types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}, currSecret)
		if currSecret == nil {
			reqLogger.Info("Creating a new Secret", "Secret.Namespace", secret.Namespace, "Secret.Name", secret.Name)
			err = r.client.Create(context.TODO(), secret)
		} else {
			reqLogger.Info("Updating Secret", "Secret.Namespace", secret.Namespace, "Secret.Name", secret.Name)
			currSecret.StringData = secret.StringData
			r.client.Update(context.TODO(), secret)
		}
		if err != nil {
			reqLogger.Error(err, "could not find or create identities Secret")
			return reconcile.Result{}, err
		}

		found.Spec.Template.ObjectMeta.Annotations["updateDate"] = time.Now().String()
		err = r.client.Update(context.TODO(), found)

		infinispan.Spec.Security.DeepCopyInto(&infinispan.Status.Security)
		err = r.client.Status().Update(context.TODO(), infinispan)

		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan", "Infinispan.Namespace", infinispan.Namespace, "Infinispan.Name", infinispan.Name)
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Here where to reconcile with spec updates that reflect into
	// changes to statefulset.spec.container.

	updateNeeded := false
	// Ensure the deployment size is the same as the spec
	replicas := infinispan.Spec.Replicas
	if *found.Spec.Replicas != replicas {
		found.Spec.Replicas = &replicas
		updateNeeded = true
	}

	// Changes to statefulset.spec.template.spec.containers[].resources
	res := &found.Spec.Template.Spec.Containers[0].Resources
	env := &found.Spec.Template.Spec.Containers[0].Env
	ispnContr := &infinispan.Spec.Container
	if ispnContr.Memory != "" {
		quantity := resource.MustParse(ispnContr.Memory)
		if quantity.Cmp(res.Requests["memory"]) != 0 {
			res.Requests["memory"] = quantity
			updateNeeded = true
		}
	}
	if ispnContr.CPU != "" {
		quantity := resource.MustParse(ispnContr.CPU)
		if quantity.Cmp(res.Requests["cpu"]) != 0 {
			res.Requests["cpu"] = quantity
			updateNeeded = true
		}
	}

	index := 0
	for i, value := range *env {
		if value.Name == "JAVA_OPTIONS" {
			index = i
		}
	}
	if (*env)[index].Value != ispnContr.ExtraJvmOpts {
		(*env)[index].Value = ispnContr.ExtraJvmOpts
		updateNeeded = true
	}
	if updateNeeded {
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
	var protocol string
	if infinispan.Spec.Security.EndpointEncryption.Type != "" {
		protocol = "https"
	} else {
		protocol = "http"
	}
	currConds := getInfinispanConditions(podList.Items, infinispan.Name, protocol, cluster)
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

	// View didn't form, requeue until view has formed
	if currConds[0].Status == "False" {
		return reconcile.Result{Requeue: true}, nil
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
func (r *ReconcileInfinispan) deploymentForInfinispan(m *infinispanv1.Infinispan, secret *corev1.Secret, configMap *corev1.ConfigMap) *appsv1beta1.StatefulSet {
	// This field specifies the flavor of the
	// Infinispan cluster. "" is plain community edition (vanilla)
	ls := labelsForInfinispan(m.ObjectMeta.Name)

	var imageName string
	if m.Spec.Image != "" {
		imageName = m.Spec.Image
	} else {
		imageName = DefaultImageName
	}

	memory := "512Mi"

	if m.Spec.Container.Memory != "" {
		memory = m.Spec.Container.Memory
	}

	cpu := "0.5"

	if m.Spec.Container.CPU != "" {
		cpu = m.Spec.Container.CPU
	}

	envVars := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: "/etc/config/infinispan.yaml"},
		{Name: "IDENTITIES_PATH", Value: "/etc/security/identities.yaml"},
		{Name: "JAVA_OPTIONS", Value: m.Spec.Container.ExtraJvmOpts},
	}

	// Adding additional variables listed in ADDITIONAL_VARS env var
	envVar, defined := os.LookupEnv("ADDITIONAL_VARS")
	if defined {
		var addVars []string
		err := json.Unmarshal([]byte(envVar), &addVars)
		if err == nil {
			for _, name := range addVars {
				value, defined := os.LookupEnv(name)
				if defined {
					envVars = append(envVars, corev1.EnvVar{Name: name, Value: value})
				}
			}
		}
	}

	dep := &appsv1beta1.StatefulSet{

		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1beta1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name,
			Namespace: m.ObjectMeta.Namespace,
			Annotations: map[string]string{"description": "Infinispan 10 (Ephemeral)",
				"iconClass":                      "icon-infinispan",
				"openshift.io/display-name":      "Infinispan 10 (Ephemeral)",
				"openshift.io/documentation-url": "http://infinispan.org/documentation/",
			},
			Labels: map[string]string{"template": "infinispan-ephemeral"},
		},
		Spec: appsv1beta1.StatefulSetSpec{
			UpdateStrategy: appsv1beta1.StatefulSetUpdateStrategy{Type: appsv1beta1.RollingUpdateStatefulSetStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      ls,
					Annotations: map[string]string{"updateDate": time.Now().String()},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: imageName,
						Name:  "infinispan",
						Env:   envVars,
						LivenessProbe: &corev1.Probe{
							Handler:             corev1.Handler{Exec: &corev1.ExecAction{Command: []string{"/opt/infinispan/bin/livenessProbe.sh"}}},
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       60,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Ports: []corev1.ContainerPort{
							{ContainerPort: 8888, Name: "ping", Protocol: corev1.ProtocolTCP},
							{ContainerPort: 11222, Name: "hotrod", Protocol: corev1.ProtocolTCP},
						},
						ReadinessProbe: &corev1.Probe{
							Handler:             corev1.Handler{Exec: &corev1.ExecAction{Command: []string{"/opt/infinispan/bin/readinessProbe.sh"}}},
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{"cpu": resource.MustParse(cpu),
							"memory": resource.MustParse(memory)}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config-volume",
							MountPath: "/etc/config",
						}, {
							Name:      "identities-volume",
							MountPath: "/etc/security",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config-volume",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: configMap.Name},
							},
						},
					}, {
						Name: "identities-volume",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: secret.Name,
							},
						},
					}},
				},
			},
		},
	}
	setupVolumesForEncryption(m, dep)
	//	appendVolumes(m, dep)

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, dep, r.scheme)
	return dep
}

func getEncryptionSecretName(m *infinispanv1.Infinispan) string {
	return m.Spec.Security.EndpointEncryption.CertSecretName
}

func setupServiceForEncryption(m *infinispanv1.Infinispan, ser *corev1.Service) {
	ee := m.Spec.Security.EndpointEncryption
	if ee.Type == "service" {
		if strings.Contains(ee.CertService, "openshift.io") {
			// Using platform service. Only OpenShift is integrated atm
			secretName := getEncryptionSecretName(m)
			if ser.ObjectMeta.Annotations == nil {
				ser.ObjectMeta.Annotations = map[string]string{}
			}
			ser.ObjectMeta.Annotations[ee.CertService+"/serving-cert-secret-name"] = secretName
		}
	}
}

func setupVolumesForEncryption(m *infinispanv1.Infinispan, dep *appsv1beta1.StatefulSet) {
	secretName := getEncryptionSecretName(m)
	if secretName != "" {
		v := &dep.Spec.Template.Spec.Volumes
		vm := &dep.Spec.Template.Spec.Containers[0].VolumeMounts
		*vm = append(*vm, corev1.VolumeMount{Name: "encrypt-volume", MountPath: "/etc/encrypt"})
		*v = append(*v, corev1.Volume{Name: "encrypt-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				}}})
	}
}

func setupConfigForEncryption(m *infinispanv1.Infinispan, c *ispnutil.InfinispanConfiguration, client client.Client) error {
	ee := m.Spec.Security.EndpointEncryption
	if ee.Type == "service" {
		if strings.Contains(ee.CertService, "openshift.io") {
			c.Keystore.CrtPath = "/etc/encrypt"
			c.Keystore.Path = "/opt/infinispan/server/conf/keystore"
			c.Keystore.Password = "password"
			c.Keystore.Alias = "server"
			return nil
		}
	}
	// Fetch the tls secret if name is provided
	tlsSecretName := getEncryptionSecretName(m)
	if tlsSecretName != "" {
		tlsSecret := &corev1.Secret{}
		err := client.Get(context.TODO(), types.NamespacedName{Name: tlsSecretName, Namespace: m.Namespace}, tlsSecret)
		if err != nil {
			reqLogger := log.WithValues("Infinispan.Namespace", m.Namespace, "Infinispan.Name", m.Name)
			if errors.IsNotFound(err) {
				reqLogger.Error(err, "Secret %s for endpoint encryption not found.", tlsSecretName)
				return err
			}
			reqLogger.Error(err, "Error in getting secret %s for endpoint encryption.", tlsSecretName)
			return err
		}
		if _, ok := tlsSecret.Data["keystore.p12"]; ok {
			// If user provide a keystore in secret then use it ...
			c.Keystore.Path = "/etc/encrypt/keystore.p12"
			c.Keystore.Password = string(tlsSecret.Data["password"])
			c.Keystore.Alias = string(tlsSecret.Data["alias"])
		} else {
			// ... else suppose tls.key and tls.crt are provided
			c.Keystore.CrtPath = "/etc/encrypt"
			c.Keystore.Path = "/opt/infinispan/server/conf/keystore"
			c.Keystore.Password = "password"
			c.Keystore.Alias = "server"
		}
	}
	return nil
}

func (r *ReconcileInfinispan) configMapForInfinispan(m *infinispanv1.Infinispan) (*corev1.ConfigMap, error) {
	name := m.ObjectMeta.Name
	namespace := m.ObjectMeta.Namespace

	config := ispnutil.CreateInfinispanConfiguration(name, namespace)
	err := setupConfigForEncryption(m, &config, r.client)
	if err != nil {
		return nil, err
	}
	configYaml, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}

	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-configuration",
			Namespace: namespace,
		},
		Data: map[string]string{"infinispan.yaml": string(configYaml)},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, configMap, r.scheme)
	return configMap, nil
}

func (r *ReconcileInfinispan) findCredentials(m *infinispanv1.Infinispan) ([]byte, error) {
	secretName := m.Spec.Security.EndpointSecret
	if secretName != "" {
		secret, err := kubernetes.GetSecret(secretName, m.ObjectMeta.Namespace)
		if err != nil {
			return nil, err
		}

		return secret.Data["identities.yaml"], nil
	}

	identities := ispnutil.CreateIdentities()
	data, err := yaml.Marshal(identities)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (r *ReconcileInfinispan) secretForInfinispan(identities []byte, m *infinispanv1.Infinispan) *corev1.Secret {
	secretName := ispnutil.GetSecretName(m.ObjectMeta.Name)
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.ObjectMeta.Namespace,
		},
		Type:       corev1.SecretType("Opaque"),
		StringData: map[string]string{"identities.yaml": string(identities)},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, secret, r.scheme)
	return secret
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

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(pods []corev1.Pod, name, protocol string, cluster ispnutil.ClusterInterface) []infinispanv1.InfinispanCondition {
	var status []infinispanv1.InfinispanCondition
	var wellFormedErr error
	clusterViews := make(map[string]bool)
	var errors []string
	secretName := ispnutil.GetSecretName(name)
	for _, pod := range pods {
		var clusterView string
		var members []string
		members, wellFormedErr = cluster.GetClusterMembers(secretName, pod.Name, pod.Namespace, protocol)
		clusterView = strings.Join(members, ",")
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

// ServiceExternal creates an external service that's linked to the internal service
func (r *ReconcileInfinispan) serviceExternal(internalService *corev1.Service, m *infinispanv1.Infinispan) *corev1.Service {
	// An external service can be simply achieved with a LoadBalancer
	// that has same selectors as original service.
	externalServiceName := fmt.Sprintf("%s-external", internalService.Name)
	externalService := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalServiceName,
			Namespace: m.ObjectMeta.Namespace,
		},
		Spec: corev1.ServiceSpec{
			// TODO: temporarily set to NodePort to make kind work (soon it'll be configurable)
			Type:     corev1.ServiceTypeNodePort,
			Selector: internalService.Spec.Selector,
			Ports: []corev1.ServicePort{
				{
					Port:       int32(11222),
					TargetPort: intstr.FromInt(11222),
					// Fix NodePort to match exported port in kind configuration
					NodePort: int32(30222),
				},
			},
		},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, externalService, r.scheme)
	return externalService
}

// getEnvWithDefault return GetEnv(name) if exists else
// return defVal
func getEnvWithDefault(name, defVal string) string {
	str := os.Getenv(name)
	if str != "" {
		return str
	}
	return defVal
}
