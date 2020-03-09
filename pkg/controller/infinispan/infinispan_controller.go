package infinispan

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"github.com/infinispan/infinispan-operator/version"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

// DefaultImageName is used if a specific image name is not provided
var DefaultImageName = getEnvWithDefault("DEFAULT_IMAGE", "infinispan/server:latest")

// DefaultMemorySize string with default size for memory
var DefaultMemorySize = resource.MustParse("512Mi")

// DefaultCPUSize string with default size for CPU
var DefaultCPULimit int64 = 500

// DefaultPVSize default size for persistent volume
var DefaultPVSize = resource.MustParse("1Gi")

// Kubernetes object
var kubernetes *ispnutil.Kubernetes

// Cluster object
var cluster ispnutil.ClusterInterface

const CacheServiceFixedMemoryXmxMb = 200
const CacheServiceJvmNativeMb = 220
const CacheServiceMaxRamMb = CacheServiceFixedMemoryXmxMb + CacheServiceJvmNativeMb
const CacheServiceAdditionalJavaOptions = "-Dsun.zip.disableMemoryMapping=true -XX:+UseSerialGC -XX:MinHeapFreeRatio=5 -XX:MaxHeapFreeRatio=10"
const CacheServiceJvmNativePercentageOverhead = 1
const infinispanFinalizer = "finalizer.infinispan.org"

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
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
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
	reqLogger.Info("Reconciling Infinispan. Operator Version: " + version.Version)

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
		reqLogger.Info("Error reading the object")
		return reconcile.Result{}, err
	}

	// Check if the cluster must be deleted
	if infinispan.GetDeletionTimestamp() != nil {
		// Infinispan resource is to be deleted
		if contains(infinispan.GetFinalizers(), infinispanFinalizer) {
			if err := r.finalizeInfinispan(reqLogger, infinispan); err != nil {
				return reconcile.Result{}, err
			}

			infinispan.SetFinalizers(remove(infinispan.GetFinalizers(), infinispanFinalizer))
			err := r.client.Update(context.TODO(), infinispan)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Apply defaults if not already set
	applyDefaults(infinispan)

	// Check x-site configuration first.
	// Must be done before creating any Infinispan resources,
	// because remote site host:port combinations need to be injected into Infinispan.
	xsite, err := r.computeXSite(infinispan, reqLogger)
	if err != nil {
		reqLogger.Error(err, "Error in computeXSite", "Infinispan.Namespace")
		return reconcile.Result{}, err
	}

	// Check if the StatefulSet already exists, if not create a new one
	found := &appsv1.StatefulSet{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Configuring the StatefulSet")
		// Populate EndpointEncryption if serving cert service is available
		if getServingCertsMode(kubernetes) == "openshift.io" {
			reqLogger.Info("Serving certificate service present. Configuring into CRD")
			requeue := false
			ee := &infinispan.Spec.Security.EndpointEncryption
			if ee.Type == "" {
				ee.Type = "service"
				ee.CertServiceName = "service.beta.openshift.io"
				requeue = true
			}
			if ee.CertSecretName == "" {
				ee.CertSecretName = infinispan.Name + "-cert-secret"
				requeue = true
			}
			if requeue {
				reqLogger.Info("Updating Infinispan resource and requeing")
				if err = updateSecurity(infinispan, r.client, reqLogger); err != nil {
					return reconcile.Result{}, err
				}
				return reconcile.Result{Requeue: true}, nil
			}
		}

		secret, err := r.findSecret(infinispan)
		if err != nil && infinispan.Spec.Security.EndpointSecretName != "" {
			reqLogger.Error(err, "could not find secret", "Secret.Name", infinispan.Spec.Security.EndpointSecretName)
			return reconcile.Result{}, err
		}

		configMap, err := r.configMapForInfinispan(xsite, infinispan)
		if err != nil {
			reqLogger.Error(err, "could not create Infinispan configuration")
			return reconcile.Result{}, err
		}

		if secret == nil {
			reqLogger.Info("Creating identity secret")
			// Generate the identities secret if not provided by the user
			identities, err := ispnutil.GetCredentials()
			if err != nil {
				reqLogger.Error(err, "could not get identities for Secret")
				return reconcile.Result{}, err
			}

			secret = r.secretForInfinispan(identities, infinispan)
			reqLogger.Info("Creating a new Secret", "Secret.Name", secret.Name)
			err = r.client.Create(context.TODO(), secret)
			if err != nil {
				reqLogger.Error(err, "could not create a new Secret")
				return reconcile.Result{}, err
			}
			// Set the endpoint secret name and update .Spec.Security
			infinispan.Spec.Security.EndpointSecretName = secret.GetName()
			err = r.client.Update(context.TODO(), infinispan)
			if err != nil {
				reqLogger.Error(err, "failed to update Infinispan Spec")
				return reconcile.Result{}, err
			}
		}

		if err = updateSecurity(infinispan, r.client, reqLogger); err != nil {
			return reconcile.Result{}, err
		}

		reqLogger.Info("Creating a new ConfigMap", "ConfigMap.Name", configMap.Name)
		err = r.client.Create(context.TODO(), configMap)
		if err != nil {
			reqLogger.Error(err, "failed to create new ConfigMap")
			return reconcile.Result{}, err
		}

		// Define a new deployment
		dep, err := r.deploymentForInfinispan(infinispan, secret, configMap)
		if err != nil {
			reqLogger.Error(err, "failed to configure new StatefulSet")
			return reconcile.Result{}, err
		}
		reqLogger.Info("Creating a new StatefulSet", "StatefulSet.Name", dep.Name)
		err = r.client.Create(context.TODO(), dep)
		if err != nil {
			reqLogger.Error(err, "failed to create new StatefulSet", "StatefulSet.Name", dep.Name)
			return reconcile.Result{}, err
		}

		ser := r.serviceForInfinispan(infinispan)

		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan")
			return reconcile.Result{}, err
		}

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
		reqLogger.Info("Created Service", "Service", serDNS)

		if isExposed(infinispan) {
			externalService := r.serviceExternal(infinispan)
			err = r.client.Create(context.TODO(), externalService)
			if err != nil && !errors.IsAlreadyExists(err) {
				reqLogger.Error(err, "failed to create external Service", "Service", ser)
				return reconcile.Result{}, err
			}
			reqLogger.Info("Created External Service", "Service", externalService)
		}

		// Infinispan resource must have a finalizer
		if !contains(dep.GetFinalizers(), infinispanFinalizer) {
			r.addFinalizer(reqLogger, infinispan)
		}

		// StatefulSet created successfully - return and requeue
		reqLogger.Info("End of the StetefulSet creation")
		return reconcile.Result{Requeue: true}, nil
	} else if err != nil {
		reqLogger.Error(err, "failed to get StatefulSet")
		return reconcile.Result{}, err
	} else if upgradeNeeded(infinispan, reqLogger) {
		reqLogger.Info("Upgrade needed")
		err = r.destroyResources(infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to delete resources before upgrade")
			return reconcile.Result{}, err
		}

		infinispan.SetCondition("upgrade", "False", "")
		r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan status")
			return reconcile.Result{}, err
		}

		reqLogger.Info("removed Infinispan resources, force an upgrade now", "replicasWantedAtRestart", infinispan.Status.ReplicasWantedAtRestart)

		infinispan.Spec.Replicas = infinispan.Status.ReplicasWantedAtRestart
		r.client.Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan Spec")
			return reconcile.Result{}, err
		}

		return reconcile.Result{Requeue: true}, nil
	}

	reqLogger.Info("Reconciling Infinispan: update case")

	// List the pods for this infinispan's deployment
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(labelsForInfinispan(infinispan.Name, ""))
	listOps := &client.ListOptions{Namespace: infinispan.Namespace, LabelSelector: labelSelector}
	err = r.client.List(context.TODO(), podList, listOps)
	if err != nil {
		reqLogger.Error(err, "failed to list pods", "Infinispan.Namespace")
		return reconcile.Result{}, err
	}

	r.scheduleUpgradeIfNeeded(infinispan, podList, reqLogger)

	protocol := infinispanProtocol(infinispan)
	// If user set Spec.replicas=0 we need to perform a graceful shutdown
	// to preserve the data
	if infinispan.Spec.Replicas == 0 {
		reqLogger.Info(".Spec.Replicas==0")
		updateStatus := false
		if *found.Spec.Replicas != 0 {
			reqLogger.Info("StatefulSet.Spec.Replicas!=0")
			r.client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, infinispan)
			r.client.Get(context.TODO(), types.NamespacedName{Namespace: found.Namespace, Name: found.Name}, found)
			infinispan.Status.ReplicasWantedAtRestart = *found.Spec.Replicas
			err = r.client.Status().Update(context.TODO(), infinispan)
			if err != nil {
				reqLogger.Error(err, "failed to update Infinispan status")
				return reconcile.Result{}, err
			}

			// If here some pods are still ready
			// Disable restart policy if needed
			for _, pod := range podList.Items {
				if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
					runtimePod := &corev1.Pod{}
					r.client.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, runtimePod)
					runtimePod.Spec.RestartPolicy = corev1.RestartPolicyNever
					r.client.Update(context.TODO(), runtimePod)
				}
			}
			// If cluster hasn't a `stopping` condition or it's false then send a graceful shutdown
			if cond := infinispan.GetCondition("stopping"); cond == nil || *cond != "True" {
				reqLogger.Info("Sending graceful shutdown request")
				// send a graceful shutdown to the first ready pod
				// if there's no ready pod we're in trouble
				for _, pod := range podList.Items {
					if len(pod.Status.ContainerStatuses) > 0 && pod.Status.ContainerStatuses[0].Ready {
						err = cluster.GracefulShutdown(ispnutil.GetSecretName(infinispan), pod.GetName(), pod.GetNamespace(), protocol)
						if err != nil {
							reqLogger.Error(err, "failed to exec shutdown command on pod")
							continue
						}
						reqLogger.Info("Executed graceful shutdown on pod: ", "Pod.Name", pod.Name)
						infinispan.SetCondition("stopping", "True", "")
						infinispan.SetCondition("wellFormed", "False", "")
						r.client.Status().Update(context.TODO(), infinispan)
						if err != nil {
							reqLogger.Error(err, "This should not happens but failed to update Infinispan Status")
							return reconcile.Result{}, err
						}
						// Stop the work and requeue until cluster is down
						return reconcile.Result{Requeue: true, RequeueAfter: time.Second}, nil
					}
				}
			} else {
				// cluster has a `stopping` wait for all the pods becomes unready
				reqLogger.Info("Waiting that all the pods become unready")
				for _, pod := range podList.Items {
					if pod.Status.ContainerStatuses[0].Ready {
						reqLogger.Info("One or more pods still ready", "Pod.Name", pod.Name)
						// Stop the work and requeue until cluster is down
						return reconcile.Result{Requeue: true, RequeueAfter: time.Second}, nil
					}
				}
			}

			// If here all the pods are unready, set statefulset replicas and infinispan.replicas to 0
			if *found.Spec.Replicas != 0 {
				updateStatus = true
				zeroReplicas := int32(0)
				found.Spec.Replicas = &zeroReplicas
				r.client.Update(context.TODO(), found)
			}
		}
		if found.Status.CurrentReplicas == 0 {
			updateStatus = updateStatus || infinispan.SetCondition("gracefulShutdown", "True", "")
			updateStatus = updateStatus || infinispan.SetCondition("stopping", "False", "")
		}
		if updateStatus {
			r.client.Status().Update(context.TODO(), infinispan)
			if err != nil {
				reqLogger.Error(err, "failed to update Infinispan Status")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}
	isGracefulShutdown := infinispan.GetCondition("gracefulShutdown")
	if infinispan.Spec.Replicas != 0 && isGracefulShutdown != nil && *isGracefulShutdown == "True" {
		reqLogger.Info("Resuming from graceful shutdown")
		// If here we're resuming from graceful shutdown
		r.client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, infinispan)
		if infinispan.Spec.Replicas != infinispan.Status.ReplicasWantedAtRestart {
			return reconcile.Result{Requeue: true}, fmt.Errorf("Spec.Replicas(%d) must be 0 or equal to Status.ReplicasWantedAtRestart(%d)", infinispan.Spec.Replicas, infinispan.Status.ReplicasWantedAtRestart)
		}
		infinispan.Spec.Replicas = infinispan.Status.ReplicasWantedAtRestart
		err = r.client.Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan Spec")
			return reconcile.Result{}, err
		}
		infinispan.Status.ReplicasWantedAtRestart = 0
		infinispan.SetCondition("gracefulShutdown", "False", "")
		err = r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan Status")
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true}, nil
	}

	// If upgrade required, do not process any further and handle upgrades
	if isUpgradeCondition(infinispan) {
		reqLogger.Info("isUpgradeCondition")
		return reconcile.Result{Requeue: true}, nil
	}

	// If secretName for identities has changed reprocess all the
	// identities secrets and then upgrade the cluster
	if infinispan.Spec.Security.EndpointSecretName != infinispan.Status.Security.EndpointSecretName {
		reqLogger.Info("Changed EndpointSecretName",
			"PrevName", infinispan.Status.Security.EndpointSecretName,
			"NewName", infinispan.Spec.Security.EndpointSecretName)
		secret, err := r.findSecret(infinispan)
		if err != nil {
			reqLogger.Error(err, "could not find secret", "Secret.Name", infinispan.Spec.Security.EndpointSecretName)
			return reconcile.Result{}, err
		}

		if secret == nil {
			// Generate the identities secret if not provided by the user
			identities, err := ispnutil.GetCredentials()
			if err != nil {
				reqLogger.Error(err, "could not get identities for Secret")
				return reconcile.Result{}, err
			}

			secret = r.secretForInfinispan(identities, infinispan)
			reqLogger.Info("Creating a new Secret", "Secret.Name", secret.Name)
			err = r.client.Create(context.TODO(), secret)
			if err != nil {
				reqLogger.Error(err, "could not create a new Secret")
				return reconcile.Result{}, err
			}
		}

		found.Spec.Template.ObjectMeta.Annotations["updateDate"] = time.Now().String()
		// Find and update secret in StatefulSet volume
		for i, volumes := range found.Spec.Template.Spec.Volumes {
			if volumes.Secret != nil && volumes.Name == "identities-volume" {
				found.Spec.Template.Spec.Volumes[i].Secret.SecretName = secret.GetName()
			}
		}

		err = r.client.Update(context.TODO(), found)
		if err != nil {
			reqLogger.Error(err, "failed to update StatefulSet")
			return reconcile.Result{}, err
		}
		// Get latest version of the infinispan crd
		r.client.Get(context.TODO(), types.NamespacedName{Namespace: infinispan.Namespace, Name: infinispan.Name}, infinispan)
		// Copy and update .Status.Security to match .Spec.Security
		infinispan.Spec.Security.DeepCopyInto(&infinispan.Status.Security)
		err = r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan Status")
			return reconcile.Result{}, err
		}
		reqLogger.Info("Security set",
			"Spec.EndpointSecretName", infinispan.Spec.Security.EndpointSecretName,
			"Status.EndpointSecretName", infinispan.Status.Security.EndpointSecretName)

		return reconcile.Result{}, nil
	}

	// Here where to reconcile with spec updates that reflect into
	// changes to statefulset.spec.container.

	updateNeeded := false
	// Ensure the deployment size is the same as the spec
	replicas := infinispan.Spec.Replicas
	previousReplicas := *found.Spec.Replicas
	if previousReplicas != replicas {
		found.Spec.Replicas = &replicas
		reqLogger.Info("replicas changed, update infinispan", "replicas", replicas, "previous replicas", previousReplicas)
		updateNeeded = true
	}

	// Changes to statefulset.spec.template.spec.containers[].resources
	res := &found.Spec.Template.Spec.Containers[0].Resources
	env := &found.Spec.Template.Spec.Containers[0].Env
	ispnContr := &infinispan.Spec.Container
	if ispnContr.Memory != "" {
		quantity := resource.MustParse(ispnContr.Memory)
		previousMemory := res.Requests["memory"]
		if quantity.Cmp(previousMemory) != 0 {
			res.Requests["memory"] = quantity
			res.Limits["memory"] = quantity
			reqLogger.Info("memory changed, update infinispan", "memory", quantity, "previous memory", previousMemory)
			updateNeeded = true
		}
	}
	if ispnContr.CPU != "" {
		cpuReq, cpuLim := cpuResources(infinispan)
		previousCPUReq := res.Requests["cpu"]
		previousCPULim := res.Limits["cpu"]
		if cpuReq.Cmp(previousCPUReq) != 0 || cpuLim.Cmp(previousCPULim) != 0 {
			res.Requests["cpu"] = cpuReq
			res.Limits["cpu"] = cpuLim
			reqLogger.Info("cpu changed, update infinispan", "cpuLim", cpuLim, "cpuReq", cpuReq, "previous cpuLim", previousCPULim, "previous cpuReq", previousCPUReq)
			updateNeeded = true
		}
	}

	extraJavaOptionsIndex := getEnvVarIndex("EXTRA_JAVA_OPTIONS", env)
	extraJavaOptions := ispnContr.ExtraJvmOpts
	previousExtraJavaOptions := (*env)[extraJavaOptionsIndex].Value
	if extraJavaOptions != previousExtraJavaOptions {
		(*env)[extraJavaOptionsIndex].Value = ispnContr.ExtraJvmOpts
		javaOptionsIndex := getEnvVarIndex("JAVA_OPTIONS", env)
		newJavaOptions, _ := r.javaOptions(infinispan)
		(*env)[javaOptionsIndex].Value = newJavaOptions
		reqLogger.Info("extra jvm options changed, update infinispan",
			"extra java options", extraJavaOptions,
			"previous extra java options", previousExtraJavaOptions,
			"full java options", newJavaOptions,
		)
		updateNeeded = true
	}
	if updateNeeded {
		reqLogger.Info("updateNeeded")
		err = r.client.Update(context.TODO(), found)
		if err != nil {
			reqLogger.Error(err, "failed to update StatefulSet", "StatefulSet.Name", found.Name)
			return reconcile.Result{}, err
		}
		// Spec updated - return and requeue
		return reconcile.Result{Requeue: true}, nil
	}
	// Update the Infinispan status with the pod status
	// Wait until all pods have ips assigned
	// Without those ips, it's not possible to execute next calls
	if !arePodIPsReady(podList) {
		reqLogger.Info("Pods IPs are not ready yet")
		return reconcile.Result{Requeue: true, RequeueAfter: time.Second}, nil
	}

	// Inspect the system and get the current Infinispan conditions
	currConds := getInfinispanConditions(podList.Items, infinispan, protocol, cluster)

	// Before updating reload the resource to avoid problems with status update
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: infinispan.Name, Namespace: infinispan.Namespace}, infinispan)
	if err != nil {
		reqLogger.Error(err, "failed to reload Infinispan status")
		return reconcile.Result{}, err
	}

	// Update the Infinispan status with the pod status
	changed := infinispan.SetConditions(currConds)
	infinispan.Status.StatefulSetName = found.ObjectMeta.Name
	if changed {
		err := r.client.Status().Update(context.TODO(), infinispan)
		if err != nil {
			reqLogger.Error(err, "failed to update Infinispan status")
			return reconcile.Result{}, err
		}
	}

	// View didn't form, requeue until view has formed
	if notClusterFormed(currConds, podList.Items, infinispan.Spec.Replicas) {
		reqLogger.Info("notClusterFormed")
		return reconcile.Result{Requeue: true, RequeueAfter: time.Second}, nil
	}
	if isCache(infinispan) && !existsCacheServiceDefaultCache(podList.Items[0].Name, infinispan, cluster) {
		reqLogger.Info("createDefaultCache")
		err := createCacheServiceDefaultCache(podList.Items[0].Name, infinispan, cluster, reqLogger)
		if err != nil {
			reqLogger.Error(err, "failed to create default cache for cache service")
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

func getEnvVarIndex(envVarName string, env *[]corev1.EnvVar) int {
	for i, value := range *env {
		if value.Name == envVarName {
			return i
		}
	}
	return 0
}

func updateSecurity(infinispan *infinispanv1.Infinispan, client client.Client, reqLogger logr.Logger) error {
	err := client.Update(context.TODO(), infinispan)
	if err != nil {
		reqLogger.Error(err, "failed to update Infinispan Spec")
		return err
	}
	// Copy and update .Status.Security to match .Spec.Security
	infinispan.Spec.Security.DeepCopyInto(&infinispan.Status.Security)
	err = client.Status().Update(context.TODO(), infinispan)
	if err != nil {
		reqLogger.Error(err, "failed to update Infinispan Status", "Infinispan.Namespace")
		return err
	}
	reqLogger.Info("Security set",
		"Spec.EndpointSecretName", infinispan.Spec.Security.EndpointSecretName,
		"Status.EndpointSecretName", infinispan.Status.Security.EndpointSecretName)
	return nil
}

func (r *ReconcileInfinispan) destroyResources(infinispan *infinispanv1.Infinispan) error {
	// TODO destroying all upgradable resources for recreation is too manual
	// Labels cannot easily be used to remove all resources with a given label.
	// Resource controller could be used to make this easier.
	// If all upgradable resources are controlled by the Stateful Set,
	// removing the Stateful Set should remove the rest.
	// Then, stateful set could be controlled by Infinispan to keep current logic.
	err := r.client.Delete(context.TODO(),
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.ObjectMeta.Name,
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.ObjectMeta.Name + "-configuration",
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.ObjectMeta.Name,
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      infinispan.ObjectMeta.Name + "-ping",
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getServiceExternalName(infinispan),
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	err = r.client.Delete(context.TODO(),
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getSiteServiceName(infinispan),
				Namespace: infinispan.ObjectMeta.Namespace,
			},
		})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

func upgradeNeeded(infinispan *infinispanv1.Infinispan, logger logr.Logger) bool {
	if isUpgradeCondition(infinispan) {
		stoppingCondition := infinispan.GetCondition("stopping")
		if stoppingCondition != nil {
			stoppingCompleted := *stoppingCondition == "False"
			if stoppingCompleted {
				if infinispan.Status.ReplicasWantedAtRestart > 0 {
					logger.Info("graceful shutdown after upgrade completed, continue upgrade process")
					return true
				} else {
					logger.Info("replicas to restart with not yet set, wait for graceful shutdown to complete")
					return false
				}
			} else {
				logger.Info("wait for graceful shutdown before update to complete")
				return false
			}
		}
	}

	return false
}

func isUpgradeCondition(infinispan *infinispanv1.Infinispan) bool {
	return isConditionTrue("upgrade", infinispan)
}

func isConditionTrue(name string, infinispan *infinispanv1.Infinispan) bool {
	upgradeCondition := infinispan.GetCondition(name)
	return upgradeCondition != nil && *upgradeCondition == "True"
}

func (r *ReconcileInfinispan) scheduleUpgradeIfNeeded(infinispan *infinispanv1.Infinispan, podList *corev1.PodList, logger logr.Logger) {
	if len(podList.Items) == 0 {
		return
	}

	if isUpgradeCondition(infinispan) {
		return
	}

	// All pods need to be ready for the upgrade to be scheduled
	// Handles brief window during which resources have been removed,
	//and old ones terminating while new ones are being created.
	// We don't want yet another upgrade to be scheduled then.
	if !areAllPodsReady(podList) {
		return
	}

	// Get default Infinispan image for a running Infinispan pod
	podDefaultImage := getPodDefaultImage(podList.Items[0].Spec.Containers[0])

	// Get Infinispan image that the operator creates
	desiredImage := DefaultImageName

	// If the operator's default image differs from the pod's default image,
	// schedule an upgrade by gracefully shutting down the current cluster.
	if podDefaultImage != desiredImage {
		logger.Info("schedule an Infinispan cluster upgrade",
			"pod default image", podDefaultImage,
			"desired image", desiredImage)
		infinispan.Spec.Replicas = 0
		r.client.Update(context.TODO(), infinispan)
		infinispan.SetCondition("upgrade", "True", "")
		r.client.Status().Update(context.TODO(), infinispan)
	}
}

// getPodDefaultImage returns an Infinispan pod's default image.
// If the default image cannot be found, it returns the running image.
func getPodDefaultImage(container corev1.Container) string {
	envs := container.Env
	for _, env := range envs {
		if env.Name == "DEFAULT_IMAGE" {
			return env.Value
		}
	}

	return container.Image
}

func areAllPodsReady(podList *corev1.PodList) bool {
	for _, pod := range podList.Items {
		containerStatuses := pod.Status.ContainerStatuses
		if len(containerStatuses) == 0 || !containerStatuses[0].Ready {
			return false
		}
	}

	return true
}

func existsCacheServiceDefaultCache(podName string, infinispan *infinispanv1.Infinispan, cluster ispnutil.ClusterInterface) bool {
	namespace := infinispan.ObjectMeta.Namespace
	secretName := ispnutil.GetSecretName(infinispan)
	protocol := infinispanProtocol(infinispan)
	return cluster.ExistsCache("default", secretName, podName, namespace, protocol)
}

func notClusterFormed(currConds []infinispanv1.InfinispanCondition, pods []corev1.Pod, replicas int32) bool {
	notFormed := currConds[0].Status != "True"
	notEnoughMembers := len(pods) < int(replicas)
	return notFormed || notEnoughMembers
}

func arePodIPsReady(pods *corev1.PodList) bool {
	for _, pod := range pods.Items {
		if pod.Status.PodIP == "" {
			return false
		}
	}

	return true
}

func applyDefaults(infinispan *infinispanv1.Infinispan) {
	if infinispan.Status.Conditions == nil {
		infinispan.Status.Conditions = []infinispanv1.InfinispanCondition{}
	}

	if infinispan.Spec.Service.Type == "" {
		infinispan.Spec.Service.Type = infinispanv1.ServiceTypeCache
	}
}

func infinispanProtocol(infinispan *infinispanv1.Infinispan) string {
	if infinispan.Spec.Security.EndpointEncryption.Type != "" {
		return "https"
	}
	return "http"
}

func createCacheServiceDefaultCache(podName string, infinispan *infinispanv1.Infinispan, cluster ispnutil.ClusterInterface, logger logr.Logger) error {
	namespace := infinispan.ObjectMeta.Namespace

	memoryLimitBytes, err := cluster.GetMemoryLimitBytes(podName, namespace)
	if err != nil {
		logger.Error(err, "unable to extract memory limit (bytes) from pod")
		return err
	}

	maxUnboundedMemory, err := cluster.GetMaxMemoryUnboundedBytes(podName, namespace)
	if err != nil {
		logger.Error(err, "unable to extract max memory unbounded from pod")
		return err
	}

	containerMaxMemory := maxUnboundedMemory
	if memoryLimitBytes < maxUnboundedMemory {
		containerMaxMemory = memoryLimitBytes
	}

	nativeMemoryOverhead := containerMaxMemory * (CacheServiceJvmNativePercentageOverhead / 100)
	evictTotalMemoryBytes :=
		containerMaxMemory -
			(CacheServiceJvmNativeMb * 1024 * 1024) -
			(CacheServiceFixedMemoryXmxMb * 1024 * 1024) -
			nativeMemoryOverhead

	logger.Info("calculated maximum off-heap size",
		"size", evictTotalMemoryBytes,
		"container max memory", containerMaxMemory,
		"memory limit (bytes)", memoryLimitBytes,
		"max memory bound", maxUnboundedMemory,
	)

	defaultCacheXML := `<infinispan><cache-container>
        <distributed-cache name="default" mode="SYNC" owners="1">
            <memory>
                <off-heap 
                    size="` + strconv.FormatUint(evictTotalMemoryBytes, 10) + `"
                    eviction="MEMORY"
                    strategy="REMOVE"
                />
            </memory>
            <partition-handling when-split="DENY_READ_WRITES" merge-policy="REMOVE_ALL" />
        </distributed-cache>
    </cache-container></infinispan>`

	secretName := ispnutil.GetSecretName(infinispan)
	protocol := infinispanProtocol(infinispan)
	return cluster.CreateCache("default", defaultCacheXML, secretName, podName, namespace, protocol)
}

func (r *ReconcileInfinispan) computeXSite(infinispan *infinispanv1.Infinispan, logger logr.Logger) (*ispnutil.XSite, error) {
	if hasSites(infinispan) {
		siteServiceName := getSiteServiceName(infinispan)
		siteService, err := r.getOrCreateSiteService(siteServiceName, infinispan, logger)
		if err != nil {
			logger.Error(err, "could not get or create site service")
			return nil, err
		}

		localSiteHost, localSitePort, err := getLocalSiteServiceHostPort(siteService, infinispan, logger)
		if err != nil {
			logger.Error(err, "error retrieving local x-site service information")
			return nil, err
		}

		if localSiteHost == "" {
			msg := "local x-site service host not yet available"
			logger.Info(msg)
			return nil, fmt.Errorf(msg)
		}

		logger.Info("local site service",
			"service name", siteServiceName,
			"host", localSiteHost,
			"port", localSitePort,
		)

		xsite := &ispnutil.XSite{
			Address: localSiteHost,
			Name:    infinispan.Spec.Service.Sites.Local.Name,
			Port:    localSitePort,
		}

		remoteLocations := findRemoteLocations(xsite.Name, infinispan)
		for _, remoteLocation := range remoteLocations {
			err := appendRemoteLocation(xsite, infinispan, &remoteLocation, logger)
			if err != nil {
				return nil, err
			}
		}

		logger.Info("x-site configured", "configuration", *xsite)
		return xsite, nil
	}

	return nil, nil
}

func findRemoteLocations(localSiteName string, infinispan *infinispanv1.Infinispan) (remoteLocations []infinispanv1.InfinispanSiteLocationSpec) {
	locations := infinispan.Spec.Service.Sites.Locations
	for _, location := range locations {
		if localSiteName != location.Name {
			remoteLocations = append(remoteLocations, location)
		}
	}

	return
}

func getLocalSiteServiceHostPort(service *corev1.Service, infinispan *infinispanv1.Infinispan, logger logr.Logger) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		externalIP, err := findNodePortExternalIP(infinispan)
		if err != nil {
			return "", 0, err
		}

		// If configuring NodePort, expect external IPs to be configured
		return externalIP, kubernetes.GetNodePort(service), nil
	case corev1.ServiceTypeLoadBalancer:
		return getLoadBalancerServiceHostPort(service, logger)
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}
}

func findNodePortExternalIP(infinispan *infinispanv1.Infinispan) (string, error) {
	localSiteName := infinispan.Spec.Service.Sites.Local.Name
	locations := infinispan.Spec.Service.Sites.Locations
	for _, location := range locations {
		if location.Name == localSiteName {
			masterURL, err := url.Parse(location.URL)
			if err != nil {
				return "", nil
			}
			host, _, err := net.SplitHostPort(masterURL.Host)
			if err != nil {
				return "", nil
			}
			return host, nil
		}
	}
	return "", fmt.Errorf("could not find node port external IP, check local site name matches one of the members")
}

func getLoadBalancerServiceHostPort(service *corev1.Service, logger logr.Logger) (string, int32, error) {
	port := service.Spec.Ports[0].Port

	// If configuring load balancer, look for external ingress
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		ingress := service.Status.LoadBalancer.Ingress[0]
		if ingress.IP != "" {
			return ingress.IP, port, nil
		}
		if ingress.Hostname != "" {
			// Resolve load balancer host
			ip, err := lookupHost(ingress.Hostname, logger)

			// Load balancer gets created asynchronously,
			// so it might take time for the status to be updated.
			return ip, port, err
		}
	}

	return "", port, nil
}

func lookupHost(host string, logger logr.Logger) (string, error) {
	addresses, err := net.LookupHost(host)
	if err != nil {
		logger.Error(err, "host does not resolve")
		return "", err
	}
	logger.Info("host resolved", "host", host, "addresses", addresses)
	return host, nil
}

// deploymentForInfinispan returns an infinispan Deployment object
func (r *ReconcileInfinispan) deploymentForInfinispan(m *infinispanv1.Infinispan, secret *corev1.Secret, configMap *corev1.ConfigMap) (*appsv1.StatefulSet, error) {
	reqLogger := log.WithValues("Request.Namespace", m.Namespace, "Request.Name", m.Name)
	lsPod := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-pod")
	// This field specifies the flavor of the
	// Infinispan cluster. "" is plain community edition (vanilla)
	var imageName string
	if m.Spec.Image != "" {
		imageName = m.Spec.Image
	} else {
		imageName = DefaultImageName
	}

	memory := DefaultMemorySize
	if m.Spec.Container.Memory != "" {
		memory = resource.MustParse(m.Spec.Container.Memory)
	}

	cpuRequests, cpuLimits := cpuResources(m)

	javaOptions, err := r.javaOptions(m)
	if err != nil {
		return nil, err
	}

	envVars := []corev1.EnvVar{
		{Name: "CONFIG_PATH", Value: "/etc/config/infinispan.yaml"},
		{Name: "IDENTITIES_PATH", Value: "/etc/security/identities.yaml"},
		{Name: "JAVA_OPTIONS", Value: javaOptions},
		{Name: "EXTRA_JAVA_OPTIONS", Value: m.Spec.Container.ExtraJvmOpts},
		{Name: "DEFAULT_IMAGE", Value: DefaultImageName},
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
	replicas := m.Spec.Replicas
	protocolScheme := corev1.URISchemeHTTP
	if infinispanProtocol(m) != "http" {
		protocolScheme = corev1.URISchemeHTTPS
	}
	dep := &appsv1.StatefulSet{

		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
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
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType},
			Selector: &metav1.LabelSelector{
				MatchLabels: lsPod,
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      lsPod,
					Annotations: map[string]string{"updateDate": time.Now().String()},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: imageName,
						Name:  "infinispan",
						Env:   envVars,
						LivenessProbe: &corev1.Probe{
							Handler:             ispnutil.ClusterStatusHandler(protocolScheme),
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
							Handler:             ispnutil.ClusterStatusHandler(protocolScheme),
							FailureThreshold:    5,
							InitialDelaySeconds: 10,
							PeriodSeconds:       10,
							SuccessThreshold:    1,
							TimeoutSeconds:      80},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"cpu":    cpuRequests,
								"memory": memory,
							},
							Limits: corev1.ResourceList{
								"cpu":    cpuLimits,
								"memory": memory,
							},
						},
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

	// Persistent vol size must exceed memory size
	// so that it can contain all the in memory data
	pvSize := DefaultPVSize
	if pvSize.Cmp(memory) < 0 {
		pvSize = memory
	}

	if isDataGrid(m) && m.Spec.Service.Container.Storage != "" {
		var pvErr error
		pvSize, pvErr = resource.ParseQuantity(m.Spec.Service.Container.Storage)
		if pvErr != nil {
			return nil, pvErr
		}
		if pvSize.Cmp(memory) < 0 {
			reqLogger.Info("WARNING: persistent volume size is less than memory size. Graceful shutdown may not work.", "Volume Size", pvSize, "Memory", memory)
		}
	}

	dep.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name,
			Namespace: m.ObjectMeta.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: pvSize,
				},
			},
		}}}

	// Adding persistent volume mount
	v := &dep.Spec.Template.Spec.Containers[0].VolumeMounts
	*v = append(*v, corev1.VolumeMount{
		Name:      m.ObjectMeta.Name,
		MountPath: "/opt/infinispan/server/data",
	})
	// Adding an init container that run chmod if needed
	if chmod, ok := os.LookupEnv("MAKE_DATADIR_WRITABLE"); ok && chmod == "true" {
		dep.Spec.Template.Spec.InitContainers = []corev1.Container{{
			Image:   "busybox",
			Name:    "chmod-pv",
			Command: []string{"sh", "-c", "chmod -R g+w /opt/infinispan/server/data"},
			VolumeMounts: []corev1.VolumeMount{{
				Name:      m.ObjectMeta.Name,
				MountPath: "/opt/infinispan/server/data",
			}},
		}}
	}
	setupVolumesForEncryption(m, dep)
	//	appendVolumes(m, dep)

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, dep, r.scheme)
	return dep, nil
}

func cpuResources(infinispan *infinispanv1.Infinispan) (resource.Quantity, resource.Quantity) {
	if infinispan.Spec.Container.CPU != "" {
		cpuLimits := resource.MustParse(infinispan.Spec.Container.CPU)
		cpuRequestsMillis := cpuLimits.MilliValue() / 2
		cpuRequests := toMilliDecimalQuantity(int64(cpuRequestsMillis))
		return cpuRequests, cpuLimits
	}

	cpuLimits := toMilliDecimalQuantity(DefaultCPULimit)
	cpuRequests := toMilliDecimalQuantity(DefaultCPULimit / 2)
	return cpuRequests, cpuLimits
}

func toMilliDecimalQuantity(value int64) resource.Quantity {
	return *resource.NewMilliQuantity(value, resource.DecimalSI)
}

func (r *ReconcileInfinispan) javaOptions(m *infinispanv1.Infinispan) (string, error) {
	switch m.Spec.Service.Type {
	case infinispanv1.ServiceTypeDataGrid:
		return m.Spec.Container.ExtraJvmOpts, nil
	case infinispanv1.ServiceTypeCache:
		javaOptions := fmt.Sprintf(
			"-Xmx%dM -Xms%dM -XX:MaxRAM=%dM %s %s",
			CacheServiceFixedMemoryXmxMb,
			CacheServiceFixedMemoryXmxMb,
			CacheServiceMaxRamMb,
			CacheServiceAdditionalJavaOptions,
			m.Spec.Container.ExtraJvmOpts,
		)
		return javaOptions, nil
	default:
		return "", fmt.Errorf("unknown service type '%s'", m.Spec.Service.Type)
	}
}

func getEncryptionSecretName(m *infinispanv1.Infinispan) string {
	return m.Spec.Security.EndpointEncryption.CertSecretName
}

func setupServiceForEncryption(m *infinispanv1.Infinispan, ser *corev1.Service) {
	ee := m.Spec.Security.EndpointEncryption
	if ee.Type == "service" {
		if strings.Contains(ee.CertServiceName, "openshift.io") {
			// Using platform service. Only OpenShift is integrated atm
			secretName := getEncryptionSecretName(m)
			if ser.ObjectMeta.Annotations == nil {
				ser.ObjectMeta.Annotations = map[string]string{}
			}
			ser.ObjectMeta.Annotations[ee.CertServiceName+"/serving-cert-secret-name"] = secretName
		}
	}
}

func setupVolumesForEncryption(m *infinispanv1.Infinispan, dep *appsv1.StatefulSet) {
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
		if strings.Contains(ee.CertServiceName, "openshift.io") {
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

func (r *ReconcileInfinispan) configMapForInfinispan(xsite *ispnutil.XSite, m *infinispanv1.Infinispan) (*corev1.ConfigMap, error) {
	name := m.ObjectMeta.Name
	namespace := m.ObjectMeta.Namespace

	loggingCategories := copyLoggingCategories(m)
	config := ispnutil.CreateInfinispanConfiguration(name, xsite, loggingCategories, namespace)
	err := setupConfigForEncryption(m, &config, r.client)
	if err != nil {
		return nil, err
	}
	configYaml, err := yaml.Marshal(config)
	if err != nil {
		return nil, err
	}
	lsConfigMap := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-configmap-configuration")
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-configuration",
			Namespace: namespace,
			Labels:    lsConfigMap,
		},
		Data: map[string]string{"infinispan.yaml": string(configYaml)},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, configMap, r.scheme)
	return configMap, nil
}

func copyLoggingCategories(infinispan *infinispanv1.Infinispan) map[string]string {
	categories := infinispan.Spec.Logging.Categories
	if categories != nil {
		copied := make(map[string]string, len(categories))
		for category, level := range categories {
			copied[category] = level
		}
		return copied
	}

	return make(map[string]string)
}

func (r *ReconcileInfinispan) findSecret(m *infinispanv1.Infinispan) (*corev1.Secret, error) {
	secretName := ispnutil.GetSecretName(m)
	return kubernetes.GetSecret(secretName, m.Namespace)
}

func (r *ReconcileInfinispan) secretForInfinispan(identities []byte, m *infinispanv1.Infinispan) *corev1.Secret {
	lsSecret := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-secret-identities")
	secretName := ispnutil.GetSecretName(m)
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsSecret,
		},
		Type:       corev1.SecretType("Opaque"),
		StringData: map[string]string{"identities.yaml": string(identities)},
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(m, secret, r.scheme)
	return secret
}

// labelsForInfinispan returns the labels that must me applied to the resource
func labelsForInfinispan(name, resourceType string) map[string]string {
	m := map[string]string{"infinispan_cr": name, "clusterName": name}
	if resourceType != "" {
		m["app"] = resourceType
	}
	return m
}

// getInfinispanConditions returns the pods status and a summary status for the cluster
func getInfinispanConditions(pods []corev1.Pod, m *infinispanv1.Infinispan, protocol string, cluster ispnutil.ClusterInterface) []infinispanv1.InfinispanCondition {
	var status []infinispanv1.InfinispanCondition
	var wellFormedErr error
	clusterViews := make(map[string]bool)
	var errors []string
	// Avoid to inspect the system if we're still waiting for the pods
	if int32(len(pods)) < m.Spec.Replicas {
		errors = append(errors, fmt.Sprintf("Running %d pods. Needed %d", len(pods), m.Spec.Replicas))
	} else {
		secretName := ispnutil.GetSecretName(m)
		for _, pod := range pods {
			var clusterView string
			var members []string
			if isPodReady(pod) {
				// If pod is ready query it for the cluster members
				members, wellFormedErr = cluster.GetClusterMembers(secretName, pod.Name, pod.Namespace, protocol)
				clusterView = strings.Join(members, ",")
				if wellFormedErr == nil {
					clusterViews[clusterView] = true
				} else {
					errors = append(errors, pod.Name+": "+wellFormedErr.Error())
				}
			} else {
				// Pod not ready, no need to query
				errors = append(errors, pod.Name+": pod not ready")
			}
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

func isPodReady(pod corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.ContainersReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *ReconcileInfinispan) serviceForInfinispan(m *infinispanv1.Infinispan) *corev1.Service {
	lsPodSelector := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-pod")
	lsService := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-service")
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name,
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsService,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: lsPodSelector,
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
	lsPodSelector := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-pod")
	lsService := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-service-ping")
	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.ObjectMeta.Name + "-ping",
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsService,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector:  lsPodSelector,
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
func (r *ReconcileInfinispan) serviceExternal(m *infinispanv1.Infinispan) *corev1.Service {
	lsPodSelector := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-pod")
	lsService := labelsForInfinispan(m.ObjectMeta.Name, "infinispan-service-external")
	externalServiceType := m.Spec.Expose.Type

	// An external service can be simply achieved with a LoadBalancer
	// that has same selectors as original service.
	externalServiceName := getServiceExternalName(m)
	externalService := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalServiceName,
			Namespace: m.ObjectMeta.Namespace,
			Labels:    lsService,
		},
		Spec: corev1.ServiceSpec{
			Type:     externalServiceType,
			Selector: lsPodSelector,
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

func getServiceExternalName(m *infinispanv1.Infinispan) string {
	return fmt.Sprintf("%s-external", m.ObjectMeta.Name)
}

func isDataGrid(infinispan *infinispanv1.Infinispan) bool {
	return infinispanv1.ServiceTypeDataGrid == infinispan.Spec.Service.Type
}

func isCache(infinispan *infinispanv1.Infinispan) bool {
	return infinispanv1.ServiceTypeCache == infinispan.Spec.Service.Type
}

func hasSites(infinispan *infinispanv1.Infinispan) bool {
	return isDataGrid(infinispan) && len(infinispan.Spec.Service.Sites.Locations) > 0
}

func isExposed(infinispan *infinispanv1.Infinispan) bool {
	return infinispan.Spec.Expose.Type != ""
}

func getSiteServiceName(infinispan *infinispanv1.Infinispan) string {
	return fmt.Sprintf("%v-site", infinispan.Name)
}

func (r *ReconcileInfinispan) getOrCreateSiteService(siteServiceName string, infinispan *infinispanv1.Infinispan, logger logr.Logger) (*corev1.Service, error) {
	siteService := &corev1.Service{}
	ns := types.NamespacedName{
		Name:      siteServiceName,
		Namespace: infinispan.ObjectMeta.Namespace,
	}
	err := r.client.Get(context.TODO(), ns, siteService)

	if errors.IsNotFound(err) {
		return r.createSiteService(siteServiceName, infinispan, logger)
	}

	if err != nil {
		return nil, err
	}

	return siteService, nil
}

func (r *ReconcileInfinispan) createSiteService(siteServiceName string, infinispan *infinispanv1.Infinispan, logger logr.Logger) (*corev1.Service, error) {
	lsPodSelector := labelsForInfinispan(infinispan.ObjectMeta.Name, "infinispan-pod")
	lsService := labelsForInfinispan(infinispan.ObjectMeta.Name, "infinispan-service-xsite")
	exposeSpec := infinispan.Spec.Service.Sites.Local.Expose
	exposeSpec.Selector = lsPodSelector

	switch exposeSpec.Type {
	case corev1.ServiceTypeNodePort:
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port:     7900,
				NodePort: 32660,
			},
		}
	case corev1.ServiceTypeLoadBalancer:
		exposeSpec.Ports = []corev1.ServicePort{
			{
				Port: 7900,
			},
		}
	}

	logger.Info("create exposed site service", "configuration", exposeSpec)

	siteService := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      siteServiceName,
			Namespace: infinispan.ObjectMeta.Namespace,
			Annotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-backend-protocol": "tcp",
			},
			Labels: lsService,
		},
		Spec: exposeSpec,
	}

	// Set Infinispan instance as the owner and controller
	controllerutil.SetControllerReference(infinispan, &siteService, r.scheme)

	err := r.client.Create(context.TODO(), &siteService)
	if err != nil && !errors.IsAlreadyExists(err) {
		return nil, err
	}

	return &siteService, nil
}

func appendRemoteLocation(xsite *ispnutil.XSite, infinispan *infinispanv1.Infinispan, remoteLocation *infinispanv1.InfinispanSiteLocationSpec, logger logr.Logger) error {
	restConfig, err := GetRemoteSiteRESTConfig(infinispan, remoteLocation, logger)
	if err != nil {
		return err
	}

	remoteKubernetes, err := ispnutil.NewKubernetesFromConfig(restConfig)
	if err != nil {
		logger.Error(err, "could not connect to remote location URL", "URL", remoteLocation.URL)
		return err
	}

	err = appendKubernetesRemoteLocation(xsite, infinispan, remoteLocation, remoteKubernetes, logger)
	if err != nil {
		return err
	}
	return nil
}

func GetRemoteSiteRESTConfig(infinispan *infinispanv1.Infinispan, location *infinispanv1.InfinispanSiteLocationSpec, logger logr.Logger) (*restclient.Config, error) {
	backupSiteURL, err := url.Parse(location.URL)
	if err != nil {
		return nil, err
	}

	// Copy URL to make modify it for backup access
	copyURL, err := url.Parse(backupSiteURL.String())
	if err != nil {
		return nil, err
	}

	// All remote sites locations are accessed via encrypted http
	copyURL.Scheme = "https"

	switch scheme := backupSiteURL.Scheme; scheme {
	case "minikube":
		return getMinikubeRESTConfig(copyURL.String(), location.SecretName, infinispan, logger)
	case "openshift":
		return getOpenShiftRESTConfig(copyURL.String(), location.SecretName, infinispan, logger)
	default:
		return nil, fmt.Errorf("backup site URL scheme '%s' not supported", scheme)
	}
}

func getMinikubeRESTConfig(masterURL string, secretName string, infinispan *infinispanv1.Infinispan, logger logr.Logger) (*restclient.Config, error) {
	logger.Info("connect to backup minikube cluster", "url", masterURL)

	config, err := clientcmd.BuildConfigFromFlags(masterURL, "")
	if err != nil {
		logger.Error(err, "unable to create REST configuration", "master URL", masterURL)
		return nil, err
	}

	secret, err := kubernetes.GetSecret(secretName, infinispan.ObjectMeta.Namespace)
	if err != nil {
		logger.Error(err, "unable to find Secret", "secret name", secretName)
		return nil, err
	}

	config.CAData = secret.Data["certificate-authority"]
	config.CertData = secret.Data["client-certificate"]
	config.KeyData = secret.Data["client-key"]

	return config, nil
}

func getOpenShiftRESTConfig(masterURL string, secretName string, infinispan *infinispanv1.Infinispan, logger logr.Logger) (*restclient.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags(masterURL, "")
	if err != nil {
		logger.Error(err, "unable to create REST configuration", "master URL", masterURL)
		return nil, err
	}

	// Skip-tls for accessing other OpenShift clusters
	config.Insecure = true

	secret, err := kubernetes.GetSecret(secretName, infinispan.ObjectMeta.Namespace)
	if err != nil {
		logger.Error(err, "unable to find Secret", "secret name", secretName)
		return nil, err
	}

	if token, ok := secret.Data["token"]; ok {
		config.BearerToken = string(token)
		return config, nil
	}

	return nil, fmt.Errorf("token required connect to OpenShift cluster")
}

func appendKubernetesRemoteLocation(xsite *ispnutil.XSite, infinispan *infinispanv1.Infinispan, remoteLocation *infinispanv1.InfinispanSiteLocationSpec, remoteKubernetes *ispnutil.Kubernetes, logger logr.Logger) error {
	siteServiceName := getSiteServiceName(infinispan)
	namespacedName := types.NamespacedName{Name: siteServiceName, Namespace: infinispan.Namespace}
	siteService := &corev1.Service{}
	err := remoteKubernetes.Client.Get(context.TODO(), namespacedName, siteService)
	if err != nil {
		logger.Error(err, "could not find x-site service in remote cluster", "site service name", siteServiceName)
		return err
	}

	host, port, err := getRemoteSiteServiceHostPort(siteService, remoteKubernetes, logger)
	if err != nil {
		logger.Error(err, "error retrieving remote x-site service information")
		return err
	}

	if host == "" {
		msg := "remote x-site service host not yet available"
		logger.Info(msg)
		return fmt.Errorf(msg)
	}

	logger.Info("remote site service",
		"service name", siteServiceName,
		"host", host,
		"port", port,
	)

	backupSite := ispnutil.BackupSite{
		Address: host,
		Name:    remoteLocation.Name,
		Port:    port,
	}

	xsite.Backups = append(xsite.Backups, backupSite)
	return nil
}

func getRemoteSiteServiceHostPort(service *corev1.Service, remoteKubernetes *ispnutil.Kubernetes, logger logr.Logger) (string, int32, error) {
	switch serviceType := service.Spec.Type; serviceType {
	case corev1.ServiceTypeNodePort:
		// If configuring NodePort, expect external IPs to be configured
		return remoteKubernetes.PublicIP(), remoteKubernetes.GetNodePort(service), nil
	case corev1.ServiceTypeLoadBalancer:
		return getLoadBalancerServiceHostPort(service, logger)
	default:
		return "", 0, fmt.Errorf("unsupported service type '%v'", serviceType)
	}
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

// getServingCertsMode returns a label that identify the kind of serving
// certs service is available. Returns 'openshift.io' for service-ca on openshift
func getServingCertsMode(remoteKubernetes *ispnutil.Kubernetes) string {
	if remoteKubernetes.HasServiceCAsCRDResource() {
		return "openshift.io"

		// Code to check if other modes of serving TLS cert service is available
		// can be added here
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) []string {
	for i, v := range list {
		if v == s {
			list = append(list[:i], list[i+1:]...)
		}
	}
	return list
}

func (r *ReconcileInfinispan) finalizeInfinispan(reqLogger logr.Logger, ispn *infinispanv1.Infinispan) error {
	las := labelsForInfinispan(ispn.Name, "infinispan-pod")
	labelSelector := labels.SelectorFromSet(las)
	lo := client.ListOptions{Namespace: ispn.Namespace, LabelSelector: labelSelector}
	deleteOptions := &client.DeleteAllOfOptions{ListOptions: lo}
	r.client.DeleteAllOf(context.TODO(), &corev1.PersistentVolumeClaim{}, deleteOptions)
	reqLogger.Info("Successfully finalized Infinispan")
	return nil
}

func (r *ReconcileInfinispan) addFinalizer(reqLogger logr.Logger, ispn *infinispanv1.Infinispan) error {
	reqLogger.Info("Adding Finalizer for the Infinispan")
	ispn.SetFinalizers(append(ispn.GetFinalizers(), infinispanFinalizer))

	// Update CR
	err := r.client.Update(context.TODO(), ispn)
	if err != nil {
		reqLogger.Error(err, "Failed to update Infinispan with finalizer")
		return err
	}
	return nil
}
