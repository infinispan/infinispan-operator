package util

import (
	"fmt"
)

// InfinispanConfiguration is the top level configuration type
type InfinispanConfiguration struct {
	ClusterName string  `yaml:"clusterName"`
	JGroups     JGroups `yaml:"jgroups"`
	Keystore    Keystore
	XSite       XSite `yaml:"xsite"`
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
	Transport string
	DNSPing   DNSPing `yaml:"dnsPing"`
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

// CreateInfinispanConfiguration generates a server configuration
func CreateInfinispanConfiguration(name string, xsite *XSite, namespace string) InfinispanConfiguration {
	query := fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace)
	jgroups := JGroups{Transport: "tcp", DNSPing: DNSPing{Query: query}}
	config := InfinispanConfiguration{
		ClusterName: name,
		JGroups:     jgroups}

	if xsite != nil {
		config.XSite = *xsite
	}

	return config
}
