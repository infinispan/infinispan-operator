package manage

import (
	"fmt"
	"sort"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func PreliminaryChecks(i *ispnv1.Infinispan, ctx pipeline.Context) {
	// Initialize status replicas on Upgrade from older Operator version
	if i.Status.Replicas == nil {
		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				i.Status.Replicas = &i.Spec.Replicas
			}),
		)
	}

	if i.GetCondition(ispnv1.ConditionPrelimChecksPassed).Status == metav1.ConditionFalse {
		ctx.Requeue(
			ctx.UpdateInfinispan(func() {

				if ctx.IsTypeSupported(pipeline.ServiceMonitorGVK) {
					i.ApplyMonitoringAnnotation()
				}
				// Ensure that CR has all the operator required labels
				i.ApplyOperatorMeta(ctx.DefaultLabels(), ctx.DefaultAnnotations())
				i.SetCondition(ispnv1.ConditionPrelimChecksPassed, metav1.ConditionTrue, "")
				i.Status.Operand = OperandStatus(i, ispnv1.OperandPhasePending, ctx.Operand())
			}),
		)
	}
}

func OperatorStatusChecks(i *ispnv1.Infinispan, ctx pipeline.Context) {
	operatorPod := kube.GetOperatorPodName()
	// CacheService is no longer supported. Set WellFormed = false and stop reconciliation
	if i.IsCache() {
		_ = ctx.UpdateInfinispan(func() {
			i.SetCondition(ispnv1.ConditionWellFormed, metav1.ConditionFalse, "Please change to service type DataGrid. CacheService is no longer supported.")
			i.Status.Operator.Pod = operatorPod
		})
		ctx.Stop(nil)
		return
	}
	// Pod name is changed, means operator restarted
	if i.Status.Operator.Pod != operatorPod {
		if ctx.Operand().Deprecated {
			msg := fmt.Sprintf("Infinispan version '%s' will be removed in a subsequent Operator release. You must upgrade to a non-deprecated release before upgrading the Operator.", i.Spec.Version)
			ctx.EventRecorder().Event(i, corev1.EventTypeWarning, "DeprecatedOperandVersion", msg)
			ctx.Log().Error(nil, msg)
		}

		if i.Spec.Autoscale != nil {
			errMsg := "Autoscale is no longer supported. Please remove spec.autoscale field."
			ctx.EventRecorder().Event(i, corev1.EventTypeWarning, "AutoscaleNotSupported", errMsg)
			ctx.Log().Error(fmt.Errorf("AutoscaleNotSupported"), errMsg)
		}
		ctx.Requeue(
			ctx.UpdateInfinispan(func() {
				i.Status.Operator.Pod = operatorPod
			}),
		)
	}
}

func PodStatus(i *ispnv1.Infinispan, ctx pipeline.Context) {
	ss := &appsv1.StatefulSet{}
	if err := ctx.Resources().Load(i.GetStatefulSetName(), ss, pipeline.RetryOnErr); err != nil {
		return
	}
	var ready, starting []string
	pods, err := ctx.InfinispanPods()
	if err != nil {
		return
	}
	for _, pod := range pods.Items {
		if kube.IsPodReady(pod) {
			ready = append(ready, pod.GetName())
		} else {
			starting = append(starting, pod.GetName())
		}
	}
	ctx.Log().Info("Found deployments with status ", "starting", starting, "ready", ready)
	_ = ctx.UpdateInfinispan(func() {
		i.Status.PodStatus = ispnv1.DeploymentStatus{
			Starting: starting,
			Ready:    ready,
		}
	})
}

func AwaitWellFormedCondition(i *ispnv1.Infinispan, ctx pipeline.Context) {
	statefulSet := &appsv1.StatefulSet{}
	// Ignore NotFound. StatefulSet hasn't been created yet, so it's not possible for cluster to be well-formed
	if err := ctx.Resources().Load(i.GetStatefulSetName(), statefulSet, pipeline.IgnoreNotFound, pipeline.RetryOnErr); err != nil {
		return
	}

	podList, err := ctx.InfinispanPods()
	if err != nil {
		return
	}

	wellFormed := wellFormedCondition(i, ctx, podList)
	if err := ctx.UpdateInfinispan(func() {
		i.SetConditions(wellFormed)
		if wellFormed.Status == metav1.ConditionTrue {
			i.Status.Operand.Phase = ispnv1.OperandPhaseRunning
		}
	}); err != nil {
		return
	}

	if i.NotClusterFormed(len(podList.Items), int(i.Spec.Replicas)) {
		ctx.Log().Info("Cluster not well-formed, retrying ...")
		ctx.Log().Info(fmt.Sprintf("podList.Items=%d, i.Spec.Replicas=%d", len(podList.Items), int(i.Spec.Replicas)))
		ctx.RequeueAfter(consts.DefaultWaitClusterNotWellFormed, nil)
	}
}

func wellFormedCondition(i *ispnv1.Infinispan, ctx pipeline.Context, podList *corev1.PodList) ispnv1.InfinispanCondition {
	clusterViews := make(map[string]bool)
	numPods := int32(len(podList.Items))
	var podErrors []string

	// Avoid contacting the server(s) if we're still waiting for pods
	if numPods < i.Spec.Replicas {
		podErrors = append(podErrors, fmt.Sprintf("Running %d pods. Needed %d", numPods, i.Spec.Replicas))
	} else {
		for _, pod := range podList.Items {
			if kube.IsPodReady(pod) {
				if members, err := ctx.InfinispanClientForPod(pod.Name).Container().Members(); err == nil {
					sort.Strings(members)
					clusterView := strings.Join(members, ",")
					clusterViews[clusterView] = true
				} else {
					podErrors = append(podErrors, pod.Name+": "+err.Error())
				}
			} else {
				// Pod not ready, no need to query
				podErrors = append(podErrors, pod.Name+": pod not ready")
			}
		}
	}

	// Evaluating WellFormed condition
	wellFormed := ispnv1.InfinispanCondition{Type: ispnv1.ConditionWellFormed}
	views := make([]string, len(clusterViews))
	index := 0
	for k := range clusterViews {
		views[index] = k
		index++
	}
	sort.Strings(views)
	if len(podErrors) == 0 {
		if len(views) == 1 {
			wellFormed.Status = metav1.ConditionTrue
			wellFormed.Message = "View: " + views[0]
		} else {
			wellFormed.Status = metav1.ConditionFalse
			wellFormed.Message = "Views: " + strings.Join(views, ",")
		}
	} else {
		wellFormed.Status = metav1.ConditionUnknown
		wellFormed.Message = "Errors: " + strings.Join(podErrors, ",") + " Views: " + strings.Join(views, ",")
	}
	return wellFormed
}

func XSiteViewCondition(i *ispnv1.Infinispan, ctx pipeline.Context) {
	podList, err := ctx.InfinispanPods()
	if err != nil {
		return
	}

	crossSiteViewCondition, err := getCrossSiteViewCondition(ctx, podList, i.GetSiteLocationsName())
	if err != nil {
		ctx.Requeue(fmt.Errorf("unable to set CrossSiteViewFormed condition: %w", err))
		return
	}

	// ISPN-13116 If xsite view has been formed, then we must perform state-transfer to all sites if a SFS recovery has occurred
	if crossSiteViewCondition.Status == metav1.ConditionTrue {
		podName := podList.Items[0].Name
		k8s := ctx.Kubernetes()
		logs, err := k8s.Logs(provision.InfinispanContainer, podName, i.Namespace, false, ctx.Ctx())
		if err != nil {
			ctx.Log().Error(err, fmt.Sprintf("Unable to retrive logs for i pod %s", podName))
		}
		if strings.Contains(logs, "ISPN000643") {
			if err := ctx.InfinispanClientForPod(podName).Container().Xsite().PushAllState(); err != nil {
				ctx.Log().Error(err, "Unable to push xsite state after SFS data recovery")
			}
		}
	}

	err = ctx.UpdateInfinispan(func() {
		i.SetConditions(*crossSiteViewCondition)
	})
	if err != nil || crossSiteViewCondition.Status != metav1.ConditionTrue {
		ctx.RequeueAfter(consts.DefaultWaitOnCluster, err)
	}
}

func getCrossSiteViewCondition(ctx pipeline.Context, podList *corev1.PodList, siteLocations []string) (*ispnv1.InfinispanCondition, error) {
	for _, item := range podList.Items {
		cacheManager, err := ctx.InfinispanClientForPod(item.Name).Container().Info()
		if err == nil {
			if cacheManager.Coordinator {
				// Perform cross-site view validation
				crossSiteViewFormed := &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionTrue}
				sitesView := make(map[string]bool)
				var err error
				if cacheManager.SitesView == nil {
					err = fmt.Errorf("retrieving the cross-site view is not supported with the server image you are using")
				}
				for _, site := range *cacheManager.SitesView {
					sitesView[site.(string)] = true
				}
				if err == nil {
					for _, location := range siteLocations {
						if !sitesView[location] {
							crossSiteViewFormed.Status = metav1.ConditionFalse
							crossSiteViewFormed.Message = fmt.Sprintf("Site '%s' not ready", location)
							break
						}
					}
					if crossSiteViewFormed.Status == metav1.ConditionTrue {
						crossSiteViewFormed.Message = fmt.Sprintf("Cross-Site view: %s", strings.Join(siteLocations, ","))
					}
				} else {
					crossSiteViewFormed.Status = metav1.ConditionUnknown
					crossSiteViewFormed.Message = fmt.Sprintf("Error: %s", err.Error())
				}
				return crossSiteViewFormed, nil
			}
		}
	}
	return &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionFalse, Message: "Coordinator not ready"}, nil
}

func OperandStatus(i *ispnv1.Infinispan, phase ispnv1.OperandPhase, operand version.Operand) ispnv1.OperandStatus {
	customImg := i.Spec.Image != nil
	var img string
	if customImg {
		img = *i.Spec.Image
	} else {
		img = operand.Image
	}

	return ispnv1.OperandStatus{
		CustomImage: customImg,
		Deprecated:  operand.Deprecated,
		Image:       img,
		Phase:       phase,
		Version:     operand.Ref(),
	}
}
