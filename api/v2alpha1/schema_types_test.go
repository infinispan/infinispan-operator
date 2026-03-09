package v2alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetSchemaName(t *testing.T) {
	tests := []struct {
		name     string
		schema   Schema
		expected string
	}{
		{
			name: "Uses spec.Name when set",
			schema: Schema{
				ObjectMeta: metav1.ObjectMeta{Name: "my-schema"},
				Spec:       SchemaSpec{Name: "person.proto"},
			},
			expected: "person.proto",
		},
		{
			name: "Falls back to ObjectMeta.Name",
			schema: Schema{
				ObjectMeta: metav1.ObjectMeta{Name: "person"},
				Spec:       SchemaSpec{},
			},
			expected: "person.proto",
		},
		{
			name: "Appends .proto suffix if missing from spec.Name",
			schema: Schema{
				ObjectMeta: metav1.ObjectMeta{Name: "my-schema"},
				Spec:       SchemaSpec{Name: "person"},
			},
			expected: "person.proto",
		},
		{
			name: "Does not double-append .proto suffix",
			schema: Schema{
				ObjectMeta: metav1.ObjectMeta{Name: "my-schema"},
				Spec:       SchemaSpec{Name: "person.proto"},
			},
			expected: "person.proto",
		},
		{
			name: "Appends .proto suffix to ObjectMeta.Name",
			schema: Schema{
				ObjectMeta: metav1.ObjectMeta{Name: "address"},
				Spec:       SchemaSpec{},
			},
			expected: "address.proto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.schema.GetSchemaName())
		})
	}
}

func TestSchemaSetCondition(t *testing.T) {
	schema := &Schema{}

	// Adding a new condition returns true
	changed := schema.SetCondition(SchemaConditionReady, metav1.ConditionTrue, "")
	assert.True(t, changed)
	assert.Len(t, schema.Status.Conditions, 1)
	assert.Equal(t, SchemaConditionReady, schema.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, schema.Status.Conditions[0].Status)

	// Setting the same condition with same values returns false
	changed = schema.SetCondition(SchemaConditionReady, metav1.ConditionTrue, "")
	assert.False(t, changed)
	assert.Len(t, schema.Status.Conditions, 1)

	// Updating status returns true
	changed = schema.SetCondition(SchemaConditionReady, metav1.ConditionFalse, "some error")
	assert.True(t, changed)
	assert.Len(t, schema.Status.Conditions, 1)
	assert.Equal(t, metav1.ConditionFalse, schema.Status.Conditions[0].Status)
	assert.Equal(t, "some error", schema.Status.Conditions[0].Message)

	// Updating only message returns true
	changed = schema.SetCondition(SchemaConditionReady, metav1.ConditionFalse, "different error")
	assert.True(t, changed)
	assert.Equal(t, "different error", schema.Status.Conditions[0].Message)
}

func TestSchemaGetCondition(t *testing.T) {
	schema := &Schema{}

	// Absent condition returns False status
	cond := schema.GetCondition(SchemaConditionReady)
	assert.Equal(t, SchemaConditionReady, cond.Type)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)

	// Returns existing condition
	schema.SetCondition(SchemaConditionReady, metav1.ConditionTrue, "all good")
	cond = schema.GetCondition(SchemaConditionReady)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, "all good", cond.Message)
}
