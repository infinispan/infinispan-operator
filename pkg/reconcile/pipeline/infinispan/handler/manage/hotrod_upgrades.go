package manage

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	. "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	config "github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/server"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/upgrades"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type HotRodRollingUpgradeRequest struct {
	ctx                pipeline.Context
	i                  *ispnv1.Infinispan
	log                logr.Logger
	currentStatefulSet string
}

var nameRegexp = regexp.MustCompile(`(.*)-([\d]+$)`)

// HotRodRollingUpgrade handles all stages of a Hot Rod Rolling upgrade. Throughout the execution we use RequeueEventually
// so that the Infinispan CR pipeline execution is not stopped by any rolling upgrade failures. This ensures the source
// cluster will continue to be reconciled as normal throughout the upgrade process.
func HotRodRollingUpgrade(i *ispnv1.Infinispan, ctx pipeline.Context) {
	log := ctx.Log().WithName("HotRodRollingUpgrade")

	podList, err := ctx.InfinispanPods()
	if err != nil || len(podList.Items) == 0 {
		log.Info("No pods found")
		return
	}

	rollbackNeeded := false
	upgradeStatus := i.Status.HotRodRollingUpgradeStatus

	if upgradeStatus == nil && !isImageOutdated(podList) {
		log.Info("Pods already running correct version, skipping upgrade")
		return
	}

	req := HotRodRollingUpgradeRequest{
		ctx:                ctx,
		i:                  i,
		log:                log,
		currentStatefulSet: podList.Items[0].Labels[StatefulSetPodLabel],
	}

	if upgradeStatus != nil {
		sourcePods, err := req.PodsCreatedBy(upgradeStatus.SourceStatefulSetName)
		if err != nil {
			return
		}
		targetPods, err := req.PodsCreatedBy(upgradeStatus.TargetStatefulSetName)
		if err != nil {
			return
		}

		sourcePodOutdated := len(sourcePods.Items) > 0 && isImageOutdated(sourcePods)
		targetPodOutdated := len(targetPods.Items) > 0 && isImageOutdated(targetPods)

		if sourcePodOutdated && targetPodOutdated {
			ctx.Requeue(errors.New("on-going rolling upgrade cannot proceed or rollback: both source and target statefulSet differ from operator image version"))
			return
		}

		// If the source pods match the operation image version, schedule a rollback since there is an ongoing migration
		if !sourcePodOutdated {
			rollbackNeeded = true
		}
	}

	if rollbackNeeded {
		if err := req.rollback(); err != nil {
			log.Error(err, "error encountered on rollback")
		}
		ctx.RequeueEventually(0)
		return
	}

	if upgradeStatus == nil {
		err := ctx.UpdateInfinispan(func() {
			i.Status.HotRodRollingUpgradeStatus = &ispnv1.HotRodRollingUpgradeStatus{
				Stage:                 ispnv1.HotRodRollingStageStart,
				SourceStatefulSetName: i.GetStatefulSetName(),
				TargetStatefulSetName: getOrCreateTargetStatefulSetName(i),
			}
		})
		if err != nil {
			log.Error(err, "unable to create initial status")
		}
		ctx.RequeueEventually(0)
		return
	}
	if err := req.handleMigration(); err != nil {
		ctx.Log().Error(err, fmt.Sprintf("%s failed, retrying", upgradeStatus.Stage))
		ctx.RequeueEventually(0)
	}
}

func (r *HotRodRollingUpgradeRequest) handleMigration() error {
	status := r.i.Status.HotRodRollingUpgradeStatus
	switch status.Stage {
	case ispnv1.HotRodRollingStageStart:
		r.log.Info(string(ispnv1.HotRodRollingStageStart))
		return r.createNewStatefulSet()
	case ispnv1.HotRodRollingStagePrepare:
		r.log.Info(string(ispnv1.HotRodRollingStagePrepare))
		return r.prepare()
	case ispnv1.HotRodRollingStageRedirect:
		r.log.Info(string(ispnv1.HotRodRollingStageRedirect))
		return r.redirectService()
	case ispnv1.HotRodRollingStageSync:
		r.log.Info(string(ispnv1.HotRodRollingStageSync))
		return r.syncData()
	case ispnv1.HotRodRollingStageStatefulSetReplace:
		r.log.Info(string(ispnv1.HotRodRollingStageStatefulSetReplace))
		return r.replaceStatefulSet()
	case ispnv1.HotRodRollingStageCleanup:
		r.log.Info(string(ispnv1.HotRodRollingStageCleanup))
		return r.cleanup(getSourceStatefulSetName(r.i), false)
	default:
		return nil
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

func (r *HotRodRollingUpgradeRequest) removeStatefulSetSelector(serviceName string) error {
	return r.changeSelector(serviceName, func(selector map[string]string) {
		delete(selector, StatefulSetPodLabel)
	})
}

// redirectServiceToStatefulSet Changes the selector of a service to pod with the supplied statefulSet label
func (r *HotRodRollingUpgradeRequest) redirectServiceToStatefulSet(serviceName string, statefulSetName string) error {
	return r.changeSelector(serviceName, func(selector map[string]string) {
		selector[StatefulSetPodLabel] = statefulSetName
	})
}

// changeSelector Applies a transformation to a service selector
func (r *HotRodRollingUpgradeRequest) changeSelector(serviceName string, selectorFunc func(map[string]string)) error {
	resources := r.ctx.Resources()
	service := &corev1.Service{}
	if err := resources.Load(serviceName, service); err != nil {
		return err
	}

	_, err := resources.CreateOrPatch(service, false, func() error {
		if r.i.CreationTimestamp.IsZero() {
			return kerrors.NewNotFound(schema.ParseGroupResource("i.i.org"), r.i.Name)
		}
		selector := service.Spec.Selector
		selectorFunc(selector)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update service: %w", err)
	}
	return nil
}

// redirectServices Redirects all services from ispn to a certain statefulSet's pods
func (r *HotRodRollingUpgradeRequest) redirectServices(statefulSet string) error {
	// Redirect the user service to the new pods
	ispn := r.i
	if err := r.redirectServiceToStatefulSet(ispn.GetServiceName(), statefulSet); err != nil {
		return err
	}

	// Redirect NodePort to the new pods
	if ispn.IsExposed() {
		if err := r.redirectServiceToStatefulSet(ispn.GetServiceExternalName(), statefulSet); err != nil {
			return err
		}
	}
	return nil
}

// updateStage Updates the status of the Hot Rod Rolling Upgrade process
func (r *HotRodRollingUpgradeRequest) updateStage(stage ispnv1.HotRodRollingUpgradeStage) error {
	err := r.ctx.UpdateInfinispan(func() {
		r.i.Status.HotRodRollingUpgradeStatus.Stage = stage
	})
	return err
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

// reconcileNewConfigMap Creates a new configMap based on the existing (but with different ping discovery) for the new statefulSet
// returns the hash of the server config
func (r *HotRodRollingUpgradeRequest) reconcileNewConfigMap() (string, error) {
	ctx := r.ctx
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.i)
	newConfigName := fmt.Sprintf("%v-configuration", targetStatefulSetName)

	if err := ctx.Resources().Load(newConfigName, &corev1.ConfigMap{}); err == nil {
		// ConfigMap already exists, do nothing
		return "", nil
	}

	// Modifying the existing ConfigSpec to use the targetStatefulSetName
	configFiles := ctx.ConfigFiles()
	configSpec := configFiles.ConfigSpec
	configSpec.StatefulSetName = targetStatefulSetName

	// TODO utilise a version specific configurator once server/operator versions decoupled
	var serverConfig, zeroConfig string
	var err error
	if serverConfig, err = config.Generate(nil, &configSpec); err != nil {
		return "", fmt.Errorf("unable to generate new infinispan.xml for Rolling Upgrade: %w", err)
	}

	if zeroConfig, err = config.Generate(nil, &configSpec); err != nil {
		return "", fmt.Errorf("unable to generate new infinispan-zero.xml for Rolling Upgrade: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newConfigName,
			Namespace: r.i.Namespace,
		},
	}
	provision.PopulateServerConfigMap(serverConfig, zeroConfig, configFiles.Log4j, cm)
	return hash.HashString(serverConfig), r.ctx.Resources().Create(cm, true)
}

// reconcileNewPingService Reconcile a specific ping service for the pods of the new statefulSet
func (r *HotRodRollingUpgradeRequest) reconcileNewPingService() error {
	resources := r.ctx.Resources()
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.i)
	pingService := &corev1.Service{}
	newPingServiceName := fmt.Sprintf("%s-ping", targetStatefulSetName)
	if err := resources.Load(newPingServiceName, pingService); err == nil || !kerrors.IsNotFound(err) {
		return err
	}
	// Obtains the existing ping service
	if err := resources.Load(r.i.GetPingServiceName(), pingService); err != nil {
		return fmt.Errorf("could not find ping service '%s': %w", r.i.GetPingServiceName(), err)
	}
	// Clone existing service changing the DNS Query
	newPingService := pingService.DeepCopy()
	newPingService.ObjectMeta.ResourceVersion = ""
	newPingService.Name = newPingServiceName
	newPingService.Spec.Selector[StatefulSetPodLabel] = targetStatefulSetName
	if err := resources.Create(newPingService, true); err != nil {
		return fmt.Errorf("error creating new ping service '%s' : %w", newPingServiceName, err)
	}
	return nil
}

// createNewStatefulSet Creates a new statefulSet to migrate the existing cluster to
func (r *HotRodRollingUpgradeRequest) createNewStatefulSet() error {
	ctx := r.ctx
	log := r.log

	// Reconcile ping service with new DNS query
	if err := r.reconcileNewPingService(); err != nil {
		return err
	}

	// Obtain existing statefulSet
	statefulSetName := r.i.GetStatefulSetName()
	sourceStatefulSet := &appsv1.StatefulSet{}
	if err := ctx.Resources().Load(statefulSetName, sourceStatefulSet); err != nil {
		return fmt.Errorf("error obtaining the existing statefulSet '%s': %w", statefulSetName, err)
	}

	// Redirect the admin service to the current statefulSet
	if err := r.redirectServiceToStatefulSet(r.i.GetAdminServiceName(), r.currentStatefulSet); err != nil {
		return err
	}
	// Redirect the ping service to the current statefulSet
	if err := r.redirectServiceToStatefulSet(r.i.GetPingServiceName(), r.currentStatefulSet); err != nil {
		return err
	}
	// Redirect the user service to the current statefulSet
	if err := r.redirectServiceToStatefulSet(r.i.GetServiceName(), r.currentStatefulSet); err != nil {
		return err
	}
	// Redirect the nodePort service to the current statefulSet
	if r.i.IsExposed() {
		if err := r.redirectServiceToStatefulSet(r.i.GetServiceExternalName(), r.currentStatefulSet); err != nil {
			return err
		}
	}

	// Create a new statefulSet based on the existing one
	targetStatefulSet := &appsv1.StatefulSet{}
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.i)
	if err := ctx.Resources().Load(targetStatefulSetName, targetStatefulSet); err != nil {
		if !kerrors.IsNotFound(err) {
			return err
		}

		var configHash string
		if configHash, err = r.reconcileNewConfigMap(); err != nil {
			return err
		}

		targetStatefulSet := sourceStatefulSet.DeepCopy()
		volumes := targetStatefulSet.Spec.Template.Spec.Volumes
		// Change the config name
		for _, volume := range volumes {
			if volume.Name == provision.ConfigVolumeName {
				volume.ConfigMap.Name = fmt.Sprintf("%v-configuration", targetStatefulSetName)
			}
		}
		targetStatefulSet.Name = targetStatefulSetName
		targetStatefulSet.ObjectMeta.ResourceVersion = ""
		targetStatefulSet.Spec.Template.ObjectMeta.Labels[StatefulSetPodLabel] = targetStatefulSetName
		targetStatefulSet.Spec.Selector.MatchLabels[StatefulSetPodLabel] = targetStatefulSetName
		container := kube.GetContainer(provision.InfinispanContainer, &targetStatefulSet.Spec.Template.Spec)
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
					Value:     hash.HashString(configHash),
					ValueFrom: envVar.ValueFrom,
				}
			}
		}

		if err = ctx.Resources().Create(targetStatefulSet, true); err != nil {
			return fmt.Errorf("failed to create new statefulSet '%s': %w", targetStatefulSetName, err)
		}
		r.ctx.RequeueEventually(DefaultWaitOnCluster)
		return nil
	}
	// If the new statefulSet was already created, check if the pods are ready
	podList, err := r.PodsCreatedBy(targetStatefulSetName)
	if err != nil {
		return fmt.Errorf("failed to get pods of the new statefulSet '%s': %w", targetStatefulSetName, err)
	}

	for _, pod := range podList.Items {
		if !kube.IsPodReady(pod) {
			log.Info(fmt.Sprintf("Pod '%s' not ready", pod.Name))
			r.ctx.RequeueEventually(DefaultWaitOnCluster)
			return nil
		}
	}

	// Check if cluster is well-formed
	wellFormed := wellFormedCondition(r.i, r.ctx, podList)
	log.Info(fmt.Sprintf("Cluster WellFormed: %v", wellFormed))
	if wellFormed.Status != metav1.ConditionTrue {
		log.Info(fmt.Sprintf("Cluster from Statefulset '%s' not well formed", targetStatefulSet.Name))
		ctx.RequeueEventually(DefaultWaitClusterNotWellFormed)
		return nil
	}
	return r.updateStage(ispnv1.HotRodRollingStagePrepare)
}

// prepare Prepares the new statefulSet, creating caches and connecting them to the source cluster
func (r *HotRodRollingUpgradeRequest) prepare() error {
	ctx := r.ctx
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.i)

	// Obtain the podName from the new statefulSet to invoke requests
	targetStatefulSet := &appsv1.StatefulSet{}
	if err := ctx.Resources().Load(targetStatefulSetName, targetStatefulSet); err != nil {
		return fmt.Errorf("error obtaining the existing statefulSet '%s': %w", targetStatefulSetName, err)
	}

	targetPodList, err := r.PodsCreatedBy(targetStatefulSetName)
	if err != nil {
		return fmt.Errorf("failed to find the pods from the new statefulSet : %w", err)
	}
	targetPod := targetPodList.Items[0]

	// Obtain the pods of the source cluster so that we can use their IPs to create the remote store config
	sourcePodList, err := r.PodsCreatedBy(getSourceStatefulSetName(r.i))
	if err != nil {
		return fmt.Errorf("failed to find the pods from the source statefulSet : %w", err)
	}

	sourceClient, err := ctx.InfinispanClient()
	if err != nil {
		return err
	}
	// Clone the source curl client as the credentials are the same, updating the pod to one from the target statefulset
	targetClient := ctx.InfinispanClientForPod(targetPod.Name)

	sourceIp := sourcePodList.Items[0].Status.PodIP
	pass := ctx.ConfigFiles().AdminIdentities.Password
	if err = upgrades.ConnectCaches(pass, sourceIp, sourceClient, targetClient, r.log); err != nil {
		return err
	}

	// Move to next stage
	return r.updateStage(ispnv1.HotRodRollingStageRedirect)
}

// redirectService Redirects the user service to the new pods to avoid downtime when migrating data
func (r *HotRodRollingUpgradeRequest) redirectService() error {
	if err := r.redirectServices(r.i.Status.HotRodRollingUpgradeStatus.TargetStatefulSetName); err != nil {
		return err
	}

	// Move to next stage
	return r.updateStage(ispnv1.HotRodRollingStageSync)
}

// syncData Copies data from the source cluster to the destination statefulSet
func (r *HotRodRollingUpgradeRequest) syncData() error {
	ctx := r.ctx
	// Obtain the podName from the new statefulSet to invoke requests
	targetStatefulSet := &appsv1.StatefulSet{}
	targetStatefulSetName := getOrCreateTargetStatefulSetName(r.i)
	if err := ctx.Resources().Load(targetStatefulSetName, targetStatefulSet); err != nil {
		return fmt.Errorf("error obtaining the existing statefulSet '%s': %w", targetStatefulSetName, err)
	}

	podList, err := r.PodsCreatedBy(targetStatefulSetName)
	if err != nil {
		return fmt.Errorf("failed to obtain pods from the target cluster: %w", err)
	}

	// Clone the source curl client as the credentials are the same, updating the pod to one from the target statefulset
	targetClient := ctx.InfinispanClientForPod(podList.Items[0].Name)
	if err = upgrades.SyncCaches(targetClient, r.log); err != nil {
		return err
	}

	// Move to next stage
	return r.updateStage(ispnv1.HotRodRollingStageStatefulSetReplace)
}

// replaceStatefulSet Replaces the statefulSet in the i resource
func (r *HotRodRollingUpgradeRequest) replaceStatefulSet() error {
	ispn := r.i
	targetStatefulSetName := getOrCreateTargetStatefulSetName(ispn)

	// Move admin to new cluster
	if err := r.redirectServiceToStatefulSet(r.i.GetAdminServiceName(), targetStatefulSetName); err != nil {
		return err
	}

	// Change statefulSet reference in the Infinispan resource and move to next stage
	return r.ctx.UpdateInfinispan(func() {
		ispn.Status.StatefulSetName = targetStatefulSetName
		ispn.Status.HotRodRollingUpgradeStatus.Stage = ispnv1.HotRodRollingStageCleanup
	})
}

// cleanup Dispose a statefulSet from an Infinispan CRD and its dependencies
func (r *HotRodRollingUpgradeRequest) cleanup(statefulSetName string, rollback bool) error {
	ctx := r.ctx
	pingServiceName := fmt.Sprintf("%s-ping", statefulSetName)
	configName := fmt.Sprintf("%v-configuration", statefulSetName)

	// Wait for cluster with new statefulSet stability
	if !r.i.IsWellFormed() {
		ctx.RequeueEventually(DefaultWaitOnCreateResource)
		return nil
	}

	// Remove statefulSet label from the services
	if err := r.removeStatefulSetSelector(r.i.GetAdminServiceName()); err != nil {
		return err
	}
	if err := r.removeStatefulSetSelector(r.i.GetPingServiceName()); err != nil {
		return err
	}
	if err := r.removeStatefulSetSelector(r.i.GetServiceName()); err != nil {
		return err
	}
	if r.i.IsExposed() {
		if err := r.removeStatefulSetSelector(r.i.GetServiceExternalName()); err != nil {
			return err
		}
	}

	type resource struct {
		name string
		obj  client.Object
	}

	resources := []resource{
		{configName, &corev1.ConfigMap{}},
		{pingServiceName, &corev1.Service{}},
		{statefulSetName, &appsv1.StatefulSet{}},
	}

	del := func(name string, obj client.Object) error {
		if err := ctx.Resources().Delete(name, obj, pipeline.IgnoreNotFound); err != nil {
			return err
		}
		return nil
	}

	for _, r := range resources {
		if err := del(r.name, r.obj); err != nil {
			return err
		}
	}
	// Remove HotRodRollingUpgradeStatus status from the Infinispan CR
	err := ctx.UpdateInfinispan(func() {
		ispn := r.i
		rollingUpgradeStatus := ispn.Status.HotRodRollingUpgradeStatus
		if rollback {
			ispn.Status.StatefulSetName = rollingUpgradeStatus.SourceStatefulSetName
		}
		ispn.Status.HotRodRollingUpgradeStatus = nil
	})

	if err != nil {
		return fmt.Errorf("error removing HotRodRollingUpgradeStatus: %w", err)
	}

	r.log.Info("Hot Rod Rolling Upgrade finished successfully")
	return nil
}

// rollback Undo changes of a partial upgrade
func (r *HotRodRollingUpgradeRequest) rollback() error {
	status := r.i.Status.HotRodRollingUpgradeStatus
	sourceStatefulSetName := status.SourceStatefulSetName

	// Redirect services back to source cluster
	if err := r.redirectServices(sourceStatefulSetName); err != nil {
		return err
	}

	// Rollback is needed when state is cleanup, since the statefulSet in the CRD was replaced
	return r.cleanup(status.TargetStatefulSetName, status.Stage == ispnv1.HotRodRollingStageCleanup)
}

func (r *HotRodRollingUpgradeRequest) PodsCreatedBy(statefulSetName string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	labels := map[string]string{StatefulSetPodLabel: statefulSetName}
	if err := r.ctx.Resources().List(labels, podList, pipeline.RetryOnErr); err != nil {
		return nil, err
	}
	return podList, nil
}

func isImageOutdated(podList *corev1.PodList) bool {
	podDefaultImage := kube.GetPodDefaultImage(*kube.GetContainer(provision.InfinispanContainer, &podList.Items[0].Spec))
	return podDefaultImage != DefaultImageName
}
