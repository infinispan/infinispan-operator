package configuration

import (
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

type Endpoints struct {
	Authenticate   bool   `yaml:"auth"`
	DedicatedAdmin bool   `yaml:"dedicatedAdmin"`
	ClientCert     string `yaml:"clientCert,omitempty"`
}

type Locks struct {
	Owners      int32  `yaml:"owners,omitempty"`
	Reliability string `yaml:"reliability,omitempty"`
}

// Keystore configuration info for endpoint encryption
type Keystore struct {
	Path     string
	Password string
	Alias    string
	CrtPath  string `yaml:"crtPath,omitempty"`
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
	Transport   string
	DNSPing     DNSPing `yaml:"dnsPing"`
	Diagnostics bool    `yaml:"diagnostics"`
}

// DNSPing configures DNS cluster lookup settings
type DNSPing struct {
	Query string
}

type XSite struct {
	Address        string       `yaml:"address"`
	Name           string       `yaml:"name"`
	Port           int32        `yaml:"port"`
	Transport      string       `yaml:"transport"`
	MaxSiteMasters int32        `yaml:"maxSiteMasters"`
	Backups        []BackupSite `yaml:"backups"`
}

type BackupSite struct {
	Address string `yaml:"address"`
	Name    string `yaml:"name"`
	Port    int32  `yaml:"port"`
}

type Logging struct {
	Categories map[string]string `yaml:"categories,omitempty"`
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
