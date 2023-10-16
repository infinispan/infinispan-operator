package provision

import (
	"testing"

	"github.com/golang/mock/gomock"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		ctx.EXPECT().Resources().Return(resources)

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
			},
		}

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
})
