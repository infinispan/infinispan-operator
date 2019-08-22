package infinispan

import (
	"errors"
	"strings"
	"testing"

	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Inputs and expected outputs for getInfinispanCondition func
var podsSameView = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1-View1"}},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod2-View1"}},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod3-View1"}},
}
var resPodsSameView = infinispanv1.InfinispanCondition{Type: "wellFormed", Status: "True", Message: "View: View1"}
var podsDifferentView = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod1-View1"}},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod2-View2"}},
	{ObjectMeta: metav1.ObjectMeta{Name: "pod3-View2"}},
}
var resPodsDifferentView = infinispanv1.InfinispanCondition{Type: "wellFormed", Status: "False", Message: "Views: View1,View2"}

var podsErroredView = []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Namespace: "unitTest", Name: "pod1-View1"}},
	{ObjectMeta: metav1.ObjectMeta{Namespace: "unitTest", Name: "pod2-ErrorView1"}},
	{ObjectMeta: metav1.ObjectMeta{Namespace: "unitTest", Name: "pod3-View1"}},
}
var resPodsErroredView = infinispanv1.InfinispanCondition{Type: "wellFormed", Status: "Unknown", Message: "Errors: pod2-ErrorView1: Error in getting view Views: View1"}

var testTable = []struct {
	pods      []corev1.Pod
	condition infinispanv1.InfinispanCondition
}{
	{podsSameView, resPodsSameView},
	{podsDifferentView, resPodsDifferentView},
	{podsErroredView, resPodsErroredView}}

// mockCluster produce fake cluster member infos
type mockCluster struct{}

// GetClusterMembers returns a fake cluster view, produced returning the substring
// after the - char of the `name` arg. If the substring doesn't start with View and error
// will be also returned
func (m mockCluster) GetClusterMembers(ns, name, secretName string) ([]string, error) {
	arr := strings.Split(name, "-")
	if (len(arr) > 1) && strings.HasPrefix(arr[1], "View") {
		return []string{arr[1]}, nil
	}
	return nil, errors.New("Error in getting view")
}

// TestGetInfinispanConditions test for getInfinispanConditions func
func TestGetInfinispanConditions(t *testing.T) {
	var m mockCluster

	for _, tup := range testTable {
		conditions := getInfinispanConditions(m, tup.pods, "")
		if len(conditions) != 1 {
			t.Errorf("Expected exaclty 1 condition got %d", len(conditions))
		}
		if !(conditions[0] == tup.condition) {
			t.Errorf("Expected %+v and got %+v\n", tup.condition, conditions[0])
		}
	}

}
