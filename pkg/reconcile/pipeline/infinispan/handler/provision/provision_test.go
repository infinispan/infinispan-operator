package provision

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ingressv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestBuilder(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provision Unit Tests")
}

var _ = Describe("Provision", func() {

	key := types.NamespacedName{
		Name:      "infinispan-unit",
		Namespace: "default",
	}

	It("should correctly set StatefulSet PriorityClassName when defined", func() {
		mockCtrl := gomock.NewController(GinkgoT())
		resources := infinispan.NewMockResources(mockCtrl)

		ctx := infinispan.NewMockContext(mockCtrl)
		ctx.EXPECT().ConfigFiles().AnyTimes().Return(
			&infinispan.ConfigFiles{
				AdminIdentities: &infinispan.AdminIdentities{},
			},
		)
		ctx.EXPECT().Resources().AnyTimes().Return(resources)
		ctx.EXPECT().Operand().AnyTimes().Return(version.Operand{UpstreamVersion: &semver.Version{Major: 15, Minor: 0, Patch: 0}})

		ispn := &ispnv1.Infinispan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: ispnv1.InfinispanSpec{
				Replicas: 1,
				Container: ispnv1.InfinispanContainerSpec{
					Memory: "1Gi",
				},
				Scheduling: &ispnv1.SchedulingSpec{
					PriorityClassName: "example-priority-class",
				},
				Service: ispnv1.InfinispanServiceSpec{
					Type: ispnv1.ServiceTypeDataGrid,
					Container: &ispnv1.InfinispanServiceContainerSpec{
						EphemeralStorage: true,
					},
				},
				Version: "IGNORED. Required so we can call Default()",
			},
		}
		Expect((&ispnv1.InfinispanCustomDefaulter{}).Default(context.TODO(), ispn)).To(Succeed())

		// Assert PriorityClassName set when specified
		ss, err := ClusterStatefulSetSpec("statefulset", ispn, ctx)
		Expect(err).Should(BeNil())
		Expect(ss.Spec.Template.Spec.PriorityClassName).Should(Equal(ispn.Spec.Scheduling.PriorityClassName))

		// Assert PriorityClassName ignored when not specified
		ispn.Spec.Scheduling.PriorityClassName = ""
		ss, err = ClusterStatefulSetSpec("statefulset", ispn, ctx)
		Expect(err).Should(BeNil())
		Expect(ss.Spec.Template.Spec.PriorityClassName).Should(BeEmpty())
	})

	It("should propagate scheduling spec to the Gossip Router Deployment", func() {
		mockCtrl := gomock.NewController(GinkgoT())
		resources := infinispan.NewMockResources(mockCtrl)

		ctx := infinispan.NewMockContext(mockCtrl)
		ctx.EXPECT().Log().AnyTimes().Return(logr.Discard())
		ctx.EXPECT().Resources().AnyTimes().Return(resources)
		ctx.EXPECT().Operand().AnyTimes().Return(version.Operand{UpstreamVersion: &semver.Version{Major: 15, Minor: 0, Patch: 0}})

		nodeAffinity := &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{
						MatchExpressions: []corev1.NodeSelectorRequirement{{
							Key:      "feature.node.example.io/cpu-cpuid.AVX2",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{"true"},
						}},
					}},
				},
			},
		}
		tolerations := []corev1.Toleration{{
			Key:      "dedicated",
			Operator: corev1.TolerationOpEqual,
			Value:    "infinispan",
			Effect:   corev1.TaintEffectNoSchedule,
		}}

		ispn := &ispnv1.Infinispan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: ispnv1.InfinispanSpec{
				Replicas: 1,
				Scheduling: &ispnv1.SchedulingSpec{
					Affinity:          nodeAffinity,
					Tolerations:       tolerations,
					PriorityClassName: "example-priority-class",
				},
				Service: ispnv1.InfinispanServiceSpec{
					Type: ispnv1.ServiceTypeDataGrid,
					Sites: &ispnv1.InfinispanSitesSpec{
						Local: ispnv1.InfinispanSitesLocalSpec{
							Name: "site1",
						},
					},
				},
				Version: "IGNORED. Required so we can call Default()",
			},
		}
		Expect((&ispnv1.InfinispanCustomDefaulter{}).Default(context.TODO(), ispn)).To(Succeed())

		// Delete of the legacy "-tunnel" Deployment performed unconditionally
		resources.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

		var router appsv1.Deployment
		resources.EXPECT().
			CreateOrUpdate(gomock.Any(), true, gomock.Any(), gomock.Any()).
			DoAndReturn(func(obj client.Object, _ bool, mutate func() error, _ ...func(*infinispan.ResourcesConfig)) (infinispan.OperationResult, error) {
				if err := mutate(); err != nil {
					return infinispan.OperationResultNone, err
				}
				router = *obj.(*appsv1.Deployment)
				return infinispan.OperationResultUpdated, nil
			})

		readyPod := corev1.Pod{
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
			},
		}
		resources.EXPECT().
			List(gomock.Any(), gomock.AssignableToTypeOf(&corev1.PodList{}), gomock.Any()).
			DoAndReturn(func(_ map[string]string, list client.ObjectList, _ ...func(*infinispan.ResourcesConfig)) error {
				list.(*corev1.PodList).Items = []corev1.Pod{readyPod}
				return nil
			})
		ctx.EXPECT().UpdateInfinispan(gomock.Any()).DoAndReturn(func(mutate func()) error {
			mutate()
			return nil
		})

		GossipRouter(ispn, ctx)

		podSpec := router.Spec.Template.Spec
		Expect(podSpec.Affinity).Should(Equal(nodeAffinity))
		Expect(podSpec.Tolerations).Should(Equal(tolerations))
		Expect(podSpec.PriorityClassName).Should(Equal("example-priority-class"))
	})

	It("should not set any affinity/tolerations/priorityClassName on the Gossip Router Deployment when scheduling is not configured", func() {
		mockCtrl := gomock.NewController(GinkgoT())
		resources := infinispan.NewMockResources(mockCtrl)

		ctx := infinispan.NewMockContext(mockCtrl)
		ctx.EXPECT().Log().AnyTimes().Return(logr.Discard())
		ctx.EXPECT().Resources().AnyTimes().Return(resources)
		ctx.EXPECT().Operand().AnyTimes().Return(version.Operand{UpstreamVersion: &semver.Version{Major: 15, Minor: 0, Patch: 0}})

		ispn := &ispnv1.Infinispan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: ispnv1.InfinispanSpec{
				Replicas: 1,
				Service: ispnv1.InfinispanServiceSpec{
					Type: ispnv1.ServiceTypeDataGrid,
					Sites: &ispnv1.InfinispanSitesSpec{
						Local: ispnv1.InfinispanSitesLocalSpec{
							Name: "site1",
						},
					},
				},
				Version: "IGNORED. Required so we can call Default()",
			},
		}
		Expect((&ispnv1.InfinispanCustomDefaulter{}).Default(context.TODO(), ispn)).To(Succeed())

		// Delete of the legacy "-tunnel" Deployment performed unconditionally
		resources.EXPECT().Delete(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

		var router appsv1.Deployment
		resources.EXPECT().
			CreateOrUpdate(gomock.Any(), true, gomock.Any(), gomock.Any()).
			DoAndReturn(func(obj client.Object, _ bool, mutate func() error, _ ...func(*infinispan.ResourcesConfig)) (infinispan.OperationResult, error) {
				if err := mutate(); err != nil {
					return infinispan.OperationResultNone, err
				}
				router = *obj.(*appsv1.Deployment)
				return infinispan.OperationResultUpdated, nil
			})

		readyPod := corev1.Pod{
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{Ready: true}},
			},
		}
		resources.EXPECT().
			List(gomock.Any(), gomock.AssignableToTypeOf(&corev1.PodList{}), gomock.Any()).
			DoAndReturn(func(_ map[string]string, list client.ObjectList, _ ...func(*infinispan.ResourcesConfig)) error {
				list.(*corev1.PodList).Items = []corev1.Pod{readyPod}
				return nil
			})
		ctx.EXPECT().UpdateInfinispan(gomock.Any()).DoAndReturn(func(mutate func()) error {
			mutate()
			return nil
		})

		GossipRouter(ispn, ctx)

		podSpec := router.Spec.Template.Spec
		Expect(podSpec.Affinity).Should(BeNil())
		Expect(podSpec.Tolerations).Should(BeEmpty())
		Expect(podSpec.PriorityClassName).Should(BeEmpty())
	})

	It("should correctly set StatefulSet ServiceAccountName when defined", func() {
		mockCtrl := gomock.NewController(GinkgoT())
		resources := infinispan.NewMockResources(mockCtrl)

		ctx := infinispan.NewMockContext(mockCtrl)
		ctx.EXPECT().ConfigFiles().AnyTimes().Return(
			&infinispan.ConfigFiles{
				AdminIdentities: &infinispan.AdminIdentities{},
			},
		)
		ctx.EXPECT().Resources().AnyTimes().Return(resources)
		ctx.EXPECT().Operand().AnyTimes().Return(version.Operand{UpstreamVersion: &semver.Version{Major: 15, Minor: 0, Patch: 0}})

		ispn := &ispnv1.Infinispan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: ispnv1.InfinispanSpec{
				Replicas:           1,
				ServiceAccountName: "custom-sa",
				Container: ispnv1.InfinispanContainerSpec{
					Memory: "1Gi",
				},
				Service: ispnv1.InfinispanServiceSpec{
					Type: ispnv1.ServiceTypeDataGrid,
					Container: &ispnv1.InfinispanServiceContainerSpec{
						EphemeralStorage: true,
					},
				},
				Version: "IGNORED. Required so we can call Default()",
			},
		}
		Expect((&ispnv1.InfinispanCustomDefaulter{}).Default(context.TODO(), ispn)).To(Succeed())

		ss, err := ClusterStatefulSetSpec("statefulset", ispn, ctx)
		Expect(err).Should(BeNil())
		Expect(ss.Spec.Template.Spec.ServiceAccountName).Should(Equal("custom-sa"))

		// Assert ServiceAccountName is empty when not specified
		ispn.Spec.ServiceAccountName = ""
		ss, err = ClusterStatefulSetSpec("statefulset", ispn, ctx)
		Expect(err).Should(BeNil())
		Expect(ss.Spec.Template.Spec.ServiceAccountName).Should(BeEmpty())
	})

	// Regression test for https://github.com/infinispan/infinispan-operator/issues/2632: when expose.type=Route is
	// backed by an Ingress (plain K8s cluster without the Route API), the Ingress must not be deleted on every reconcile.
	It("should not delete the Ingress backing expose.type=Route when the Route API is unavailable", func() {
		mockCtrl := gomock.NewController(GinkgoT())
		resources := infinispan.NewMockResources(mockCtrl)

		ctx := infinispan.NewMockContext(mockCtrl)
		ctx.EXPECT().Resources().AnyTimes().Return(resources)
		ctx.EXPECT().IsTypeSupported(infinispan.ServiceGVK).AnyTimes().Return(false)
		ctx.EXPECT().IsTypeSupported(infinispan.RouteGVK).AnyTimes().Return(false)
		ctx.EXPECT().IsTypeSupported(infinispan.IngressGVK).AnyTimes().Return(true)

		// The Ingress must be reconciled in place; no Delete call is registered, so any deletion fails the test.
		resources.EXPECT().
			CreateOrUpdate(gomock.AssignableToTypeOf(&ingressv1.Ingress{}), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(infinispan.OperationResultUpdated, nil)

		ExternalService(exposedInfinispan(key, ispnv1.ExposeTypeRoute), ctx)
	})

	// On OpenShift (Route API available) expose.type=Route is backed by a Route, and a stale Ingress should be cleaned up.
	It("should keep the Route and delete a stale Ingress when the Route API is available", func() {
		mockCtrl := gomock.NewController(GinkgoT())
		resources := infinispan.NewMockResources(mockCtrl)

		ctx := infinispan.NewMockContext(mockCtrl)
		ctx.EXPECT().Resources().AnyTimes().Return(resources)
		ctx.EXPECT().IsTypeSupported(infinispan.ServiceGVK).AnyTimes().Return(false)
		ctx.EXPECT().IsTypeSupported(infinispan.RouteGVK).AnyTimes().Return(true)
		ctx.EXPECT().IsTypeSupported(infinispan.IngressGVK).AnyTimes().Return(true)

		// The stale Ingress is listed for deletion (the list is left empty, so no Delete calls follow)...
		resources.EXPECT().
			List(gomock.Any(), gomock.AssignableToTypeOf(&ingressv1.IngressList{}), gomock.Any()).
			Return(nil)
		// ...and the Route is reconciled.
		resources.EXPECT().
			CreateOrUpdate(gomock.AssignableToTypeOf(&routev1.Route{}), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(infinispan.OperationResultUpdated, nil)

		ExternalService(exposedInfinispan(key, ispnv1.ExposeTypeRoute), ctx)
	})
})

func exposedInfinispan(key types.NamespacedName, exposeType ispnv1.ExposeType) *ispnv1.Infinispan {
	return &ispnv1.Infinispan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: ispnv1.InfinispanSpec{
			Expose: &ispnv1.ExposeSpec{
				Type: exposeType,
				Host: "example.host",
			},
		},
	}
}
