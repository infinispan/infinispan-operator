package v2alpha1

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	// +kubebuilder:scaffold:imports
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Schema Webhook", func() {

	const timeout = time.Second * 30
	const interval = time.Second * 1

	key := types.NamespacedName{
		Name:      "schema-envtest",
		Namespace: "default",
	}

	AfterEach(func() {
		// Delete created Schema resources
		By("Expecting to delete successfully")
		Eventually(func() error {
			f := &Schema{}
			if err := k8sClient.Get(ctx, key, f); err != nil {
				var statusError *k8serrors.StatusError
				if !errors.As(err, &statusError) {
					return err
				}
				if statusError.ErrStatus.Code == 404 {
					return nil
				}
			}
			return k8sClient.Delete(ctx, f)
		}, timeout, interval).Should(Succeed())

		By("Expecting to delete finish")
		Eventually(func() error {
			f := &Schema{}
			return k8sClient.Get(ctx, key, f)
		}, timeout, interval).ShouldNot(Succeed())
	})

	Context("Schema", func() {
		It("Should create successfully with valid spec", func() {

			created := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{
					ClusterName: "some-cluster",
					Schema:      "message Person { required string name = 1; }",
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())

			fetched := &Schema{}
			Expect(k8sClient.Get(ctx, key, fetched)).Should(Succeed())
			Expect(fetched.Spec.ClusterName).Should(Equal("some-cluster"))
			Expect(fetched.Spec.Schema).Should(Equal("message Person { required string name = 1; }"))
		})

		It("Should return error if required fields not provided", func() {

			rejected := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{},
			}

			err := k8sClient.Create(ctx, rejected)
			expectInvalidErrStatus(err,
				statusDetailCause{metav1.CauseTypeFieldValueRequired, "spec.clusterName", "'spec.clusterName' must be configured"},
				statusDetailCause{metav1.CauseTypeFieldValueRequired, "spec.schema", "'spec.schema' must be configured"},
			)
		})

		It("Should return error if clusterName field is updated", func() {

			created := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{
					ClusterName: "initial-value",
					Schema:      "message Person { required string name = 1; }",
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())
			updated := &Schema{}
			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.ClusterName = "new-value"

			cause := statusDetailCause{"FieldValueForbidden", "spec.clusterName", "Schema clusterName is immutable and cannot be updated after initial Schema creation"}
			expectInvalidErrStatus(k8sClient.Update(ctx, updated), cause)
		})

		It("Should return error if name field is updated", func() {

			created := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{
					ClusterName: "some-cluster",
					Name:        "initial-schema-name",
					Schema:      "message Person { required string name = 1; }",
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())
			updated := &Schema{}
			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.Name = "new-schema-name"

			cause := statusDetailCause{"FieldValueForbidden", "spec.name", "Schema name is immutable and cannot be updated after initial Schema creation"}
			expectInvalidErrStatus(k8sClient.Update(ctx, updated), cause)
		})

		It("Should allow updating the schema definition", func() {

			created := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{
					ClusterName: "some-cluster",
					Schema:      "message Person { required string name = 1; }",
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())
			updated := &Schema{}
			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.Schema = "message Person { required string name = 1; optional int32 age = 2; }"
			Expect(k8sClient.Update(ctx, updated)).Should(Succeed())
		})

		It("Should prevent two Schema CRs being created with the same name and clusterName", func() {

			original := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{
					ClusterName: "some-cluster",
					Name:        "person.proto",
					Schema:      "message Person { required string name = 1; }",
				},
			}
			duplicate := original.DeepCopy()
			duplicate.Name = duplicate.Name + "-1"

			Expect(k8sClient.Create(ctx, original)).Should(Succeed())

			err := k8sClient.Create(ctx, duplicate)
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueDuplicate, "spec.name", "Schema CR already exists for cluster"})
		})

		It("Should allow two Schema CRs with the same name for different clusters", func() {

			cluster1Schema := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{
					ClusterName: "some-cluster",
					Name:        "person.proto",
					Schema:      "message Person { required string name = 1; }",
				},
			}
			cluster2Schema := cluster1Schema.DeepCopy()
			cluster2Schema.Name = key.Name + "1"
			cluster2Schema.Spec.ClusterName = "another-cluster"

			Expect(k8sClient.Create(ctx, cluster1Schema)).Should(Succeed())
			Expect(k8sClient.Create(ctx, cluster2Schema)).Should(Succeed())
		})

		It("Should allow two Schema CRs with the same name and clusterName in different namespaces", func() {

			namespace1Schema := &Schema{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: SchemaSpec{
					ClusterName: "some-cluster",
					Name:        "person.proto",
					Schema:      "message Person { required string name = 1; }",
				},
			}

			namespace2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "schema-another-namespace",
				},
			}

			namespace2Schema := namespace1Schema.DeepCopy()
			namespace2Schema.Namespace = namespace2.Name

			cleanup := func() {
				_ = k8sClient.Delete(ctx, namespace2)
			}
			defer cleanup()

			Expect(k8sClient.Create(ctx, namespace2)).Should(Succeed())
			Expect(k8sClient.Create(ctx, namespace1Schema)).Should(Succeed())
			Expect(k8sClient.Create(ctx, namespace2Schema)).Should(Succeed())
		})
	})
})
