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

// TODO certain characters having issues, so reduce sample for now
// var acceptedChars = []byte(",-./=@\\abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var acceptedChars = []byte("123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
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
func CreateIdentitiesFor(usr string, pass string) ([]byte, error) {
	identities := Identities{
		Credentials: []Credentials{{
			Username: usr,
			Password: pass,
		}},
	}
	data, err := yaml.Marshal(identities)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// GetAdminCredentials get admin identities credentials in yaml format
func GetAdminCredentials() ([]byte, error) {
	return CreateIdentitiesFor(consts.DefaultOperatorUser, getRandomStringForAuth(16))
}

// GetUserCredentials get identities credentials in yaml format
func GetUserCredentials() ([]byte, error) {
	return CreateIdentitiesFor(consts.DefaultDeveloperUser, getRandomStringForAuth(16))
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

// passwordFromSecret returns password associated with a user in a given secret
func passwordFromSecret(user, secretName, namespace string, k *kube.Kubernetes) (string, error) {
	secret, err := k.GetSecret(secretName, namespace)
	if err != nil {
		return "", nil
	}

	descriptor := secret.Data[consts.ServerIdentitiesFilename]
	pass, err := FindPassword(user, descriptor)
	if err != nil {
		return "", err
	}
	return pass, nil
}

func UserPassword(user, secretName, namespace string, k *kube.Kubernetes) (string, error) {
	return passwordFromSecret(user, secretName, namespace, k)
}

func AdminPassword(secretName, namespace string, k *kube.Kubernetes) (string, error) {
	return passwordFromSecret(consts.DefaultOperatorUser, secretName, namespace, k)
}
