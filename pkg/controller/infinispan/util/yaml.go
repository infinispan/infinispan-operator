package util

import (
	"gopkg.in/yaml.v2"
)

// ToYaml converts a type instance into serialized yaml
func ToYaml(in interface{}) ([]byte, error) {
	return yaml.Marshal(in)
}

// FromYaml converts serialized yaml into type instance
func FromYaml(in []byte, out interface{}) error {
	return yaml.Unmarshal(in, out)
}
