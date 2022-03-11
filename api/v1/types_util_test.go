package v1

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const namespace = "testing-namespace"

var exposeRouteInfinispan = &Infinispan{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "extra-long-cluster-name-d----------------------------d",
		Namespace: namespace,
	},
	Spec: InfinispanSpec{
		Expose: &ExposeSpec{
			Type: ExposeTypeRoute,
		},
	},
}

func TestServiceExternalName(t *testing.T) {
	assert.Equal(t, "extra-long-cluster-name-d-------------------a", exposeRouteInfinispan.GetServiceExternalName(), "Route expose long name")
	assert.LessOrEqual(t, MaxRouteObjectNameLength, len(exposeRouteInfinispan.GetServiceExternalName())+len(namespace)+1, "Route expose name length")

	exposeRouteInfinispan.Name = "example-infinispan"
	assert.Equal(t, "example-infinispan-external", exposeRouteInfinispan.GetServiceExternalName(), "Route expose name")
}

func TestApplyOperatorLabels(t *testing.T) {
	testTable := []struct {
		Labels            string
		PodLabels         string
		ExpectedLabels    string
		ExpectedPodLabels string
	}{
		{"", "", "", ""},
		{"{\"label\":\"value\"}", "", "label", ""},
		{"", "{\"label\":\"value\"}", "", "label"},
		{"{\"l2\":\"v2\",\"l1\":\"v1\"}", "", "l1,l2", ""},
		{"", "{\"l2\":\"v2\",\"l1\":\"v1\"}", "", "l1,l2"},
		{"{\"l2\":\"v2\",\"l1\":\"v1\"}", "{\"lp2\":\"v2\",\"lp1\":\"v1\"}", "l1,l2", "lp1,lp2"},
	}
	for _, testItem := range testTable {
		err := os.Setenv("INFINISPAN_OPERATOR_TARGET_LABELS", testItem.Labels)
		assert.Nil(t, err)
		err = os.Setenv("INFINISPAN_OPERATOR_POD_TARGET_LABELS", testItem.PodLabels)
		assert.Nil(t, err)

		defaultLabels, defaultAnnotations, err := LoadDefaultLabelsAndAnnotations()
		assert.Nil(t, err)

		ispn := exposeRouteInfinispan.DeepCopy()
		ispn.ApplyOperatorMeta(defaultLabels, defaultAnnotations)
		assert.Nil(t, err)
		assert.Equal(t, ispn.Annotations[OperatorTargetLabels], testItem.ExpectedLabels)
		assert.Equal(t, ispn.Annotations[OperatorPodTargetLabels], testItem.ExpectedPodLabels)
		labelMap := make(map[string]string)
		if testItem.Labels != "" {
			err = json.Unmarshal([]byte(testItem.Labels), &labelMap)
			assert.Nil(t, err)
		}
		labelPodMap := make(map[string]string)
		if testItem.PodLabels != "" {
			err = json.Unmarshal([]byte(testItem.PodLabels), &labelPodMap)
			assert.Nil(t, err)
		}
		for n, v := range labelMap {
			labelPodMap[n] = v
		}
		assert.True(t, reflect.DeepEqual(ispn.ObjectMeta.Labels, labelPodMap) || len(labelPodMap) == 0 && ispn.ObjectMeta.Labels == nil)
	}
}

func TestApplyOperatorAnnotations(t *testing.T) {
	testTable := []struct {
		Annotations            string
		PodAnnotations         string
		ExpectedAnnotations    string
		ExpectedPodAnnotations string
	}{
		{"", "", "", ""},
		{"{\"annotation\":\"value\"}", "", "annotation", ""},
		{"", "{\"annotation\":\"value\"}", "", "annotation"},
		{"{\"a2\":\"v2\",\"a1\":\"v1\"}", "", "a1,a2", ""},
		{"", "{\"a2\":\"v2\",\"a1\":\"v1\"}", "", "a1,a2"},
		{"{\"a2\":\"v2\",\"a1\":\"v1\"}", "{\"ap2\":\"v2\",\"ap1\":\"v1\"}", "a1,a2", "ap1,ap2"},
	}

	for _, testItem := range testTable {
		err := os.Setenv("INFINISPAN_OPERATOR_TARGET_ANNOTATIONS", testItem.Annotations)
		assert.Nil(t, err)

		err = os.Setenv("INFINISPAN_OPERATOR_POD_TARGET_ANNOTATIONS", testItem.PodAnnotations)
		assert.Nil(t, err)

		defaultLabels, defaultAnnotations, err := LoadDefaultLabelsAndAnnotations()
		assert.Nil(t, err)

		ispn := exposeRouteInfinispan.DeepCopy()
		ispn.ApplyOperatorMeta(defaultLabels, defaultAnnotations)
		assert.Equal(t, ispn.Annotations[OperatorTargetAnnotations], testItem.ExpectedAnnotations)
		assert.Equal(t, ispn.Annotations[OperatorPodTargetAnnotations], testItem.ExpectedPodAnnotations)

		// We've already tested the content of the target annotations, so remove to simplify user annotation comparison
		delete(ispn.Annotations, OperatorTargetAnnotations)
		delete(ispn.Annotations, OperatorPodTargetAnnotations)
		delete(ispn.Annotations, OperatorTargetLabels)
		delete(ispn.Annotations, OperatorPodTargetLabels)

		annotationMap := make(map[string]string)
		if testItem.Annotations != "" {
			err = json.Unmarshal([]byte(testItem.Annotations), &annotationMap)
			assert.Nil(t, err)
		}

		annotationPodMap := make(map[string]string)
		if testItem.PodAnnotations != "" {
			err = json.Unmarshal([]byte(testItem.PodAnnotations), &annotationPodMap)
			assert.Nil(t, err)
		}

		for n, v := range annotationMap {
			annotationPodMap[n] = v
		}
		assert.True(t, reflect.DeepEqual(ispn.Annotations, annotationPodMap) || len(annotationPodMap) == 0 && ispn.Annotations == nil)
	}
}
