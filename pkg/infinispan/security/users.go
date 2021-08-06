package security

import (
	"crypto/rand"
	"errors"
	"math/big"

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
	Roles    []string
}

// TODO certain characters having issues, so reduce sample for now
// var acceptedChars = []byte(",-./=@\\abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var acceptedChars = []byte("123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
var alphaChars = acceptedChars[8:]

// getRandomStringForAuth generate a random string that can be used as a
// user or pass for Infinispan
func getRandomStringForAuth(size int) (string, error) {
	b := make([]byte, size)
	char, err := getRandomChar(alphaChars)
	if err != nil {
		return "", err
	}
	b[0] = char
	for i := 1; i < len(b); i++ {
		char, err := getRandomChar(alphaChars)
		if err != nil {
			return "", err
		}
		b[i] = char
	}
	return string(b), nil
}

func getRandomChar(availableChars []byte) (byte, error) {
	max := big.NewInt(int64(len(alphaChars)))
	r, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, err
	}
	return availableChars[r.Int64()], nil
}

// CreateIdentitiesFor creates identities for a given username/password combination
func CreateIdentitiesFor(usr string, pass string) ([]byte, error) {
	identities := Identities{
		Credentials: []Credentials{{
			Username: usr,
			Password: pass,
			Roles:    []string{"admin"},
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
	pass, err := getRandomStringForAuth(16)
	if err != nil {
		return nil, err
	}
	return CreateIdentitiesFor(consts.DefaultOperatorUser, pass)
}

// GetUserCredentials get identities credentials in yaml format
func GetUserCredentials() ([]byte, error) {
	pass, err := getRandomStringForAuth(16)
	if err != nil {
		return nil, err
	}
	return CreateIdentitiesFor(consts.DefaultDeveloperUser, pass)
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
