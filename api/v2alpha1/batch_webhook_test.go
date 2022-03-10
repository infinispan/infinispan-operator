package v2alpha1

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/utils/pointer"

	// +kubebuilder:scaffold:imports
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var _ = Describe("Batch Webhook", func() {

	const timeout = time.Second * 30
	const interval = time.Second * 1

	key := types.NamespacedName{
		Name:      "batch-envtest",
		Namespace: "default",
	}

	AfterEach(func() {
		// Delete created Batch resources
		By("Expecting to delete successfully")
		Eventually(func() error {
			f := &Batch{}
			if err := k8sClient.Get(ctx, key, f); err != nil {
				var statusError *k8serrors.StatusError
				if !errors.As(err, &statusError) {
					return err
				}
				// If the Batch does not exist, do nothing
				if statusError.ErrStatus.Code == 404 {
					return nil
				}
			}
			return k8sClient.Delete(ctx, f)
		}, timeout, interval).Should(Succeed())

		By("Expecting to delete finish")
		Eventually(func() error {
			f := &Batch{}
			return k8sClient.Get(ctx, key, f)
		}, timeout, interval).ShouldNot(Succeed())
	})

	Context("Batch", func() {
		It("Should create successfully", func() {

			created := &Batch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: BatchSpec{
					Cluster: "some-cluster",
					Config:  pointer.String("create cache --template=org.infinispan.DIST_SYNC batch-cache"),
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())
		})

		It("Should return error if both config and configMap are specified during creation", func() {

			rejected := &Batch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: BatchSpec{
					Cluster:   "some-cluster",
					Config:    pointer.String("Config"),
					ConfigMap: pointer.String("ConfigMap"),
				},
			}

			Expect(k8sClient.Create(ctx, rejected)).ShouldNot(Succeed())
		})

		It("Should return error if neither config or configMap are specified during creation", func() {

			rejected := &Batch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: BatchSpec{
					Cluster: "some-cluster",
				},
			}

			err := k8sClient.Create(ctx, rejected)
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueRequired, "spec.configMap", "'Spec.config' OR 'spec.ConfigMap' must be configured"})
		})

		It("Should return error if any spec value is updated", func() {

			created := &Batch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: BatchSpec{
					Cluster: "some-cluster",
					Config:  pointer.String("create cache --template=org.infinispan.DIST_SYNC batch-cache"),
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())

			// Ensure Spec is immutable on update
			updated := &Batch{}

			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.Cluster = "New Cluster"

			cause := statusDetailCause{"FieldValueForbidden", "spec", "The Batch spec is immutable and cannot be updated after initial Batch creation"}
			expectInvalidErrStatus(k8sClient.Update(ctx, updated), cause)

			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.Config = pointer.String("New Config")
			expectInvalidErrStatus(k8sClient.Update(ctx, updated), cause)

			Expect(k8sClient.Get(ctx, key, updated)).Should(Succeed())
			updated.Spec.ConfigMap = pointer.String("New ConfigMap")
			expectInvalidErrStatus(k8sClient.Update(ctx, updated), cause)
		})
	})
})
