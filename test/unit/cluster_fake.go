package unit

import (
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
)

// FakeCluster produce fake Infinispan cluster member infos
type FakeCluster struct {
	Kubernetes *ispnutil.Kubernetes
}

// GetClusterMembers returns a fake Infinispan cluster view based on static data
func (fc FakeCluster) GetClusterMembers(secretName, podName, namespace, _ string) ([]string, error) {
	_, err := fc.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return nil, err
	}

	_, err = fc.Kubernetes.GetPassword("operator", secretName, namespace)
	if err != nil {
		return nil, err
	}

	return []string{"node01"}, nil
}

func (fc FakeCluster) GracefulShutdown(_, _, _, _ string) error {
	return nil
}

func (fc FakeCluster) GetClusterSize(_, _, _, _ string) (int, error) {
	return 1, nil
}

func (fc FakeCluster) ExistsCache(_, _, _, _,_ string) bool {
	return false
}

func (fc FakeCluster) CreateCache(_, _, _, _, _, _ string) error {
	return nil
}

func (fc FakeCluster) GetMemoryLimitBytes(_, _ string) (uint64, error) {
	return 0, nil
}

func (fc FakeCluster) GetMaxMemoryUnboundedBytes(_, _ string) (uint64, error) {
	return 0, nil
}
