package kubernetes

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSortPodsByName tests the SortPodsByName function.
func TestSortPodsByName(t *testing.T) {
	testCases := []struct {
		name     string
		input    *v1.PodList
		expected []string
	}{
		{
			name: "basic unsorted list",
			input: &v1.PodList{
				Items: []v1.Pod{
					{ObjectMeta: metav1.ObjectMeta{Name: "pod-2"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "pod-0"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
				},
			},
			expected: []string{"pod-0", "pod-1", "pod-2"},
		},
		{
			name: "already sorted list",
			input: &v1.PodList{
				Items: []v1.Pod{
					{ObjectMeta: metav1.ObjectMeta{Name: "alpha"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "beta"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "gamma"}},
				},
			},
			expected: []string{"alpha", "beta", "gamma"},
		},
		{
			name: "reverse sorted list",
			input: &v1.PodList{
				Items: []v1.Pod{
					{ObjectMeta: metav1.ObjectMeta{Name: "zebra"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "yak"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "xylophone"}},
				},
			},
			expected: []string{"xylophone", "yak", "zebra"},
		},
		{
			name: "empty list",
			input: &v1.PodList{
				Items: []v1.Pod{},
			},
			expected: []string{},
		},
		{
			name: "list with one item",
			input: &v1.PodList{
				Items: []v1.Pod{
					{ObjectMeta: metav1.ObjectMeta{Name: "single-pod"}},
				},
			},
			expected: []string{"single-pod"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			SortPodsByName(tc.input)

			// Extract the names from the sorted list for comparison
			actualNames := []string{}
			if tc.input != nil { // Handle nil input case
				for _, pod := range tc.input.Items {
					actualNames = append(actualNames, pod.Name)
				}
			}

			// Compare the actual sorted names with the expected names
			if !reflect.DeepEqual(actualNames, tc.expected) {
				t.Errorf("SortPodsByName() for test case '%s':\nExpected order: %v\nActual order:   %v",
					tc.name, tc.expected, actualNames)
			}
		})
	}
}
