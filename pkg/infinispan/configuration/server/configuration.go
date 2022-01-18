package server

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	rice "github.com/GeertJohan/go.rice"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"gopkg.in/yaml.v2"
)

// InfinispanConfiguration is the top level configuration type
type InfinispanConfiguration struct {
	Infinispan  Infinispan   `yaml:"infinispan"`
	JGroups     JGroups      `yaml:"jgroups"`
	Keystore    Keystore     `yaml:"keystore,omitempty"`
	Truststore  Truststore   `yaml:"truststore,omitempty"`
	XSite       *XSite       `yaml:"xsite,omitempty"`
	Logging     Logging      `yaml:"logging,omitempty"`
	Endpoints   Endpoints    `yaml:"endpoints"`
	CloudEvents *CloudEvents `yaml:"cloudEvents,omitempty"`
	Transport   Transport    `yaml:"transport,omitempty"`
}

// Transport configures the TLS for JGroups (Cross Site Replication)
// Reusing the Keystore & Truststore struct although we don't need all the fields there.
type Transport struct {
	TLS TransportTLS `yaml:"tls,omitempty"`
}

type TransportTLS struct {
	Enabled    bool       `yaml:"enabled"`
	KeyStore   Keystore   `yaml:"keystore,omitempty"`
	TrustStore Truststore `yaml:"truststore,omitempty"`
}

type CloudEvents struct {
	BootstrapServers  string `yaml:"bootstrapServers"`
	Acks              string `yaml:"acks"`
	CacheEntriesTopic string `yaml:"cacheEntriesTopic"`
}

type Infinispan struct {
	Authorization    Authorization `yaml:"authorization,omitempty"`
	ClusterName      string        `yaml:"clusterName"`
	ZeroCapacityNode bool          `yaml:"zeroCapacityNode"`
	Locks            Locks         `yaml:"locks"`
}

type Authorization struct {
	Enabled    bool                `yaml:"enabled"`
	RoleMapper string              `yaml:"roleMapper,omitempty"`
	Roles      []AuthorizationRole `yaml:"roles,omitempty"`
}

type AuthorizationRole struct {
	Name        string   `yaml:"name"`
	Permissions []string `yaml:"permissions"`
}

type Endpoint struct {
	Enabled    bool   `yaml:"enabled,omitempty"`
	Qop        string `yaml:"qop,omitempty"`
	ServerName string `yaml:"serverName,omitempty"`
}

type Endpoints struct {
	Enabled        bool     `yaml:"enabled,omitempty"`
	Cors           bool     `yaml:"cors,omitempty"` // TODO: cors not implemented
	Authenticate   bool     `yaml:"auth"`
	DedicatedAdmin bool     `yaml:"dedicatedAdmin"`
	ClientCert     string   `yaml:"clientCert,omitempty"`
	Hotrod         Endpoint `yaml:"hotrod,omitempty"`
}

type Locks struct {
	Owners      int32  `yaml:"owners,omitempty"`
	Reliability string `yaml:"reliability,omitempty"`
}

// Keystore configuration info for endpoint encryption
type Keystore struct {
	Path         string
	Password     string
	Alias        string
	CrtPath      string `yaml:"crtPath,omitempty"`
	SelfSignCert string `yaml:"selfSignCert,omitempty"`
	Type         string `yaml:"type,omitempty"`
}

// Truststore configuration info for endpoint encryption
type Truststore struct {
	CaFile   string `yaml:"cafile,omitempty"`
	Certs    string `yaml:"certs,omitempty"`
	Path     string `yaml:"path,omitempty"`
	Password string
}

// JGroups configures clustering layer
type JGroups struct {
	Transport   string  `yaml:"transport"`
	DNSPing     DNSPing `yaml:"dnsPing"`
	Diagnostics bool    `yaml:"diagnostics"`
	BindPort    int32   `yaml:"bindPort"`
	FastMerge   bool    `yaml:"fastMerge"`
}

type Relay struct {
	BindPort int32 `yaml:"bindPort"`
}

// DNSPing configures DNS cluster lookup settings
type DNSPing struct {
	Query      string `yaml:"query"`
	Address    string `yaml:"address"`
	RecordType string `yaml:"recordType"`
}

type XSite struct {
	Transport          string       `yaml:"transport"`
	MaxRelayNodes      int32        `yaml:"maxRelayNodes"`
	Sites              []BackupSite `yaml:"sites"` // first backup site is the local site
	RelayNodeCandidate bool         `yaml:"relayNodeCandidate"`
	Relay              Relay        `yaml:"relay"`
}

type BackupSite struct {
	Address string `yaml:"address"`
	Name    string `yaml:"name"`
	Port    int32  `yaml:"port"`
}

type Logging struct {
	Console    Console           `yaml:"console"`
	File       File              `yaml:"file"`
	Categories map[string]string `yaml:"categories,omitempty"`
}

type Console struct {
	Level   string `yaml:"level"`
	Pattern string `yaml:"pattern"`
}

type File struct {
	Level   string `yaml:"level"`
	Pattern string `yaml:"pattern"`
	Path    string `yaml:"path"`
}

func (c *InfinispanConfiguration) Yaml() (string, error) {
	y, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(y), nil
}

func FromYaml(src string) (*InfinispanConfiguration, error) {
	config := &InfinispanConfiguration{}
	if err := yaml.Unmarshal([]byte(src), config); err != nil {
		return nil, err
	}
	return config, nil
}

func (serverConf *InfinispanConfiguration) Xml() (infinispan string, logging string, err error) {
	// Setup go template to process infinispan.xml
	funcMap := template.FuncMap{
		"UpperCase":    strings.ToUpper,
		"LowerCase":    strings.ToLower,
		"ServerRoot":   func() string { return consts.ServerRoot },
		"ListAsString": func(elems []string) string { return strings.Join(elems, ",") },
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
	var ispnXmlTemplate, loggingXmlTemplate string
	if box, err := rice.FindBox("resources"); err != nil {
		return "", "", err
	} else {
		if ispnXmlTemplate, err = box.String("infinispan.xmltmpl"); err != nil {
			return "", "", err
		}
		if loggingXmlTemplate, err = box.String("log4j.xmltmpl"); err != nil {
			return "", "", err
		}
	}

	tIspn, err := template.New("infinispan.xml").Funcs(funcMap).Parse(ispnXmlTemplate)
	if err != nil {
		return "", "", err
	}
	buffIspn := new(bytes.Buffer)
	err = tIspn.Execute(buffIspn, serverConf)
	if err != nil {
		return "", "", err
	}

	tLog4j, err := template.New("log4j.xml").Funcs(funcMap).Parse(loggingXmlTemplate)
	if err != nil {
		return "", "", err
	}
	buffLog4j := new(bytes.Buffer)
	err = tLog4j.Execute(buffLog4j, serverConf)
	if err != nil {
		return "", "", err
	}

	return buffIspn.String(), buffLog4j.String(), nil
}
