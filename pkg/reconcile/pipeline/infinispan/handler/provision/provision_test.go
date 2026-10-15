package provision

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/golang/mock/gomock"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	ingressv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
					ContainerSpec: ispnv1.ContainerSpec{
						Memory: "1Gi",
					},
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
					ContainerSpec: ispnv1.ContainerSpec{
						Memory: "1Gi",
					},
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
