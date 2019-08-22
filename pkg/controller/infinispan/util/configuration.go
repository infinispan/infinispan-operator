package util

import (
	"fmt"
	"gopkg.in/yaml.v2"
)

type Infinispan struct {
	ClusterName string `yaml:"clusterName"`
	JGroups     JGroups
}

type JGroups struct {
	Transport string
	DnsPing   DnsPing `yaml:"dnsPing"`
}

type DnsPing struct {
	Query string
}

func InfinispanConfiguration(name, namespace string) (string, error) {
	query := fmt.Sprintf("%s-ping.%s.svc.cluster.local", name, namespace)
	jgroups := JGroups{Transport: "tcp", DnsPing: DnsPing{Query: query}}
	config := Infinispan{ClusterName: name, JGroups: jgroups}
	serialized, err := yaml.Marshal(&config)
	if err != nil {
		return "", err
	}

	return string(serialized), nil
}
