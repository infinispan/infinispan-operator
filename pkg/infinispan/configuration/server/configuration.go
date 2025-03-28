package server

import (
	"fmt"

	"github.com/blang/semver"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/templates"
)

type Spec struct {
	ClusterName         string
	Namespace           string
	StatefulSetName     string
	Infinispan          Infinispan
	JGroups             JGroups
	CloudEvents         *CloudEvents
	Endpoints           Endpoints
	Keystore            Keystore
	Transport           Transport
	Truststore          Truststore
	UserCredentialStore bool
	XSite               *XSite
}

type Infinispan struct {
	Authorization    *Authorization
	ZeroCapacityNode bool
	Version          semver.Version
}

type Authorization struct {
	Enabled    bool
	RoleMapper string
	Roles      []AuthorizationRole
}

type AuthorizationRole struct {
	Name        string
	Permissions string
}

type XSite struct {
	MaxRelayNodes     int32
	Sites             []BackupSite
	HeartbeatEnabled  bool
	HeartbeatInterval int64
	HeartbeatTimeout  int64
}

type BackupSite struct {
	Address            string
	Name               string
	Port               int32
	IgnoreGossipRouter bool
}

type JGroups struct {
	Diagnostics bool
	FastMerge   bool
	Version     semver.Version
}

type CloudEvents struct {
	Acks              string
	BootstrapServers  string
	CacheEntriesTopic string
}

type Keystore struct {
	Path     string
	Password string
	Alias    string
	CrtPath  string
}

type Truststore struct {
	Authenticate bool
	CaFile       string
	Certs        string
	Path         string
	Password     string
}

type Transport struct {
	TLS TransportTLS
}

type TransportTLS struct {
	Enabled    bool
	KeyStore   Keystore
	TrustStore Truststore
}

type Endpoints struct {
	Authenticate bool
	ClientCert   string
}

// Generate the base and admin configuration files used by the Infinispan server
func Generate(operand version.Operand, spec *Spec) (baseCfg string, admingCfg string, err error) {
	v := operand.UpstreamVersion
	if !supportedMajorVersion(v.Major) {
		return "", "", version.NewUnknownError(v)
	}

	mapVersionsToSpec(operand, spec)

	baseTemplate := fmt.Sprintf("infinispan-base-%d.xml", v.Major)
	if baseCfg, err = templates.LoadAndExecute(baseTemplate, spec); err != nil {
		return
	}

	admingCfg, err = templates.LoadAndExecute("infinispan-admin.xml", spec)
	return
}

func GenerateZeroCapacity(operand version.Operand, spec *Spec) (string, error) {
	v := operand.UpstreamVersion
	if !supportedMajorVersion(v.Major) {
		return "", version.NewUnknownError(v)
	}

	mapVersionsToSpec(operand, spec)

	return templates.LoadAndExecute("infinispan-zero.xml", spec)
}

func supportedMajorVersion(v uint64) bool {
	return v >= 14 && v <= 15
}

func mapVersionsToSpec(operand version.Operand, spec *Spec) {
	// TODO: Make this declarative https://github.com/infinispan/infinispan-operator/issues/2240
	v := *operand.UpstreamVersion
	spec.Infinispan.Version = v
	spec.JGroups.Version.Major = 5

	if v.Major <= 14 {
		spec.JGroups.Version.Minor = 4
	} else if v.Minor <= 1 {
		spec.JGroups.Version.Minor = 5
	} else {
		spec.JGroups.Version.Minor = 6
	}
}
