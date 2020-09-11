package security

import (
	"errors"
	"math/rand"

	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"gopkg.in/yaml.v2"
)

// Identities represent identities that can interact with server
type Identities struct {
	Credentials []Credentials
}

// Credentials represent individual username/password combinations
type Credentials struct {
	Username string
	Password string
}

// CreateIdentities generates default identities
func createIdentities() Identities {
	developer := Credentials{Username: consts.DefaultDeveloperUser, Password: getRandomStringForAuth(16)}
	operator := Credentials{Username: consts.DefaultOperatorUser, Password: getRandomStringForAuth(16)}
	identities := Identities{Credentials: []Credentials{developer, operator}}
	return identities
}

// TODO certain characters having issues, so reduce sample for now
// var acceptedChars = []byte(",-./=@\\abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var acceptedChars = []byte("@123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var alphaChars = acceptedChars[8:]

// getRandomStringForAuth generate a random string that can be used as a
// user or pass for Infinispan
func getRandomStringForAuth(size int) string {
	b := make([]byte, size)
	for i := range b {
		if i == 0 {
			b[0] = alphaChars[rand.Intn(len(alphaChars))]
		} else {
			b[i] = acceptedChars[rand.Intn(len(acceptedChars))]
		}
	}
	return string(b)
}

// CreateIdentitiesFor creates identities for a given username/password combination
func CreateIdentitiesFor(usr string, pass string) Identities {
	// Adding required "operator" user
	operator := Credentials{Username: consts.DefaultOperatorUser, Password: getRandomStringForAuth(16)}
	credentials := Credentials{Username: usr, Password: pass}
	identities := Identities{Credentials: []Credentials{credentials, operator}}
	return identities
}

// GetCredentials get identities credentials in yaml format
func GetCredentials() ([]byte, error) {
	identities := createIdentities()
	data, err := yaml.Marshal(identities)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// FindPassword finds a user's password
func FindPassword(usr string, descriptor []byte) (string, error) {
	var identities Identities
	err := yaml.Unmarshal(descriptor, &identities)
	if err != nil {
		return "", err
	}

	for _, v := range identities.Credentials {
		if v.Username == usr {
			return v.Password, nil
		}
	}

	return "", errors.New("no operator credentials found")
}

// PasswordFromSecret returns password associated with a user in a given secret
func PasswordFromSecret(user, secretName, namespace string, k *kube.Kubernetes) (string, error) {
	secret, err := k.GetSecret(secretName, namespace)
	if err != nil {
		return "", nil
	}

	descriptor := secret.Data["identities.yaml"]
	pass, err := FindPassword(user, descriptor)
	if err != nil {
		return "", err
	}

	return pass, nil
}
