package util

import (
	"fmt"
	"gopkg.in/yaml.v2"
)

// Infinispan is the top level configuration type
type Infinispan struct {
	ClusterName string `yaml:"clusterName"`
	JGroups     JGroups
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

// InfinispanConfiguration generates a server configuration
func InfinispanConfiguration(name, namespace string) (string, error) {
	query := fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace)
	jgroups := JGroups{Transport: "tcp", DNSPing: DNSPing{Query: query}}
	config := Infinispan{ClusterName: name, JGroups: jgroups}
	serialized, err := yaml.Marshal(&config)
	if err != nil {
		return "", err
	}

	return string(serialized), nil
}
