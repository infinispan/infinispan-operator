package configuration

// InfinispanConfiguration is the top level configuration type
type InfinispanConfiguration struct {
	ClusterName string   `yaml:"clusterName"`
	JGroups     JGroups  `yaml:"jgroups"`
	Keystore    Keystore `yaml:"keystore"`
	XSite       XSite    `yaml:"xsite"`
	Logging     Logging  `yaml:"logging"`
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

type Logging struct {
	Categories map[string]string `yaml:"categories"`
}
