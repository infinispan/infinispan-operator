package constants

import "os"

const (
	// OperandImageOpenJDK envvar name for OpenJDK operand image
	OperandImageOpenJDK = "RELATED_IMAGE_OPENJDK"

	// DefaultOperandImageOpenJDK default image for OpenJDK stack
	DefaultOperandImageOpenJDK = "quay.io/infinispan/server:13.0"
)

// GetDefaultInfinispanJavaImage returns default Infinispan Java image
func GetDefaultInfinispanJavaImage() string {
	if retVal := os.Getenv(OperandImageOpenJDK); retVal != "" {
		return retVal
	}
	// If RELATED_IMAGE_OPENJDK is not defined fallback on old name for backward comp
	return GetEnvWithDefault("DEFAULT_IMAGE", DefaultOperandImageOpenJDK)
}
