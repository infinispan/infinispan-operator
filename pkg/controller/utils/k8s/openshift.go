package k8s

import (
	"fmt"
	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ServiceCAsCRDResourceExists returns true if the platform
// has the servicecas.operator.openshift.io custom resource deployed
// Used to check if service CA operator is serving TLS certificates
func (k Kubernetes) HasServiceCAsCRDResource() bool {
	// Using an ad-hoc path
	req := k.RestClient.Get().AbsPath("apis/apiextensions.k8s.io/v1beta1/customresourcedefinitions/servicecas.operator.openshift.io")
	result := req.Do()
	var status int
	result.StatusCode(&status)
	return status >= 200 && status < 299
}

// GetServingCertsMode returns a label that identify the kind of serving
// certs service is available. Returns 'openshift.io' for service-ca on openshift
func (k Kubernetes) GetServingCertsMode() string {
	if k.HasServiceCAsCRDResource() {
		return "openshift.io"

		// Code to check if other modes of serving TLS cert service is available
		// can be added here
	}
	return ""
}

func (k Kubernetes) GetOpenShiftRESTConfig(masterURL string, secretName string, infinispan *infinispanv1.Infinispan, logger logr.Logger) (*restclient.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags(masterURL, "")
	if err != nil {
		logger.Error(err, "unable to create REST configuration", "master URL", masterURL)
		return nil, err
	}

	// Skip-tls for accessing other OpenShift clusters
	config.Insecure = true

	secret, err := k.GetSecret(secretName, infinispan.ObjectMeta.Namespace)
	if err != nil {
		logger.Error(err, "unable to find Secret", "secret name", secretName)
		return nil, err
	}

	if token, ok := secret.Data["token"]; ok {
		config.BearerToken = string(token)
		return config, nil
	}

	return nil, fmt.Errorf("token required connect to OpenShift cluster")
}
