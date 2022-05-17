package version_test

import (
	"fmt"
	"os"

	"github.com/blang/semver"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("VersionManager", func() {
	It("should successfully load upstream operands from JSON", func() {
		json := `
			[{
				"upstream-version": "13.0.8",
				"image": "quay.io/infinispan/server:13.0.8.Final",
				"deprecated": true
			}, {
				"upstream-version": "13.0.9",
				"image": "quay.io/infinispan/server:13.0.9.Final",
				"cve": true
			}, {
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}]`

		m, err := version.ManagerFromJson(json)
		Expect(err).Should(BeNil())
		Expect(m.Latest().UpstreamVersion).Should(Equal(&semver.Version{
			Major: 13,
			Minor: 0,
			Patch: 10,
		}))

		operand, err := m.WithRef("13.0.8")
		Expect(err).Should(BeNil())
		Expect(operand.Ref()).Should(Equal("13.0.8"))
		Expect(operand.UpstreamVersion).Should(Equal(&semver.Version{
			Major: 13,
			Minor: 0,
			Patch: 8,
		}))
		Expect(operand.DownstreamVersion).Should(BeNil())
		Expect(operand.Deprecated).Should(BeTrue())
		Expect(operand.CVE).Should(BeFalse())

		operand, err = m.WithRef("13.0.9")
		Expect(err).Should(BeNil())
		Expect(operand.Ref()).Should(Equal("13.0.9"))
		Expect(operand.UpstreamVersion).Should(Equal(&semver.Version{
			Major: 13,
			Minor: 0,
			Patch: 9,
		}))
		Expect(operand.DownstreamVersion).Should(BeNil())
		Expect(operand.Deprecated).Should(BeFalse())
		Expect(operand.CVE).Should(BeTrue())

		operand, err = m.WithRef("13.0.10")
		Expect(err).Should(BeNil())
		Expect(operand.Ref()).Should(Equal("13.0.10"))
		Expect(operand.UpstreamVersion).Should(Equal(&semver.Version{
			Major: 13,
			Minor: 0,
			Patch: 10,
		}))
		Expect(operand.DownstreamVersion).Should(BeNil())
		Expect(operand.Deprecated).Should(BeFalse())
		Expect(operand.CVE).Should(BeFalse())
	})

	It("should successfully load downstream operands from JSON", func() {
		json := `
			[{
				"downstream-version": "8.3.1",
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}]`
		m, err := version.ManagerFromJson(json)
		Expect(err).Should(BeNil())

		operand, err := m.WithRef("8.3.1")
		Expect(err).Should(BeNil())
		Expect(operand.Ref()).Should(Equal("8.3.1"))
		Expect(operand.UpstreamVersion).Should(Equal(&semver.Version{
			Major: 13,
			Minor: 0,
			Patch: 10,
		}))
		Expect(operand.DownstreamVersion).Should(Equal(&semver.Version{
			Major: 8,
			Minor: 3,
			Patch: 1,
		}))
	})

	It("should fail on duplicate downstream version ref in JSON", func() {
		json := `
			[{
				"downstream-version": "8.3.1",
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}, {
				"downstream-version": "8.3.1",
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}]`
		_, err := version.ManagerFromJson(json)
		Expect(err).Should(Equal(fmt.Errorf("multiple operands have the same version reference '8.3.1'")))
	})

	It("should fail on duplicate upstream version ref in JSON", func() {
		json := `
			[{
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}, {
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}]`
		_, err := version.ManagerFromJson(json)
		Expect(err).Should(Equal(fmt.Errorf("multiple operands have the same version reference '13.0.10'")))
	})

	It("should allow duplicate upstream-version when downstream-version set in JSON", func() {
		json := `
			[{
				"downstream-version": "8.3.0",
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}, {
				"downstream-version": "8.3.1",
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}]`
		_, err := version.ManagerFromJson(json)
		Expect(err).Should(BeNil())
	})

	It("should fail if upstream-version not specified", func() {
		json := `
			[{
				"downstream-version": "8.3.1",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}]`
		_, err := version.ManagerFromJson(json)
		Expect(err).Should(Equal(fmt.Errorf("upstream-version field must be specified")))
	})

	It("should fail if image not specified", func() {
		json := `
			[{
				"upstream-version": "13.0.10"
			}]`
		_, err := version.ManagerFromJson(json)
		Expect(err).Should(Equal(fmt.Errorf("image field must be specified")))
	})

	It("should load json from env var", func() {
		json := `
			[{
				"upstream-version": "13.0.10",
				"image": "quay.io/infinispan/server:13.0.10.Final"
			}]`
		envName := "test-env"
		defer func() {
			_ = os.Unsetenv(envName)
		}()
		Expect(os.Setenv(envName, json)).Should(BeNil())
		m, err := version.ManagerFromEnv(envName)

		Expect(err).Should(BeNil())
		Expect(m.Latest().UpstreamVersion).Should(Equal(&semver.Version{
			Major: 13,
			Minor: 0,
			Patch: 10,
		}))
	})
})
