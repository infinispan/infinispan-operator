package caches

import (
	"encoding/json"
	"github.com/infinispan/infinispan-operator/controllers/constants"
)

type RemoteStoreConfig struct {
	RemoteStore *RemoteStore `json:"remote-store"`
}

type RemoteStore struct {
	ProtocolVersion string        `json:"protocol-version,omitempty"`
	RawValues       bool          `json:"raw-values"`
	Segmented       bool          `json:"segmented"`
	Shared          bool          `json:"shared"`
	Cache           string        `json:"cache,omitempty"`
	RemoteServer    *RemoteServer `json:"remote-server,omitempty"`
	Security        *Security     `json:"security,omitempty"`
}

type RemoteServer struct {
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
}

type Security struct {
	Authentication *Authentication `json:"authentication,omitempty"`
	Encryption     *Encryption     `json:"encryption,omitempty"`
}

type Authentication struct {
	ServerName string  `json:"server-name,omitempty"`
	Digest     *Digest `json:"digest"`
}

type Digest struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Realm    string `json:"realm,omitempty"`
}

type Encryption struct {
	Protocol    string      `json:"protocol,omitempty"`
	SniHostName string      `json:"sni-host-name,omitempty"`
	Keystore    *Keystore   `json:"keystore,omitempty"`
	TrustStore  *Truststore `json:"truststore,omitempty"`
}

type Keystore struct {
	Filename            string `json:"filename,omitempty"`
	Password            string `json:"password,omitempty"`
	CertificatePassword string `json:"certificate-password,omitempty"`
	KeyAlias            string `json:"key-alias,omitempty"`
	Type                string `json:"type,omitempty"`
}

type Truststore struct {
	Filename string `json:"filename,omitempty"`
	Password string `json:"password,omitempty"`
	Type     string `json:"type,omitempty"`
}

func CreateRemoteStoreConfig(ip string, cache, pass string) (string, error) {

	cfg := RemoteStoreConfig{
		RemoteStore: &RemoteStore{
			RawValues: true,
			Shared:    true,
			Cache:     cache,
			Segmented: false,
			RemoteServer: &RemoteServer{
				Host: ip,
				Port: constants.InfinispanAdminPort,
			},
			Security: &Security{
				Authentication: &Authentication{
					ServerName: "infinispan",
					Digest: &Digest{
						Username: constants.DefaultOperatorUser,
						Password: pass,
						Realm:    "admin",
					},
				},
			},
		},
	}

	doc, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(doc), nil
}
