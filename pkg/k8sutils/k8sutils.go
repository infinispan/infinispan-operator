package k8sutils

import (
	"fmt"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func GetK8sClient(config *rest.Config) (kubernetes.Interface, error) {
	kubeclient, err := kubernetes.NewForConfig(config)

	if err != nil {
		return nil, fmt.Errorf("failed to build the kubeclient: %v", err)
	}

	return kubeclient, nil
}
