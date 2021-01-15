package configuration

import (
	"fmt"

	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"gopkg.in/yaml.v2"
)

// InfinispanConfiguration is the top level configuration type
type InfinispanConfiguration struct {
	Infinispan Infinispan `yaml:"infinispan"`
	JGroups    JGroups    `yaml:"jgroups"`
	Keystore   Keystore   `yaml:"keystore,omitempty"`
	XSite      *XSite     `yaml:"xsite,omitempty"`
	Logging    Logging    `yaml:"logging,omitempty"`
}

type Infinispan struct {
	ClusterName      string `yaml:"clusterName"`
	ZeroCapacityNode bool   `yaml:"zeroCapacityNode"`
	Locks            Locks  `yaml:"locks"`
}

type Locks struct {
	Owners      int32  `yaml:"owners,omitempty"`
	Reliability string `yaml:"reliability,omitempty"`
}

// Keystore configuration info for connector encryption
type Keystore struct {
	Path     string
	Password string
	Alias    string
	CrtPath  string `yaml:"crtPath,omitempty"`
}

// JGroups configures clustering layer
type JGroups struct {
	Transport   string
	DNSPing     DNSPing `yaml:"dnsPing"`
	Diagnostics bool    `yaml:"diagnostics"`
}

// DNSPing configures DNS cluster lookup settings
type DNSPing struct {
	Query string
}

type XSite struct {
	Address string       `yaml:"address"`
	Name    string       `yaml:"name"`
	Port    int32        `yaml:"port"`
	Backups []BackupSite `yaml:"backups"`
}

type BackupSite struct {
	Address string `yaml:"address"`
	Name    string `yaml:"name"`
	Port    int32  `yaml:"port"`
}

type Logging struct {
	Categories map[string]string `yaml:"categories"`
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

// CreateInfinispanConfiguration generates a server configuration
func CreateInfinispanConfiguration(name, namespace string, loggingCategories map[string]string, xsite *XSite) InfinispanConfiguration {
	query := fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace)
	jgroups := JGroups{Transport: "tcp", DNSPing: DNSPing{Query: query}}

	config := InfinispanConfiguration{
		Infinispan: Infinispan{
			ClusterName: name,
		},
		JGroups: jgroups,
	}
	if consts.JGroupsDiagnosticsFlag == "TRUE" {
		config.JGroups.Diagnostics = true
	}
	if len(loggingCategories) > 0 {
		config.Logging = Logging{
			Categories: loggingCategories,
		}
	}

	if xsite != nil {
		config.XSite = xsite
	}

	return config
}
