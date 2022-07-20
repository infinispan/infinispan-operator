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

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var _ = Describe("Cache Webhook", func() {

	const timeout = time.Second * 30
	const interval = time.Second * 1

	key := types.NamespacedName{
		Name:      "cache-envtest",
		Namespace: "default",
	}

	AfterEach(func() {
		// Delete created Cache resources
		By("Expecting to delete successfully")
		Eventually(func() error {
			f := &Cache{}
			if err := k8sClient.Get(ctx, key, f); err != nil {
				var statusError *k8serrors.StatusError
				if !errors.As(err, &statusError) {
					return err
				}
				// If the Cache does not exist, do nothing
				if statusError.ErrStatus.Code == 404 {
					return nil
				}
			}
			return k8sClient.Delete(ctx, f)
		}, timeout, interval).Should(Succeed())

		By("Expecting to delete finish")
		Eventually(func() error {
			f := &Cache{}
			return k8sClient.Get(ctx, key, f)
		}, timeout, interval).ShouldNot(Succeed())
	})

	Context("Cache", func() {
		It("Should create successfully", func() {

			created := &Cache{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: CacheSpec{
					AdminAuth: &AdminAuth{
						SecretName: "some-secret",
					},
					ClusterName: "some-cluster",
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())

			updated := &Cache{}
			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			Expect(updated.Spec.AdminAuth).Should(BeNil())
		})

		It("Should return error if required fields not provided", func() {

			rejected := &Cache{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: CacheSpec{},
			}

			err := k8sClient.Create(ctx, rejected)
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueRequired, "spec.clusterName", "'spec.clusterName' must be configured"})
		})

		It("Should return error if clusterName field is updated", func() {

			created := &Cache{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: CacheSpec{
					ClusterName: "initial-value",
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())
			updated := &Cache{}
			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.ClusterName = "new-value"

			cause := statusDetailCause{"FieldValueForbidden", "spec.clusterName", "Cache clusterName is immutable and cannot be updated after initial Cache creation"}
			expectInvalidErrStatus(k8sClient.Update(ctx, updated), cause)
		})

		It("Should return error if name field is updated", func() {

			created := &Cache{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: CacheSpec{
					ClusterName: "some-cluster",
					Name:        "initial-cache-name",
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())
			updated := &Cache{}
			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.Name = "new-cache-name"

			cause := statusDetailCause{"FieldValueForbidden", "spec.name", "Cache name is immutable and cannot be updated after initial Cache creation"}
			expectInvalidErrStatus(k8sClient.Update(ctx, updated), cause)
		})

		It("Should prevent two Cache CRs being created with the same spec.cacheName and spec.clusterName", func() {

			original := &Cache{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: CacheSpec{
					ClusterName: "some-cluster",
					Name:        "some-cache",
				},
			}
			duplicate := original.DeepCopy()
			duplicate.Name = duplicate.Name + "-1"

			Expect(k8sClient.Create(ctx, original)).Should(Succeed())

			err := k8sClient.Create(ctx, duplicate)
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueDuplicate, "spec.name", "Cache CR already exists for cluster"})
		})

		It("Should allow two Cache CRs with the same spec.Name to be created for different clusters", func() {

			cluster1Cache := &Cache{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: CacheSpec{
					ClusterName: "some-cluster",
					Name:        "some-cache",
				},
			}
			cluster2Cache := cluster1Cache.DeepCopy()
			cluster2Cache.Name = key.Name + "1"
			cluster2Cache.Spec.ClusterName = "another-cluster"

			Expect(k8sClient.Create(ctx, cluster1Cache)).Should(Succeed())
			Expect(k8sClient.Create(ctx, cluster2Cache)).Should(Succeed())
		})

		It("Should allow two Cache CRs being created with the same spec.cacheName and spec.clusterName in different namespaces", func() {

			namespace1Cache := &Cache{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: CacheSpec{
					ClusterName: "some-cluster",
					Name:        "some-cache",
				},
			}

			namespace2 := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "another-namespace",
				},
			}

			namespace2Cache := namespace1Cache.DeepCopy()
			namespace2Cache.Namespace = namespace2.Name

			cleanup := func() {
				_ = k8sClient.Delete(ctx, namespace2)
			}
			defer cleanup()

			Expect(k8sClient.Create(ctx, namespace2)).Should(Succeed())
			Expect(k8sClient.Create(ctx, namespace1Cache)).Should(Succeed())
			Expect(k8sClient.Create(ctx, namespace2Cache)).Should(Succeed())
		})
	})
})
