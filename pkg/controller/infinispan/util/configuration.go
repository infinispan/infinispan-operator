package util

import (
	"fmt"
)

// InfinispanConfiguration is the top level configuration type
type InfinispanConfiguration struct {
	ClusterName string `yaml:"clusterName"`
	JGroups     JGroups
	Keystore    Keystore
}

// Keystore configuration info for connector encryption
type Keystore struct {
	Path         string
	Password     string
	Alias        string
	CrtPath      string `yaml:"crtPath,omitempty"`
	SelfSignCert bool   `yaml:"selfSignCert"`
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

// CreateInfinispanConfiguration generates a server configuration
func CreateInfinispanConfiguration(name, namespace string) InfinispanConfiguration {
	query := fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace)
	jgroups := JGroups{Transport: "tcp", DNSPing: DNSPing{Query: query}}
	config := InfinispanConfiguration{ClusterName: name, JGroups: jgroups}
	return config
}
