package manage

import (
	"errors"
	"fmt"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InitialiseOperandVersion sets the spec.Version field for CRs that were created by an older operator version
func InitialiseOperandVersion(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if i.Spec.Version == "" {
		operand := ctx.Operands().Oldest()
		operandRef := operand.Ref()
		ctx.Log().Info("Upgrading from single operand operator. Adding spec.version", "version", operandRef)
		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				// Utilise the oldest Operand as this is the Operand provided by the Operator prior to Multi-Operand support
				i.Spec.Version = operandRef
				i.Status.Operand = OperandStatus(i, ispnv1.OperandPhaseRunning, operand)
			}),
		)
	} else if operand, err := ctx.Operands().WithRef(i.Spec.Version); err != nil {
		// Version is not known to the Operator. State not possible when a CR is created for this Operator version as
		// the webhook should prevent the resource being created/updated. The only way this state can be reached is if
		// the Operator was upgraded from a previous release containing the spec.Version Operand, which is now no longer
		// supported with this Operator release.
		ctx.Stop(fmt.Errorf("unable to continue reconcilliation. Operand spec.version='%s' is not supported by this Operator version", i.Spec.Version))
	} else if operand.Deprecated != i.Status.Operand.Deprecated {
		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				i.Status.Operand.Deprecated = operand.Deprecated
			}),
		)
	}
}

// ScheduleGracefulShutdownUpgrade if an upgrade is not already in progress, pods exist and the current pod image
// is not equal to the most recent Operand image associated with the operator
// Sets ConditionUpgrade=true and spec.Replicas=0 in order to trigger GracefulShutdown
func ScheduleGracefulShutdownUpgrade(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if i.IsUpgradeCondition() {
		return
	}

	if UpgradeRequired(i, ctx) {
		ctx.Log().Info("schedule an Infinispan cluster upgrade", "current version", i.Status.Operand.Version, "desired version", ctx.Operand().Ref())
		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionUpgrade, metav1.ConditionTrue, "")
				i.Spec.Replicas = 0
				i.Status.Operand = OperandStatus(i, ispnv1.OperandPhasePending, ctx.Operand())
			}),
		)
	}
}

func UpgradeRequired(i *ispnv1.Infinispan, ctx pipeline.Context) bool {
	if i.Status.Operand.Version == "" {
		// If the status version is not set, then this means we're upgrading from an older Operator version
		return true
	} else {
		installedOperand, err := ctx.Operands().WithRef(i.Status.Operand.Version)
		requestedOperand := ctx.Operand()

		// If the installed Operand version can't be found, then this means that the Operand was installed using
		// an older Operator version and support for the Operand has been removed. In this case we must trigger an upgrade
		// as we can no longer reconcile the unsupported version.
		var unknown *version.UnknownError
		if errors.As(err, &unknown) {
			return true
		}

		// Don't schedule an upgrade if one is already in progress
		if i.Status.Operand.Phase == ispnv1.OperandPhasePending {
			return false
		}

		// If the Operand is marked as a CVE base-image release and no custom image is installed/requested, then we perform the
		// upgrade as a StatefulSet rolling upgrade as the server components are not changed.
		if requestedOperand.CVE &&
			installedOperand.UpstreamVersion.EQ(*requestedOperand.UpstreamVersion) &&
			i.Spec.Image == nil &&
			!i.Status.Operand.CustomImage {
			return false
		}

		if requestedOperand.EQ(installedOperand) {
			if i.Spec.Image == nil {
				// If the currently installed Operand was a custom image, but spec.Image is now nil, then we need to
				// initiate a new upgrade to ensure that the default image associated with the Operand is installed
				return i.Status.Operand.CustomImage
			}
			// If operand versions match, but the FQN of the image differ, then we must schedule an upgrade so the user
			// can transition to a custom/patched version of the Operand without having to recreate the Infinispan CR
			return *i.Spec.Image != i.Status.Operand.Image
		}
		return true
	}
}

// GracefulShutdown safely scales down the cluster to 0 pods if the user sets .spec.Replicas == 0 or a GracefulShutdown
// upgrade is triggered by the pipeline
// 1. If spec.Replicas == 0 and !Stopping. Shutdown server containers, set Stopping=true,WellFormed=false
// 2. Set status.ReplicasWantedAtRestart = statefulset.spec.replicas and statefulset.spec.replicas = 0
// 3. Once statefulset.currentReplicas == 0, Set GracefulShutdown=true,Stopping=false
// 4. If spec.Relicas > 0 and spec.Replicas == status.ReplicasWantedAtRestart. GracefulShutdown=false, status.ReplicasWantedAtRestart=0
func GracefulShutdown(i *ispnv1.Infinispan, ctx pipeline.Context) {
	logger := ctx.Log()

	statefulSet := &appsv1.StatefulSet{}
	if err := ctx.Resources().Load(i.GetStatefulSetName(), statefulSet, pipeline.RetryOnErr); err != nil {
		return
	}

	// Initiate the GracefulShutdown if it's not already in progress
	if i.Spec.Replicas == 0 {
		logger.Info(".Spec.Replicas==0")
		if *statefulSet.Spec.Replicas != 0 {
			logger.Info("StatefulSet.Spec.Replicas!=0")
			// Only send a GracefulShutdown request to the server if it hasn't succeeded already
			if !i.IsConditionTrue(ispnv1.ConditionStopping) {
				logger.Info("Sending GracefulShutdown request to the Infinispan cluster")

				podList, err := ctx.InfinispanPods()
				if err != nil {
					return
				}

				var rebalanceDisabled bool
				for _, pod := range podList.Items {
					ispnClient, err := ctx.InfinispanClientUnknownVersion(pod.Name)
					if err != nil {
						if shutdown, state := containerAlreadyShutdown(err); shutdown {
							logger.Info("Skipping pod whose cache-container has already been shutdown by the Operator", "pod", pod.Name, "state", state)
							continue
						}
						ctx.Requeue(fmt.Errorf("unable to create Infinispan client for cluster being upgraded: %w", err))
						return
					}

					// Disabling rebalancing is a cluster-wide operation so we only need to perform this on a single pod
					// However, multiple calls to this endpoint should be safe, so it's ok if a subsequent reconciliation
					// executes this again
					if !rebalanceDisabled {
						if err := ispnClient.Container().RebalanceDisable(); err != nil {
							ctx.Requeue(fmt.Errorf("unable to disable rebalancing: %w", err))
							return
						}
						rebalanceDisabled = true
					}

					if kube.IsPodReady(pod) {
						if err := ispnClient.Container().Shutdown(); err != nil {
							ctx.Requeue(fmt.Errorf("error encountered on container shutdown: %w", err))
							return
						} else {
							logger.Info("Executed Container Shutdown on pod: ", "Pod.Name", pod.Name)
						}
					}
				}

				logger.Info("GracefulShutdown successfully executed on the Infinispan cluster")
				ctx.Requeue(
					ctx.UpdateInfinispan(func() {
						i.SetCondition(ispnv1.ConditionStopping, metav1.ConditionTrue, "")
						i.SetCondition(ispnv1.ConditionWellFormed, metav1.ConditionFalse, "")
					}),
				)
				return
			}

			updateStatus := func() {
				i.Status.ReplicasWantedAtRestart = *statefulSet.Spec.Replicas
			}
			if err := ctx.UpdateInfinispan(updateStatus); err != nil {
				ctx.Requeue(err)
			} else {
				statefulSet.Spec.Replicas = pointer.Int32Ptr(0)
				// GracefulShutdown in progress, but we must wait until the StatefulSet has scaled down before proceeding
				ctx.Requeue(ctx.Resources().Update(statefulSet))
			}
			return
		}
		// GracefulShutdown complete, proceed with the upgrade
		if statefulSet.Status.CurrentReplicas == 0 {

			if err := provision.ScaleConfigListener(0, i, ctx); err != nil && !apierrors.IsNotFound(err) {
				ctx.Requeue(err)
				return
			}

			ctx.Requeue(
				ctx.UpdateInfinispan(func() {
					i.SetCondition(ispnv1.ConditionGracefulShutdown, metav1.ConditionTrue, "")
					i.SetCondition(ispnv1.ConditionStopping, metav1.ConditionFalse, "")
				}),
			)
		} else {
			ctx.Requeue(nil)
		}
		return
	}

	if i.Spec.Replicas != 0 && i.IsConditionTrue(ispnv1.ConditionGracefulShutdown) {
		logger.Info("Resuming from graceful shutdown")
		if i.Status.ReplicasWantedAtRestart != 0 && i.Spec.Replicas != i.Status.ReplicasWantedAtRestart {
			ctx.Requeue(fmt.Errorf("Spec.Replicas(%d) must be 0 or equal to Status.ReplicasWantedAtRestart(%d)", i.Spec.Replicas, i.Status.ReplicasWantedAtRestart))
			return
		}

		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				i.Status.Replicas = &i.Spec.Replicas
				i.Status.ReplicasWantedAtRestart = 0
				i.SetCondition(ispnv1.ConditionGracefulShutdown, metav1.ConditionFalse, "")
				// Add ConditionScalingUp so that on requeue we update the ClusterService so that no pods will be selected
				i.SetCondition(ispnv1.ConditionScalingUp, metav1.ConditionTrue, "")
			}),
		)
	}
}

func containerAlreadyShutdown(err error) (bool, string) {
	if strings.Contains(err.Error(), "Server is STOPPING") {
		return true, "STOPPING"
	} else if strings.Contains(err.Error(), "Server is TERMINATED") {
		return true, "TERMINATED"
	}
	return false, ""
}

// GracefulShutdownUpgrade performs the steps required by GracefulShutdown upgrades once the cluster has been scaled down
// to 0 replicas
func GracefulShutdownUpgrade(i *ispnv1.Infinispan, ctx pipeline.Context) {
	logger := ctx.Log()

	if i.IsUpgradeCondition() && !i.IsConditionTrue(ispnv1.ConditionStopping) && i.Status.ReplicasWantedAtRestart > 0 {
		logger.Info("GracefulShutdown complete, removing existing Infinispan resources")
		destroyResources(i, ctx)
		logger.Info("Infinispan resources removed", "replicasWantedAtRestart", i.Status.ReplicasWantedAtRestart)

		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				// Ensure that CR has all the operator required labels
				i.ApplyOperatorMeta(ctx.DefaultLabels(), ctx.DefaultAnnotations())
				// Persist replicas needed at restart
				i.Spec.Replicas = i.Status.ReplicasWantedAtRestart
				i.SetCondition(ispnv1.ConditionUpgrade, metav1.ConditionFalse, "")
			}))
	}
}

func EnableRebalanceAfterScaleUp(i *ispnv1.Infinispan, ctx pipeline.Context) {
	// Perform actions on scale up after GracefulShutdown is complete
	if i.Spec.Replicas > 0 && i.IsConditionTrue(ispnv1.ConditionScalingUp) {

		statefulSet := &appsv1.StatefulSet{}
		if err := ctx.Resources().Load(i.GetStatefulSetName(), statefulSet, pipeline.RetryOnErr); err != nil {
			return
		}

		if statefulSet.Status.CurrentReplicas != i.Spec.Replicas {
			// The StatefulSet spec has already been updated to i.spec.replicas, so we must wait for the pods to be provisioned
			ctx.RequeueAfter(consts.DefaultWaitClusterPodsNotReady, nil)
			return
		}

		ispnClient, err := ctx.InfinispanClient()
		if err != nil {
			ctx.Requeue(err)
			return
		}

		if members, err := ispnClient.Container().Members(); err != nil {
			ctx.Requeue(fmt.Errorf("unable to retrieve cluster members on scale up: %w", err))
			return
		} else if len(members) != int(i.Spec.Replicas) {
			ctx.Log().Info("waiting for cluster to form", "replicas", i.Spec.Replicas)
			ctx.RequeueAfter(consts.DefaultWaitClusterPodsNotReady, nil)
			return
		}

		ctx.Log().Info("Pods scaled up, enabling rebalancing")
		if err := ispnClient.Container().RebalanceEnable(); err != nil {
			ctx.Requeue(fmt.Errorf("unable to enable rebalancing after cluster scale up: %w", err))
			return
		}

		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				i.SetCondition(ispnv1.ConditionScalingUp, metav1.ConditionFalse, "")
			}),
		)
	}
}

func AwaitUpgrade(i *ispnv1.Infinispan, ctx pipeline.Context) {
	if i.IsUpgradeCondition() {
		ctx.Log().Info("IsUpgradeCondition")
		ctx.Requeue(nil)
	}
}

func destroyResources(i *ispnv1.Infinispan, ctx pipeline.Context) {

	type resource struct {
		name string
		obj  client.Object
	}

	resources := []resource{
		{i.GetStatefulSetName(), &appsv1.StatefulSet{}},
		{i.GetGossipRouterDeploymentName(), &appsv1.Deployment{}},
		{i.GetConfigName(), &corev1.ConfigMap{}},
		{i.Name, &corev1.Service{}},
		{i.GetPingServiceName(), &corev1.Service{}},
		{i.GetAdminServiceName(), &corev1.Service{}},
		{i.GetServiceExternalName(), &corev1.Service{}},
		{i.GetSiteServiceName(), &corev1.Service{}},
	}

	del := func(name string, obj client.Object) error {
		if err := ctx.Resources().Delete(name, obj, pipeline.RetryOnErr, pipeline.IgnoreNotFound); err != nil {
			return err
		}
		return nil
	}

	for _, r := range resources {
		if err := del(r.name, r.obj); err != nil {
			return
		}
	}

	if ctx.IsTypeSupported(pipeline.RouteGVK) {
		if err := del(i.GetServiceExternalName(), &routev1.Route{}); err != nil {
			return
		}
	} else if ctx.IsTypeSupported(pipeline.IngressGVK) {
		if err := del(i.GetServiceExternalName(), &ingressv1.Ingress{}); err != nil {
			return
		}
	}

	provision.RemoveConfigListener(i, ctx)
}
