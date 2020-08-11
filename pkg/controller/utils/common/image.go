package common

import "runtime"

const (
	// OperandImageOpenJDK envvar name for OpenJDK operand image
	OperandImageOpenJDK = "RELATED_IMAGE_OPENJDK"
	// OperandImageOpenJ9 envvar name for OpenJ9 operand image
	OperandImageOpenJ9 = "RELATED_IMAGE_OPENJ9"

	// DefaultOperandImageOpenJDK default image for OpenJDK stack
	DefaultOperandImageOpenJDK = "infinispan/server:latest"
	// DefaultOperandImageOpenJ9 default image for OpenJ9 stack
	DefaultOperandImageOpenJ9 = "infinispan-for-openj9/server:latest"
)

// GetDefaultInfinispanJavaImage returns default Infinispan Java image depends of the runtime architecture
func GetDefaultInfinispanJavaImage() string {
	// Full list of archs might be found here:
	// https://github.com/golang/go/blob/release-branch.go1.10/src/go/build/syslist.go#L8
	switch arch := runtime.GOARCH; arch {
	case "ppc64", "ppc64le", "s390x", "s390":
		return GetEnvWithDefault(OperandImageOpenJ9, DefaultOperandImageOpenJ9)
	default:
		// Try new name RELATED_IMAGE_OPENJDK first
		retVal := GetEnvWithDefault(OperandImageOpenJDK, "")
		if retVal != "" {
			return retVal
		}
		// If RELATED_IMAGE_OPENJDK is not defined fallback on old name for backward comp
		return GetEnvWithDefault("DEFAULT_IMAGE", DefaultOperandImageOpenJDK)
	}
}
