package v2alpha1

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

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
	})
})
