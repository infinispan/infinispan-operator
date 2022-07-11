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
	Authorization *Authorization
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
	Address string
	Name    string
	Port    int32
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
func Generate(v *version.Version, spec *Spec) (baseCfg string, admingCfg string, err error) {
	if v == nil {
		v = &version.Version{Major: 13, Minor: 0, Patch: 0}
	}
	switch v.Major {
	case 13:
		if baseCfg, err = templates.LoadAndExecute("infinispan-base-13.xml", funcMap(), spec); err != nil {
			return
		}

		admingCfg, err = templates.LoadAndExecute("infinispan-admin-13.xml", funcMap(), spec)
		return
	default:
		return "", "", version.UnknownError(v)
	}
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"RemoteSites": func(elems []BackupSite) string {
			var ret string
			for i, bs := range elems {
				ret += fmt.Sprintf("%s[%d]", bs.Address, bs.Port)
				if i < len(elems)-1 {
					ret += ","
				}
			}
			return ret
		},
	}
}
