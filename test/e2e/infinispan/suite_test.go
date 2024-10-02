package infinispan

import (
	"testing"

	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
)

// Executes tests that have potential to find regression in new OpenShift versions
func TestOpenShiftIntegration(t *testing.T) {
	// Prevent execution unless explicitly configured
	if !tutils.SuiteMode {
		t.SkipNow()
	}

	// Smoke tests
	t.Run("TestBaseFunctionality", TestBaseFunctionality)
	t.Run("TestCustomLoggingPattern", TestCustomLoggingPattern)
	t.Run("TestExplicitCredentials", TestExplicitCredentials)
	t.Run("TestAuthorizationWithCustomRoles", TestAuthorizationWithCustomRoles)

	// Init container
	t.Run("TestExternalDependenciesHttp", TestExternalDependenciesHttp)

	// Scaling/Updates - cooperation of Operator and StatefulSets
	t.Run("TestGracefulShutdownWithTwoReplicas", TestGracefulShutdownWithTwoReplicas)
	t.Run("TestUserCustomConfigUpdateOnChange", TestUserCustomConfigUpdateOnChange)
	t.Run("TestContainerCPUUpdateWithTwoReplicas", TestContainerCPUUpdateWithTwoReplicas)

	// Validate that request with tls certificates are correctly passing through HaProxy
	t.Run("TestClientCertValidateWithAuthorization", TestClientCertValidateWithAuthorization)
	t.Run("TestClientCertAuthenticateWithAuthorization", TestClientCertAuthenticateWithAuthorization)
	t.Run("TestClientCertGeneratedTruststoreAuthenticate", TestClientCertGeneratedTruststoreAuthenticate)
	t.Run("TestClientCertGeneratedTruststoreValidate", TestClientCertGeneratedTruststoreValidate)
}
