package controllers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	. "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	"github.com/infinispan/infinispan-operator/pkg/http/curl"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/logging"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/upgrades"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type HotRodRollingUpgradeReconciler struct {
	client.Client
	scheme     *runtime.Scheme
	log        logr.Logger
	kubernetes *kube.Kubernetes
	eventRec   record.EventRecorder
}

type HotRodRollingUpgradeRequest struct {
	*HotRodRollingUpgradeReconciler
	ctx        context.Context
	infinispan *ispnv1.Infinispan
	reqLogger  logr.Logger
	// Curl client for the source statefulset
	curl               *curl.Client
	currentStatefulSet string
}

var nameRegexp = regexp.MustCompile(`(.*)-([\d]+$)`)

func (r *HotRodRollingUpgradeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	name := "hotrod-rolling-upgrade"

	r.Client = mgr.GetClient()
	r.log = ctrl.Log.WithName("controllers").WithName(strings.Title(name))
	r.scheme = mgr.GetScheme()
	r.kubernetes = kube.NewKubernetesFromController(mgr)
	r.eventRec = mgr.GetEventRecorderFor(name + "-controller")

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		For(&ispnv1.Infinispan{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(updateEvent event.UpdateEvent) bool {
				return true
			},
		}).
		Complete(r)
}

func (r *HotRodRollingUpgradeReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := r.log.WithValues("Reconciling", "HotRodRollingUpgrade", "Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Get associated infinispan resource
	ispn := &ispnv1.Infinispan{}
	if err := r.Get(ctx, request.NamespacedName, ispn); err != nil {
		if kerrors.IsNotFound(err) {
			reqLogger.Info("Infinispan CR not found")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("unable to fetch Infinispan CR %w", err)
	}
	// Check if the CR is configured to do Hot Rod rolling upgrades
	if ispn.Spec.Upgrades.Type != ispnv1.UpgradeTypeHotRodRolling {
		return reconcile.Result{}, nil
	}

	// Check if the running pod is running a container with a different image
	podList, err := PodList(ispn, r.kubernetes, ctx)
	if err != nil || len(podList.Items) == 0 {
		reqLogger.Info("No pods found")
		return ctrl.Result{}, nil
	}

	rollbackNeeded := false
	upgradeStatus := ispn.Status.HotRodRollingUpgradeStatus

	if upgradeStatus == nil && !isImageOutdated(podList) {
		reqLogger.Info("Pods already running correct version, skipping upgrade")
		return ctrl.Result{}, nil
	}

	if upgradeStatus != nil {

		sourcePods, err := PodsCreatedBy(ispn.Namespace, r.kubernetes, ctx, upgradeStatus.SourceStatefulSetName)
		if err != nil {
			return ctrl.Result{}, err
		}
		targetPods, err := PodsCreatedBy(ispn.Namespace, r.kubernetes, ctx, upgradeStatus.TargetStatefulSetName)
		if err != nil {
			return ctrl.Result{}, err
		}

		sourcePodOutdated := len(sourcePods.Items) > 0 && isImageOutdated(sourcePods)
		targetPodOutdated := len(targetPods.Items) > 0 && isImageOutdated(targetPods)

		if sourcePodOutdated && targetPodOutdated {
			return reconcile.Result{}, errors.New("on-going rolling upgrade cannot proceed or rollback: both source and target statefulSet differ from operator image version")
		}

		// If the source pods match the operation image version, schedule a rollback since there is an ongoing migration
		if !sourcePodOutdated {
			rollbackNeeded = true
		}
	}

	curl, err := NewCurlClient(ctx, podList.Items[0].Name, ispn, r.kubernetes)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to create curl client: %w", err)
	}

	req := HotRodRollingUpgradeRequest{
		HotRodRollingUpgradeReconciler: r,
		ctx:                            ctx,
		infinispan:                     ispn,
		reqLogger:                      reqLogger,
		curl:                           curl,
		currentStatefulSet:             podList.Items[0].Labels[StatefulSetPodLabel],
	}

	if rollbackNeeded {
		return req.rollback()
	}

	if upgradeStatus == nil {
		// Store statefulSet names and start rolling upgrade process
		return req.updateInfinispan(func(ispn *ispnv1.Infinispan) {
			status := ispn.Status.HotRodRollingUpgradeStatus
			status.Stage = ispnv1.HotRodRollingStageStart
			status.SourceStatefulSetName = ispn.GetStatefulSetName()
			status.TargetStatefulSetName = getOrCreateTargetStatefulSetName(ispn)
		})
	}
	return req.handleMigration()
}

func (r *HotRodRollingUpgradeRequest) handleMigration() (ctrl.Result, error) {
	status := r.infinispan.Status.HotRodRollingUpgradeStatus
	switch status.Stage {
	case ispnv1.HotRodRollingStageStart:
		r.reqLogger.Info(string(ispnv1.HotRodRollingStageStart))
		return r.createNewStatefulSet()
	case ispnv1.HotRodRollingStagePrepare:
		r.reqLogger.Info(string(ispnv1.HotRodRollingStagePrepare))
		return r.prepare()
	case ispnv1.HotRodRollingStageRedirect:
		r.reqLogger.Info(string(ispnv1.HotRodRollingStageRedirect))
		return r.redirectService()
	case ispnv1.HotRodRollingStageSync:
		r.reqLogger.Info(string(ispnv1.HotRodRollingStageSync))
		return r.syncData()
	case ispnv1.HotRodRollingStageStatefulSetReplace:
		r.reqLogger.Info(string(ispnv1.HotRodRollingStageStatefulSetReplace))
		return r.replaceStatefulSet()
	case ispnv1.HotRodRollingStageCleanup:
		r.reqLogger.Info(string(ispnv1.HotRodRollingStageCleanup))
		return r.cleanup(getSourceStatefulSetName(r.infinispan), false)
	default:
		return reconcile.Result{}, nil
	}
}

// getNewStatefulSetName Obtain the target statefulSet name from the status or generate a new name
func getOrCreateTargetStatefulSetName(ispn *ispnv1.Infinispan) string {
	status := ispn.Status.HotRodRollingUpgradeStatus
	if status == nil || status.TargetStatefulSetName == "" {
		return generateNewStatefulSetName(ispn)
	}
	return status.TargetStatefulSetName
}

// getSourceStatefulSetName Obtain the source statefulSet name from the status or the current used one
func getSourceStatefulSetName(ispn *ispnv1.Infinispan) string {
	status := ispn.Status.HotRodRollingUpgradeStatus
	if status == nil || status.SourceStatefulSetName == "" {
		return ispn.GetStatefulSetName()
	}
	return status.SourceStatefulSetName
}

// createNewStatefulSet Creates a new statefulSet to migrate the existing cluster to
func (r *HotRodRollingUpgradeRequest) createNewStatefulSet() (ctrl.Result, error) {
	// Reconcile configmap with new DNS query
	configMap, result, err := r.reconcileNewConfigMap()
	if err != nil {
		return result, err
	}

	// Reconcile ping service with new DNS query
	if err := r.reconcileNewPingService(); err != nil {
		return reconcile.Result{}, err
	}

	// Obtain existing statefulSet
	namespace := r.infinispan.Namespace
	statefulSetName := r.infinispan.GetStatefulSetName()
	sourceStatefulSet := &appsv1.StatefulSet{}
	if result, err := kube.LookupResource(statefulSetName, namespace, sourceStatefulSet, r.infinispan, r.Client, r.reqLogger, r.eventRec, r.ctx); result != nil {
		return *result, fmt.Errorf("error obtaining the existing statefulSet '%s': %w", statefulSetName, err)
	}

	// Redirect the admin service to the current statefulSet
	result, err = r.redirectServiceToStatefulSet(r.infinispan.GetAdminServiceName(), r.currentStatefulSet)
	if err != nil {
		return result, err
	}
	// Redirect the ping service to the current statefulSet
	result, err = r.redirectServiceToStatefulSet(r.infinispan.GetPingServiceName(), r.currentStatefulSet)
	if err != nil {
		return result, err
	}
	// Redirect the user service to the current statefulSet
	result, err = r.redirectServiceToStatefulSet(r.infinispan.GetServiceName(), r.currentStatefulSet)
	if err != nil {
		return result, err
	}
	// Redirect the nodePort service to the current statefulSet
	if r.infinispan.IsExposed() {
		result, err = r.redirectServiceToStatefulSet(r.infinispan.GetServiceExternalName(), r.currentStatefulSet)
		if err != nil {
			return result, err
		}
	}

	// Create a new statefulSet based on the existing one
	targetStatefulSet := &appsv1.StatefulSet{}
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.infinispan)
	err = r.Get(r.ctx, types.NamespacedName{Namespace: namespace, Name: targetStatefulSetName}, targetStatefulSet)

	if err != nil {
		if !kerrors.IsNotFound(err) {
			return result, err
		}
		targetStatefulSet := sourceStatefulSet.DeepCopy()
		volumes := targetStatefulSet.Spec.Template.Spec.Volumes
		// Change the config name
		for _, volume := range volumes {
			if volume.Name == ConfigVolumeName {
				volume.ConfigMap.Name = fmt.Sprintf("%v-configuration", targetStatefulSetName)
			}
		}
		targetStatefulSet.Name = targetStatefulSetName
		targetStatefulSet.ObjectMeta.ResourceVersion = ""
		targetStatefulSet.Spec.Template.ObjectMeta.Labels[StatefulSetPodLabel] = targetStatefulSetName
		targetStatefulSet.Spec.Selector.MatchLabels[StatefulSetPodLabel] = targetStatefulSetName
		container := GetContainer(InfinispanContainer, &targetStatefulSet.Spec.Template.Spec)
		container.Image = DefaultImageName
		for o, envVar := range container.Env {
			if envVar.Name == "DEFAULT_IMAGE" {
				container.Env[o] = corev1.EnvVar{
					Name:      envVar.Name,
					Value:     DefaultImageName,
					ValueFrom: envVar.ValueFrom,
				}
			}
			if envVar.Name == "CONFIG_HASH" {
				container.Env[o] = corev1.EnvVar{
					Name:      envVar.Name,
					Value:     hash.HashString(configMap.Data[ServerConfigFilename]),
					ValueFrom: envVar.ValueFrom,
				}
			}
		}

		err = r.Create(r.ctx, targetStatefulSet)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to create new statefulSet '%s': %w", targetStatefulSetName, err)
		}
		return reconcile.Result{RequeueAfter: DefaultWaitOnCluster}, err
	}
	// If the new statefulSet was already created, check if the pods are ready
	podList, err := PodsCreatedBy(targetStatefulSet.GetNamespace(), r.kubernetes, r.ctx, targetStatefulSetName)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get pods of the new statefulSet '%s': %w", targetStatefulSetName, err)
	}

	for _, pod := range podList.Items {
		if !kube.IsPodReady(pod) {
			r.reqLogger.Info(fmt.Sprintf("Pod '%s' not ready", pod.Name))
			return reconcile.Result{RequeueAfter: DefaultWaitOnCluster}, err
		}
	}

	// Check if cluster is well-formed
	conditions := getInfinispanConditions(podList.Items, r.infinispan, r.curl)
	r.reqLogger.Info(fmt.Sprintf("Cluster conditions: %v", conditions))
	for _, condition := range conditions {
		if condition.Type == ispnv1.ConditionWellFormed {
			if condition.Status != metav1.ConditionTrue {
				r.reqLogger.Info(fmt.Sprintf("Cluster from Statefulset '%s' not well formed", targetStatefulSet.Name))
				return ctrl.Result{RequeueAfter: DefaultWaitClusterNotWellFormed}, nil
			}
		}
	}

	return r.updateStage(ispnv1.HotRodRollingStagePrepare)
}

// prepare Prepares the new statefulSet, creating caches and connecting them to the source cluster
func (r *HotRodRollingUpgradeRequest) prepare() (ctrl.Result, error) {
	ispn := r.infinispan
	targetStatefulSetName := getOrCreateTargetStatefulSetName(ispn)

	// Obtain the podName from the new statefulSet to invoke requests
	targetStatefulSet := &appsv1.StatefulSet{}
	if result, err := kube.LookupResource(targetStatefulSetName, ispn.GetNamespace(), targetStatefulSet, r.infinispan, r.Client, r.reqLogger, r.eventRec, r.ctx); result != nil {
		return *result, fmt.Errorf("error finding statefulSet '%s': %w", targetStatefulSet.Name, err)
	}

	targetPodList, err := PodsCreatedBy(targetStatefulSet.GetNamespace(), r.kubernetes, r.ctx, targetStatefulSetName)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to find the pods from the new statefulSet : %w", err)
	}
	targetPod := targetPodList.Items[0]

	// Obtain the pods of the source cluster so that we can use their IPs to create the remote store config
	sourcePodList, err := PodsCreatedBy(ispn.Namespace, r.kubernetes, r.ctx, getSourceStatefulSetName(ispn))
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to find the pods from the source statefulSet : %w", err)
	}

	pass, err := users.AdminPassword(ispn.GetAdminSecretName(), ispn.Namespace, r.kubernetes, r.ctx)
	if err != nil {
		return reconcile.Result{}, err
	}

	sourceClient := ispnClient.New(r.curl)
	// Clone the source curl client as the credentials are the same, updating the pod to one from the target statefulset
	targetClient := InfinispanForPod(targetPod.Name, r.curl)

	sourceIp := sourcePodList.Items[0].Status.PodIP
	if err = upgrades.ConnectCaches(pass, sourceIp, sourceClient, targetClient, r.reqLogger); err != nil {
		return reconcile.Result{}, err
	}

	// Move to next stage
	return r.updateStage(ispnv1.HotRodRollingStageRedirect)
}

// redirectService Redirects the user service to the new pods to avoid downtime when migrating data
func (r *HotRodRollingUpgradeRequest) redirectService() (ctrl.Result, error) {
	ispn := r.infinispan
	res, err := r.redirectServices(ispn.Status.HotRodRollingUpgradeStatus.TargetStatefulSetName)

	if err != nil {
		return res, err
	}

	// Move to next stage
	return r.updateStage(ispnv1.HotRodRollingStageSync)
}

// syncData Copies data from the source cluster to the destination statefulSet
func (r *HotRodRollingUpgradeRequest) syncData() (ctrl.Result, error) {
	// Obtain the podName from the new statefulSet to invoke requests
	targetStatefulSet := &appsv1.StatefulSet{}
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.infinispan)
	if result, err := kube.LookupResource(targetStatefulSetName, r.infinispan.GetNamespace(), targetStatefulSet, r.infinispan, r.Client, r.reqLogger, r.eventRec, r.ctx); result != nil {
		return *result, err
	}

	podList, err := PodsCreatedBy(targetStatefulSet.GetNamespace(), r.kubernetes, r.ctx, targetStatefulSetName)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to obtain pods from the target cluster: %w", err)
	}

	// Clone the source curl client as the credentials are the same, updating the pod to one from the target statefulset
	targetClient := InfinispanForPod(podList.Items[0].Name, r.curl)
	if err = upgrades.SyncCaches(targetClient, r.reqLogger); err != nil {
		return reconcile.Result{}, err
	}

	// Move to next stage
	return r.updateStage(ispnv1.HotRodRollingStageStatefulSetReplace)
}

// replaceStatefulSet Replaces the statefulSet in the infinispan resource
func (r *HotRodRollingUpgradeRequest) replaceStatefulSet() (ctrl.Result, error) {
	ispn := r.infinispan
	targetStatefulSetName := getOrCreateTargetStatefulSetName(ispn)

	// Move admin to new cluster
	if result, err := r.redirectServiceToStatefulSet(r.infinispan.GetAdminServiceName(), targetStatefulSetName); err != nil {
		return result, err
	}

	// Change statefulSet reference in the Infinispan resource and move to next stage
	_, err := kube.CreateOrPatch(r.ctx, r, ispn, func() error {
		if ispn.CreationTimestamp.IsZero() {
			return kerrors.NewNotFound(schema.ParseGroupResource("infinispan.infinispan.org"), ispn.Name)
		}
		ispn.Status.StatefulSetName = targetStatefulSetName
		ispn.Status.HotRodRollingUpgradeStatus.Stage = ispnv1.HotRodRollingStageCleanup
		return nil
	})

	return reconcile.Result{}, err
}

// cleanup Dispose a statefulSet from an Infinispan CRD and its dependencies
func (r *HotRodRollingUpgradeRequest) cleanup(statefulSetName string, rollback bool) (ctrl.Result, error) {
	ispn := r.infinispan
	pingServiceName := fmt.Sprintf("%s-ping", statefulSetName)
	configName := fmt.Sprintf("%v-configuration", statefulSetName)

	// Wait for cluster with new statefulSet stability
	if !r.infinispan.IsWellFormed() {
		return ctrl.Result{RequeueAfter: DefaultWaitOnCreateResource}, nil
	}

	// Remove statefulSet label from the services
	if result, err := r.removeStatefulSetSelector(r.infinispan.GetAdminServiceName()); err != nil {
		return result, err
	}
	if result, err := r.removeStatefulSetSelector(r.infinispan.GetPingServiceName()); err != nil {
		return result, err
	}
	if result, err := r.removeStatefulSetSelector(r.infinispan.GetServiceName()); err != nil {
		return result, err
	}
	if r.infinispan.IsExposed() {
		if result, err := r.removeStatefulSetSelector(r.infinispan.GetServiceExternalName()); err != nil {
			return result, err
		}
	}

	// Delete configMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: ispn.Namespace,
		},
	}
	err := r.Delete(r.ctx, configMap)
	if err != nil && !kerrors.IsNotFound(err) {
		return reconcile.Result{}, fmt.Errorf("error deleting config map: %w", err)
	}

	// Delete ping service
	pingService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pingServiceName,
			Namespace: ispn.Namespace,
		},
	}
	err = r.Delete(r.ctx, pingService)
	if err != nil && !kerrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("error deleting pingservice '%s': %w", pingServiceName, err)
	}

	// Delete statefulSet
	sourceStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: ispn.Namespace,
		},
	}
	err = r.Delete(r.ctx, sourceStatefulSet)
	if err != nil && !kerrors.IsNotFound(err) {
		return reconcile.Result{}, fmt.Errorf("error deleting statefulSet: %w", err)
	}

	// Remove HotRodRollingUpgradeStatus status from the Infinispan CR
	_, err = kube.CreateOrPatch(r.ctx, r, ispn, func() error {
		rollingUpgradeStatus := ispn.Status.HotRodRollingUpgradeStatus
		if rollback {
			ispn.Status.StatefulSetName = rollingUpgradeStatus.SourceStatefulSetName
		}
		ispn.Status.HotRodRollingUpgradeStatus = nil
		return nil
	})

	if err != nil {
		return reconcile.Result{}, fmt.Errorf("error removing HotRodRollingUpgradeStatus: %w", err)
	}

	r.reqLogger.Info("Hot Rod Rolling Upgrade finished successfully")
	return ctrl.Result{}, nil
}

func (r *HotRodRollingUpgradeRequest) removeStatefulSetSelector(serviceName string) (reconcile.Result, error) {
	return r.changeSelector(serviceName, func(selector map[string]string) {
		delete(selector, StatefulSetPodLabel)
	})
}

// redirectServiceToStatefulSet Changes the selector of a service to pod with the supplied statefulSet label
func (r *HotRodRollingUpgradeRequest) redirectServiceToStatefulSet(serviceName string, statefulSetName string) (reconcile.Result, error) {
	return r.changeSelector(serviceName, func(selector map[string]string) {
		selector[StatefulSetPodLabel] = statefulSetName
	})
}

// changeSelector Applies a transformation to a service selector
func (r *HotRodRollingUpgradeRequest) changeSelector(serviceName string, selectorFunc func(map[string]string)) (reconcile.Result, error) {
	service := &corev1.Service{}
	namespace := r.infinispan.GetNamespace()
	if result, err := kube.LookupResource(serviceName, namespace, service, r.infinispan, r, r.log, r.eventRec, r.ctx); result != nil {
		return *result, err
	}
	_, err := kube.CreateOrPatch(r.ctx, r, service, func() error {
		if r.infinispan.CreationTimestamp.IsZero() {
			return kerrors.NewNotFound(schema.ParseGroupResource("infinispan.infinispan.org"), r.infinispan.Name)
		}
		selector := service.Spec.Selector
		selectorFunc(selector)
		return nil
	})
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to update service: %w", err)
	}
	return reconcile.Result{}, nil
}

// redirectServices Redirects all services from ispn to a certain statefulSet's pods
func (r *HotRodRollingUpgradeRequest) redirectServices(statefulSet string) (ctrl.Result, error) {
	// Redirect the user service to the new pods
	ispn := r.infinispan
	res, err := r.redirectServiceToStatefulSet(ispn.GetServiceName(), statefulSet)
	if err != nil {
		return res, err
	}

	// Redirect NodePort to the new pods
	if ispn.IsExposed() {
		res, err := r.redirectServiceToStatefulSet(ispn.GetServiceExternalName(), statefulSet)
		if err != nil {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *HotRodRollingUpgradeRequest) updateInfinispan(updateFunc func(ispn *ispnv1.Infinispan)) (reconcile.Result, error) {
	_, err := kube.CreateOrPatch(r.ctx, r, r.infinispan, func() error {
		if r.infinispan.CreationTimestamp.IsZero() {
			return kerrors.NewNotFound(schema.ParseGroupResource("infinispan.infinispan.org"), r.infinispan.Name)
		}
		if r.infinispan.Status.HotRodRollingUpgradeStatus == nil {
			r.infinispan.Status.HotRodRollingUpgradeStatus = &ispnv1.HotRodRollingUpgradeStatus{}
		}
		updateFunc(r.infinispan)
		return nil
	})
	return reconcile.Result{}, err
}

// updateStage Updates the status of the Hot Rod Rolling Upgrade process
func (r *HotRodRollingUpgradeRequest) updateStage(stage ispnv1.HotRodRollingUpgradeStage) (reconcile.Result, error) {
	return r.updateInfinispan(func(ispn *ispnv1.Infinispan) {
		r.infinispan.Status.HotRodRollingUpgradeStatus.Stage = stage
	})
}

// generateNewStatefulSetName Derive a name for the new statefulSet.
func generateNewStatefulSetName(ispn *ispnv1.Infinispan) string {
	statefulSetName := ispn.GetStatefulSetName()
	subMatch := nameRegexp.FindStringSubmatch(statefulSetName)

	if len(subMatch) == 0 {
		return statefulSetName + "-1"
	}
	name := subMatch[1]
	revision, _ := strconv.Atoi(subMatch[2])
	revision++
	return name + "-" + strconv.Itoa(revision)
}

// reconcileNewPingService Reconcile a specific ping service for the pods of the new statefulSet
func (r *HotRodRollingUpgradeRequest) reconcileNewPingService() error {
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.infinispan)
	namespace := r.infinispan.GetNamespace()
	pingService := &corev1.Service{}
	newPingServiceName := fmt.Sprintf("%s-ping", targetStatefulSetName)
	err := r.Get(r.ctx, types.NamespacedName{Namespace: namespace, Name: newPingServiceName}, pingService)
	if err == nil || !kerrors.IsNotFound(err) {
		return err
	}
	// Obtains the existing ping service
	err = r.Get(r.ctx, types.NamespacedName{Namespace: namespace, Name: r.infinispan.GetPingServiceName()}, pingService)
	if err != nil {
		return fmt.Errorf("could not find ping service '%s': %w", r.infinispan.GetPingServiceName(), err)
	}
	// Clone existing service changing the DNS Query
	newPingService := pingService.DeepCopy()
	newPingService.ObjectMeta.ResourceVersion = ""
	newPingService.Name = newPingServiceName
	newPingService.Spec.Selector[StatefulSetPodLabel] = targetStatefulSetName
	err = r.Create(r.ctx, newPingService)
	if err != nil {
		return fmt.Errorf("error creating new ping service '%s' : %w", newPingServiceName, err)
	}
	return nil
}

// reconcileNewConfigMap Creates a new configMap based on the existing (but with different ping discovery) for the new statefulSet
func (r *HotRodRollingUpgradeRequest) reconcileNewConfigMap() (*corev1.ConfigMap, reconcile.Result, error) {
	namespace := r.infinispan.GetNamespace()
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.infinispan)
	newConfigName := fmt.Sprintf("%v-configuration", targetStatefulSetName)

	// Check if it was already created
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newConfigName,
			Namespace: r.infinispan.Namespace,
		},
	}
	err := r.Get(r.ctx, types.NamespacedName{Namespace: namespace, Name: newConfigName}, configMap)
	if err == nil || !kerrors.IsNotFound(err) {
		return configMap, reconcile.Result{}, err
	}

	serverConfig, result, err := GenerateServerConfig(targetStatefulSetName, r.infinispan, r.kubernetes, r.Client, r.log, r.eventRec, r.ctx)
	if result != nil {
		return nil, *result, err
	}

	loggingConfig := &logging.Spec{
		Categories: r.infinispan.GetLogCategoriesForConfig(),
	}

	// TODO utilise the version of the server being upgraded to once server/operator versions decoupled
	log4jXml, err := logging.Generate(nil, loggingConfig)
	if err != nil {
		return nil, reconcile.Result{}, err
	}
	InitServerConfigMap(configMap, r.infinispan, serverConfig, log4jXml)

	return configMap, reconcile.Result{}, r.Create(r.ctx, configMap)
}

// rollback Undo changes of a partial upgrade
func (r *HotRodRollingUpgradeRequest) rollback() (reconcile.Result, error) {
	status := r.infinispan.Status.HotRodRollingUpgradeStatus
	sourceStatefulSetName := status.SourceStatefulSetName

	// Redirect services back to source cluster
	services, err := r.redirectServices(sourceStatefulSetName)
	if err != nil {
		return services, err
	}

	// Rollback is needed when state is cleanup, since the statefulSet in the CRD was replaced
	return r.cleanup(status.TargetStatefulSetName, status.Stage == ispnv1.HotRodRollingStageCleanup)
}
