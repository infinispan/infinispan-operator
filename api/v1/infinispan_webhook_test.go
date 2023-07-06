package v1

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/hash"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	// +kubebuilder:scaffold:imports
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
)

var _ = Describe("Infinispan Webhooks", func() {

	const timeout = time.Second * 30
	const interval = time.Second * 1

	key := types.NamespacedName{
		Name:      "infinispan-envtest",
		Namespace: "default",
	}

	AfterEach(func() {
		// Delete created Batch resources
		By("Expecting to delete successfully")
		Eventually(func() error {
			f := &Infinispan{}
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
			f := &Infinispan{}
			return k8sClient.Get(ctx, key, f)
		}, timeout, interval).ShouldNot(Succeed())
	})

	Context("Infinispan", func() {
		It("Should initiate Cache Service defaults", func() {

			created := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())

			Expect(k8sClient.Get(ctx, key, created)).Should(Succeed())
			spec := created.Spec
			// Ensure default values correctly set
			Expect(spec.Service.Type).Should(Equal(ServiceTypeCache))
			Expect(spec.Service.ReplicationFactor).Should(Equal(int32(2)))
			Expect(spec.Container.Memory).Should(Equal(consts.DefaultMemorySize.String()))
			Expect(spec.Security.EndpointAuthentication).Should(Equal(pointer.BoolPtr(true)))
			Expect(spec.Security.EndpointSecretName).Should(Equal(created.GetSecretName()))
			Expect(spec.Upgrades.Type).Should(Equal(UpgradeTypeShutdown))
			Expect(spec.ConfigListener.Enabled).Should(BeTrue())
			Expect(spec.ConfigListener.Logging.Level).Should(Equal(ConfigListenerLoggingInfo))
			Expect(spec.Cryostat.Enabled).Should(Equal(false))
		})

		It("Should initiate DataGrid defaults", func() {

			created := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Service: InfinispanServiceSpec{
						Type: ServiceTypeDataGrid,
					},
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())

			Expect(k8sClient.Get(ctx, key, created)).Should(Succeed())
			spec := created.Spec
			// Ensure default values correctly set
			Expect(spec.Service.Type).Should(Equal(ServiceTypeDataGrid))
			Expect(spec.Service.Container.Storage).Should(Equal(pointer.StringPtr(consts.DefaultPVSize.String())))
			Expect(spec.Service.ReplicationFactor).Should(BeZero())
			Expect(spec.Container.Memory).Should(Equal(consts.DefaultMemorySize.String()))
			Expect(spec.Security.EndpointAuthentication).Should(Equal(pointer.BoolPtr(true)))
			Expect(spec.Security.EndpointSecretName).Should(Equal(created.GetSecretName()))
			Expect(spec.Upgrades.Type).Should(Equal(UpgradeTypeShutdown))
			Expect(spec.ConfigListener.Enabled).Should(BeTrue())
			Expect(spec.ConfigListener.Logging.Level).Should(Equal(ConfigListenerLoggingInfo))
			Expect(spec.Cryostat.Enabled).Should(Equal(false))
		})

		It("Should calculate default Labels", func() {

			testTable := []struct {
				Labels            string
				PodLabels         string
				ExpectedLabels    string
				ExpectedPodLabels string
			}{
				{"", "", "", ""},
				{"{\"label\":\"value\"}", "", "label", ""},
				{"", "{\"label\":\"value\"}", "", "label"},
				{"{\"l2\":\"v2\",\"l1\":\"v1\"}", "", "l1,l2", ""},
				{"", "{\"l2\":\"v2\",\"l1\":\"v1\"}", "", "l1,l2"},
				{"{\"l2\":\"v2\",\"l1\":\"v1\"}", "{\"lp2\":\"v2\",\"lp1\":\"v1\"}", "l1,l2", "lp1,lp2"},
			}

			for _, testItem := range testTable {
				err := os.Setenv("INFINISPAN_OPERATOR_TARGET_LABELS", testItem.Labels)
				Expect(err).ShouldNot(HaveOccurred())

				err = os.Setenv("INFINISPAN_OPERATOR_POD_TARGET_LABELS", testItem.PodLabels)
				Expect(err).ShouldNot(HaveOccurred())

				labels, annotations, err := LoadDefaultLabelsAndAnnotations()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(annotations[OperatorTargetLabels]).Should(Equal(testItem.ExpectedLabels))
				Expect(annotations[OperatorPodTargetLabels]).Should(Equal(testItem.ExpectedPodLabels))

				labelMap := make(map[string]string)
				if testItem.Labels != "" {
					err = json.Unmarshal([]byte(testItem.Labels), &labelMap)
					Expect(err).ShouldNot(HaveOccurred())
				}

				labelPodMap := make(map[string]string)
				if testItem.PodLabels != "" {
					err = json.Unmarshal([]byte(testItem.PodLabels), &labelPodMap)
					Expect(err).ShouldNot(HaveOccurred())
				}

				for n, v := range labelMap {
					labelPodMap[n] = v
				}

				if testItem.PodLabels == "" && len(labels) == 0 {
					Expect(labels).Should(BeEmpty())
				} else {
					Expect(labels).Should(Equal(labelPodMap))
				}
			}
		})

		It("Should calculate default Annotations", func() {

			testTable := []struct {
				Annotations            string
				PodAnnotations         string
				ExpectedAnnotations    string
				ExpectedPodAnnotations string
			}{
				{"", "", "", ""},
				{"{\"annotation\":\"value\"}", "", "annotation", ""},
				{"", "{\"annotation\":\"value\"}", "", "annotation"},
				{"{\"a2\":\"v2\",\"a1\":\"v1\"}", "", "a1,a2", ""},
				{"", "{\"a2\":\"v2\",\"a1\":\"v1\"}", "", "a1,a2"},
				{"{\"a2\":\"v2\",\"a1\":\"v1\"}", "{\"ap2\":\"v2\",\"ap1\":\"v1\"}", "a1,a2", "ap1,ap2"},
			}

			for _, testItem := range testTable {
				err := os.Setenv("INFINISPAN_OPERATOR_TARGET_ANNOTATIONS", testItem.Annotations)
				Expect(err).ShouldNot(HaveOccurred())

				err = os.Setenv("INFINISPAN_OPERATOR_POD_TARGET_ANNOTATIONS", testItem.PodAnnotations)
				Expect(err).ShouldNot(HaveOccurred())

				_, annotations, err := LoadDefaultLabelsAndAnnotations()
				Expect(err).ShouldNot(HaveOccurred())
				Expect(annotations[OperatorTargetAnnotations]).Should(Equal(testItem.ExpectedAnnotations))
				Expect(annotations[OperatorPodTargetAnnotations]).Should(Equal(testItem.ExpectedPodAnnotations))

				// We've already tested the content of the target annotations, so remove to simplify user annotation comparison
				delete(annotations, OperatorTargetAnnotations)
				delete(annotations, OperatorPodTargetAnnotations)
				delete(annotations, OperatorTargetLabels)
				delete(annotations, OperatorPodTargetLabels)

				annotationMap := make(map[string]string)
				if testItem.Annotations != "" {
					err = json.Unmarshal([]byte(testItem.Annotations), &annotationMap)
					Expect(err).ShouldNot(HaveOccurred())
				}

				annotationPodMap := make(map[string]string)
				if testItem.PodAnnotations != "" {
					err = json.Unmarshal([]byte(testItem.PodAnnotations), &annotationPodMap)
					Expect(err).ShouldNot(HaveOccurred())
				}

				for n, v := range annotationMap {
					annotationPodMap[n] = v
				}

				if testItem.PodAnnotations == "" && len(annotations) == 0 {
					Expect(annotations).Should(BeEmpty())
				} else {
					Expect(annotations).Should(Equal(annotationPodMap))
				}
			}
		})

		It("Should return error if certSecretName not provided", func() {

			failed := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Security: InfinispanSecurity{
						EndpointEncryption: &EndpointEncryption{
							Type: CertificateSourceTypeSecret,
						},
					},
				},
			}

			err := k8sClient.Create(ctx, failed)
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueRequired, "spec.security.endpointEncryption.certSecretName", "certificateSourceType=Secret' to be configured"})
		})

		It("Should return error if Cache Service does not have sufficient memory", func() {

			failed := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Container: InfinispanContainerSpec{
						Memory: "1Mi",
					},
				},
			}

			err := k8sClient.Create(ctx, failed)
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueInvalid, "spec.container.memory", "Not enough memory allocated"})
		})

		It("Should return error if malformed memory or CPU request is greater than limit", func() {

			failed := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Container: InfinispanContainerSpec{
						Memory: "1Gi:5Gi",
						CPU:    "1000m:2000m",
					},
					ConfigListener: &ConfigListenerSpec{
						Enabled: true,
						Memory:  "1Gi:5Gi",
						CPU:     "1000m:2000m",
					},
				},
			}

			err := k8sClient.Create(ctx, failed)
			expectInvalidErrStatus(err, []statusDetailCause{{
				metav1.CauseTypeFieldValueInvalid, "spec.container.cpu", "exceeds limit",
			}, {
				metav1.CauseTypeFieldValueInvalid, "spec.container.memory", "exceeds limit",
			}, {
				metav1.CauseTypeFieldValueInvalid, "spec.configListener.cpu", "exceeds limit",
			}, {
				metav1.CauseTypeFieldValueInvalid, "spec.configListener.memory", "exceeds limit",
			}}...)
		})

		It("Should return error if features not supported with HotRollingUpgrade ", func() {

			failed := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Upgrades: &InfinispanUpgradesSpec{
						Type: UpgradeTypeHotRodRolling,
					},
					Service: InfinispanServiceSpec{
						Sites: &InfinispanSitesSpec{
							Local: InfinispanSitesLocalSpec{
								Expose: CrossSiteExposeSpec{
									Type: CrossSiteExposeTypeClusterIP,
								},
							},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, failed)
			expectInvalidErrStatus(err, []statusDetailCause{{
				"FieldValueForbidden", "spec.service.type", "spec.service.type=DataGrid",
			}, {
				"FieldValueForbidden", "spec.service.sites", "XSite not supported",
			}}...)
		})

		It("Should convert XSite Host and Port spec fields to URL", func() {

			created := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Service: InfinispanServiceSpec{
						Sites: &InfinispanSitesSpec{
							Locations: []InfinispanSiteLocationSpec{{
								Name:        "SiteB",
								ClusterName: "example-clustera",
								Host:        pointer.StringPtr("some.host.com"),
								Port:        pointer.Int32Ptr(6443),
							}},
							Local: InfinispanSitesLocalSpec{
								Expose: CrossSiteExposeSpec{
									Type: CrossSiteExposeTypeClusterIP,
								},
							},
						},
						Type: ServiceTypeDataGrid,
					},
				},
			}

			Expect(k8sClient.Create(ctx, created)).Should(Succeed())

			Expect(k8sClient.Get(ctx, key, created)).Should(Succeed())
			spec := created.Spec
			Expect(spec.Service.Sites.Locations).Should(HaveLen(1))
			Expect(spec.Service.Sites.Locations[0].URL).Should(Equal("infinispan+xsite://some.host.com:6443"))
		})

		It("Should reject invalid external artifact", func() {

			ispn := Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Dependencies: &InfinispanExternalDependencies{
						Artifacts: []InfinispanExternalArtifacts{{
							Url:  "https://test.com",
							Hash: "sha1", // Missing checksum
						}},
					},
				},
			}

			err := k8sClient.Create(ctx, ispn.DeepCopy())
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueInvalid, "spec.dependencies.artifacts.hash", "in body should match"})

			ispn.Spec.Dependencies.Artifacts[0].Hash = "sha1:" + hash.HashString("made up")
			Expect(k8sClient.Create(ctx, ispn.DeepCopy())).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ispn)).Should(Succeed())

			// Http
			ispn.Spec.Dependencies.Artifacts[0].Url = "http://test.com"
			Expect(k8sClient.Create(ctx, ispn.DeepCopy())).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ispn)).Should(Succeed())

			// Invalid Http
			ispn.Spec.Dependencies.Artifacts[0].Url = "httasfap://test.com"
			err = k8sClient.Create(ctx, ispn.DeepCopy())
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueInvalid, "spec.dependencies.artifacts.url", "should match"})

			// FTP
			ispn.Spec.Dependencies.Artifacts[0].Url = "ftp://test.com"
			Expect(k8sClient.Create(ctx, ispn.DeepCopy())).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ispn)).Should(Succeed())

			// Maven
			ispn.Spec.Dependencies.Artifacts[0] = InfinispanExternalArtifacts{
				Maven: "org.postgresql:postgresql:42.3.1",
			}
			Expect(k8sClient.Create(ctx, ispn.DeepCopy())).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ispn)).Should(Succeed())

			// Invalid Maven
			ispn.Spec.Dependencies.Artifacts[0] = InfinispanExternalArtifacts{
				Maven: "http://org.postgresql:postgresql:42.3.1",
			}
			err = k8sClient.Create(ctx, ispn.DeepCopy())
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueInvalid, "spec.dependencies.artifacts.maven", "should match"})

			// Ensure that an artifacts Url and Maven field can't be set at the same time
			ispn.Spec.Dependencies.Artifacts[0] = InfinispanExternalArtifacts{
				Maven: "org.postgresql:postgresql:42.3.1",
				Url:   "ftp://test.com",
			}
			err = k8sClient.Create(ctx, ispn.DeepCopy())
			expectInvalidErrStatus(err, statusDetailCause{metav1.CauseTypeFieldValueDuplicate, "spec.dependencies.artifacts[0]", "At most one of"})
		})

		It("Should set the latest available Infinispan version if not specified", func() {

			ispn := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
				},
			}

			Expect(k8sClient.Create(ctx, ispn)).Should(Succeed())
			Expect(k8sClient.Get(ctx, key, ispn)).Should(Succeed())
			Expect(ispn.Spec.Version).Should(Equal("13.0.10"))
		})

		It("Should throw an error if an invalid version is specified", func() {

			failed := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Version:  "9.0.0",
				},
			}
			err := k8sClient.Create(ctx, failed)
			expectInvalidErrStatus(err, statusDetailCause{
				metav1.CauseTypeFieldValueInvalid, "spec.version", "unknown version",
			})
		})

		It("Should throw an error if version downgrades are attempted with GracefulShutdown", func() {

			ispn := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Version:  "13.0.10",
				},
			}
			Expect(k8sClient.Create(ctx, ispn)).Should(Succeed())
			ispn.Spec.Version = "13.0.9"
			err := k8sClient.Update(ctx, ispn)
			expectInvalidErrStatus(err, statusDetailCause{
				"FieldValueForbidden", "spec.version", "downgrading not supported",
			})
		})
		It("Should throw an error if rollback attempted when no HR rolling upgrade in progress", func() {

			ispn := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Service: InfinispanServiceSpec{
						Type: ServiceTypeDataGrid,
					},
					Replicas: 1,
					Version:  "13.0.10",
					Upgrades: &InfinispanUpgradesSpec{
						Type: UpgradeTypeHotRodRolling,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ispn)).Should(Succeed())
			ispn.Spec.Version = "13.0.9"
			expectInvalidErrStatus(k8sClient.Update(ctx, ispn), statusDetailCause{
				"FieldValueForbidden", "spec.version", "rollback only supported when a Hot Rolling Upgrade is in progress",
			})
		})

		It("Should allow rollback when HR rolling upgrade in progress", func() {
			ispn := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Service: InfinispanServiceSpec{
						Type: ServiceTypeDataGrid,
					},
					Replicas: 1,
					Version:  "13.0.10",
					Upgrades: &InfinispanUpgradesSpec{
						Type: UpgradeTypeHotRodRolling,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ispn)).Should(Succeed())
			ispn.Status.HotRodRollingUpgradeStatus = &HotRodRollingUpgradeStatus{
				Stage:         HotRodRollingStageStatefulSetReplace,
				SourceVersion: "13.0.9",
			}
			Expect(k8sClient.Status().Update(ctx, ispn)).Should(Succeed())
			ispn.Spec.Version = "13.0.9"
			Expect(k8sClient.Update(ctx, ispn)).Should(Succeed())
		})

		It("Should prevent HR rolling upgrade rollback when invalid source version defined", func() {
			ispn := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Service: InfinispanServiceSpec{
						Type: ServiceTypeDataGrid,
					},
					Replicas: 1,
					Version:  "13.0.10",
					Upgrades: &InfinispanUpgradesSpec{
						Type: UpgradeTypeHotRodRolling,
					},
				},
			}
			Expect(k8sClient.Create(ctx, ispn)).Should(Succeed())
			ispn.Status.HotRodRollingUpgradeStatus = &HotRodRollingUpgradeStatus{
				Stage:         HotRodRollingStageStatefulSetReplace,
				SourceVersion: "13.0.9",
			}
			Expect(k8sClient.Status().Update(ctx, ispn)).Should(Succeed())
			ispn.Spec.Version = "13.0.8"
			expectInvalidErrStatus(k8sClient.Update(ctx, ispn), statusDetailCause{
				"FieldValueForbidden", "spec.version", "Hot Rod Rolling Upgrades can only be rolled back to the original source version",
			})
		})

		It("Should prevent only allow correctly formatted versions", func() {
			ispn := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
					Version:  "should fail",
				},
			}
			expectInvalidErrStatus(k8sClient.Create(ctx, ispn), statusDetailCause{
				metav1.CauseTypeFieldValueInvalid, "spec.version", "should match",
			})
		})

		It("Should prevent immutable fields being updated", func() {
			ispn := &Infinispan{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: InfinispanSpec{
					Replicas: 1,
				},
			}
			Expect(k8sClient.Create(ctx, ispn)).Should(Succeed())
			Expect(k8sClient.Get(ctx, key, ispn)).Should(Succeed())
			ispn.Spec.Cryostat.Enabled = true
			expectInvalidErrStatus(k8sClient.Update(ctx, ispn),
				statusDetailCause{"FieldValueForbidden", "spec.cryostat", "Cryostat configuration is immutable and cannot be updated after initial Infinispan creation"},
			)
		})
	})
})

type statusDetailCause struct {
	Type          metav1.CauseType
	field         string
	messageSubStr string
}

func expectInvalidErrStatus(err error, causes ...statusDetailCause) {
	Expect(err).ShouldNot(BeNil())
	var statusError *k8serrors.StatusError
	Expect(errors.As(err, &statusError)).Should(BeTrue())

	errStatus := statusError.ErrStatus
	Expect(errStatus.Reason).Should(Equal(metav1.StatusReasonInvalid))

	Expect(errStatus.Details.Causes).Should(HaveLen(len(causes)))
	for i, c := range errStatus.Details.Causes {
		Expect(c.Type).Should(Equal(causes[i].Type))
		Expect(c.Field).Should(Equal(causes[i].field))
		Expect(c.Message).Should(ContainSubstring(causes[i].messageSubStr))
	}
}
