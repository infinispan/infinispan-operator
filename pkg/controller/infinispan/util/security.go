package util

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"gopkg.in/yaml.v2"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

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
func CreateIdentities() (string, error) {
	developer := Credentials{Username: "developer", Password: getRandomStringForAuth(16)}
	operator := Credentials{Username: "operator", Password: getRandomStringForAuth(16)}
	identities := Identities{Credentials: []Credentials{developer, operator}}
	serialized, err := yaml.Marshal(&identities)
	if err != nil {
		return "", err
	}

	return string(serialized), nil
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
func CreateIdentitiesFor(usr string, pass string) (string, error) {
	credentials := Credentials{Username: usr, Password: pass}
	identities := Identities{Credentials: []Credentials{credentials}}
	serialized, err := yaml.Marshal(&identities)
	if err != nil {
		return "", err
	}

	return string(serialized), nil
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

// GetSecretName returns the secret name associated with a server
func GetSecretName(name string) string {
	return fmt.Sprintf("%v-generated-secret", name)
}
