package infinispan

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/infinispan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Inputs and expected outputs for getInfinispanCondition func
var podsReadyStatus = corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.ContainersReady, Status: "True"}}}

var podsSameView = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1-View1"}, Status: podsReadyStatus},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod2-View1"}, Status: podsReadyStatus},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod3-View1"}, Status: podsReadyStatus},
}
var resPodsSameView = ispnv1.InfinispanCondition{Type: ispnv1.ConditionWellFormed, Status: metav1.ConditionTrue, Message: "View: View1"}

var podsDifferentView = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1-View1"}, Status: podsReadyStatus},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod2-View2"}, Status: podsReadyStatus},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod3-View2"}, Status: podsReadyStatus},
}
var resPodsDifferentView = ispnv1.InfinispanCondition{Type: ispnv1.ConditionWellFormed, Status: metav1.ConditionFalse, Message: "Views: View1,View2"}

var podsErroredView = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Namespace: "unitTest", Name: "pod1-View1"}, Status: podsReadyStatus},
	{ObjectMeta: metav1.ObjectMeta{Namespace: "unitTest", Name: "pod2-ErrorView1"}, Status: podsReadyStatus},
	{ObjectMeta: metav1.ObjectMeta{Namespace: "unitTest", Name: "pod3-View1"}, Status: podsReadyStatus},
}
var resPodsErroredView = ispnv1.InfinispanCondition{Type: ispnv1.ConditionWellFormed, Status: metav1.ConditionUnknown, Message: "Errors: pod2-ErrorView1: error in getting view Views: View1"}

var podsNotReadyView = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1-View1"}, Status: podsReadyStatus},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod2-View1"}},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod3-View1"}},
}
var resPodsNotReadyView = ispnv1.InfinispanCondition{Type: ispnv1.ConditionWellFormed, Status: metav1.ConditionUnknown, Message: "Errors: pod2-View1: pod not ready,pod3-View1: pod not ready Views: View1"}

var testTable = []struct {
	pods      []corev1.Pod
	condition ispnv1.InfinispanCondition
}{
	{podsSameView, resPodsSameView},
	{podsDifferentView, resPodsDifferentView},
	{podsErroredView, resPodsErroredView},
	{podsNotReadyView, resPodsNotReadyView}}

// mockCluster produce fake cluster member infos
type mockCluster struct{}

// GetClusterMembers returns a fake cluster view, produced returning the substring
// after the - char of the `name` arg. If the substring doesn't start with View and error
// will be also returned
func (m mockCluster) GetClusterMembers(podName string) ([]string, error) {
	arr := strings.Split(podName, "-")
	if (len(arr) > 1) && strings.HasPrefix(arr[1], "View") {
		return []string{arr[1]}, nil
	}
	return nil, errors.New("error in getting view")
}

func (m mockCluster) GracefulShutdown(podName string) error {
	return nil
}

func (m mockCluster) GracefulShutdownTask(podName string) error {
	return nil
}

func (m mockCluster) GetClusterSize(podName string) (int, error) {
	return 0, nil
}

func (m mockCluster) ExistsCache(cacheName, podName string) (bool, error) {
	return false, nil
}

func (m mockCluster) CreateCacheWithTemplateName(cacheName, cacheXML, podName string) error {
	return nil
}

// CacheNames return the names of the cluster caches available on the pod `podName`
func (m mockCluster) CacheNames(podName string) ([]string, error) {
	return nil, nil
}

func (m mockCluster) CreateCacheWithTemplate(cacheName, templateName, podName string) error {
	return nil
}

func (m mockCluster) GetMemoryLimitBytes(podName string) (uint64, error) {
	return 0, nil
}

func (m mockCluster) GetMaxMemoryUnboundedBytes(podName string) (uint64, error) {
	return 0, nil
}

func (m mockCluster) GetMetrics(podName, postfix string) (*bytes.Buffer, error) {
	buf := []byte{}
	s := bytes.NewBuffer(buf)
	return s, nil
}

func (m mockCluster) GetCacheManagerInfo(cacheManagerName, podName string) (*infinispan.CacheManagerInfo, error) {
	return nil, nil
}

func (m mockCluster) GetLoggers(podName string) (map[string]string, error) {
	return nil, nil
}

func (m mockCluster) SetLogger(podName, loggerName, loggerLevel string) error {
	return nil
}

func (m mockCluster) XsitePushAllState(podName string) error {
	return nil
}

// TestGetInfinispanConditions test for getInfinispanConditions func
func TestGetInfinispanConditions(t *testing.T) {
	var m mockCluster

	// Create an Infinispan mock definition
	var spec = ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "infinispan-example",
		},
		Spec: ispnv1.InfinispanSpec{
			Security: ispnv1.InfinispanSecurity{EndpointSecretName: "conn-secret-test"},
		},
	}

	for _, tup := range testTable {
		conditions := getInfinispanConditions(tup.pods, &spec, m)
		if len(conditions) != 1 {
			t.Errorf("Expected exaclty 1 condition got %d", len(conditions))
		}
		if !(conditions[0] == tup.condition) {
			t.Errorf("Expected %+v and got %+v\n", tup.condition, conditions[0])
		}
	}
}
