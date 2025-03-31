package server

import (
	"os"
	"testing"

	"github.com/blang/semver"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/stretchr/testify/assert"
)

func TestGenerateAndGenerateZeroCapacity(t *testing.T) {
	baseCfg_expected := readFile("testdata/base-15-cfg.xml")
	adminCfg_expected := readFile("testdata/admin-15-cfg.xml")
	zeroCfg_expected := readFile("testdata/zero-15-cfg.xml")

	ispn := Infinispan{Authorization: &Authorization{Enabled: true}}
	spec := Spec{Infinispan: ispn}
	vers := semver.Version{Major: 15, Minor: 1, Patch: 25}
	ope := version.Operand{UpstreamVersion: &vers}

	baseCfg, adminCfg, err := Generate(ope, &spec)
	assert.Equal(t, baseCfg_expected, baseCfg)
	assert.Equal(t, adminCfg_expected, adminCfg)
	assert.Nil(t, err)

	zeroCfg, err := GenerateZeroCapacity(ope, &spec)
	assert.Equal(t, zeroCfg_expected, zeroCfg)
	assert.Nil(t, err)
}

func TestGenerateAndGenerateZeroCapacity_v14(t *testing.T) {
	baseCfg_expected := readFile("testdata/base-14-cfg.xml")
	adminCfg_expected := readFile("testdata/admin-14-cfg.xml")
	zeroCfg_expected := readFile("testdata/zero-14-cfg.xml")

	ispn := Infinispan{Authorization: &Authorization{Enabled: true}}
	spec := Spec{Infinispan: ispn}
	vers := semver.Version{Major: 14, Minor: 0, Patch: 11}
	ope := version.Operand{UpstreamVersion: &vers}

	baseCfg, adminCfg, err := Generate(ope, &spec)
	assert.Equal(t, baseCfg_expected, baseCfg)
	assert.Equal(t, adminCfg_expected, adminCfg)
	assert.Nil(t, err)

	zeroCfg, err := GenerateZeroCapacity(ope, &spec)
	assert.Equal(t, zeroCfg_expected, zeroCfg)
	assert.Nil(t, err)
}

func readFile(name string) (content string) {
	data, err := os.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(data)
}
