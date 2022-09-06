package server

import (
	"fmt"
	"text/template"

	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/templates"
)

type Spec struct {
	ClusterName     string
	Namespace       string
	StatefulSetName string
	FIPS            bool
	Infinispan      Infinispan
	JGroups         JGroups
	CloudEvents     *CloudEvents
	Endpoints       Endpoints
	Keystore        Keystore
	Transport       Transport
	Truststore      Truststore
	XSite           *XSite
}

type Infinispan struct {
	Authorization    *Authorization
	ZeroCapacityNode bool
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
	MaxRelayNodes int32
	Sites         []BackupSite
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
		return "", "", version.UnknownError(v)
	}

	baseTemplate := fmt.Sprintf("infinispan-base-%d.xml", v.Major)
	adminTemplate := fmt.Sprintf("infinispan-admin-%d.xml", v.Major)

	if baseCfg, err = templates.LoadAndExecute(baseTemplate, funcMap(), spec); err != nil {
		return
	}

	admingCfg, err = templates.LoadAndExecute(adminTemplate, funcMap(), spec)
	return
}

func GenerateZeroCapacity(operand version.Operand, spec *Spec) (string, error) {
	v := operand.UpstreamVersion
	if !supportedMajorVersion(v.Major) {
		return "", version.UnknownError(v)
	}

	zeroTemplate := fmt.Sprintf("infinispan-zero-%d.xml", v.Major)
	return templates.LoadAndExecute(zeroTemplate, funcMap(), spec)
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"RemoteSites": func(elems []BackupSite) string {
			var ret string
			first := true
			for _, bs := range elems {
				if bs.IgnoreGossipRouter {
					continue
				}
				if !first {
					ret += ","
				} else {
					first = false
				}
				ret += fmt.Sprintf("%s[%d]", bs.Address, bs.Port)
			}
			return ret
		},
	}
}

func supportedMajorVersion(v uint64) bool {
	return v == 13 || v == 14
}
