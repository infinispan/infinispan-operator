package v2alpha1

import "fmt"

func (b *Batch) PropertiesSecretName() string {
	return fmt.Sprintf("%s-batch-properties", b.Name)
}
