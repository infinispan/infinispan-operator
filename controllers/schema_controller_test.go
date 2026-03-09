package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshallSchemaEventConfig(t *testing.T) {
	tests := []struct {
		name          string
		data          string
		expectedName  string
		expectedProto string
		expectErr     bool
	}{
		{
			name:          "Valid schema event",
			data:          `{"schema":{"name":"person.proto","errors":"","proto":"message Person {\n    string name = 1;\n}\n"}}`,
			expectedName:  "person.proto",
			expectedProto: "message Person {\n    string name = 1;\n}\n",
		},
		{
			name:          "Schema event with errors",
			data:          `{"schema":{"name":"broken.proto","errors":"Syntax error at line 3","proto":"message Broken { bad }"}}`,
			expectedName:  "broken.proto",
			expectedProto: "message Broken { bad }",
		},
		{
			name:      "Invalid JSON",
			data:      `not json`,
			expectErr: true,
		},
		{
			name:      "Missing schema name",
			data:      `{"schema":{"name":"","errors":"","proto":"message Foo {}"}}`,
			expectErr: true,
		},
		{
			name:      "Empty object",
			data:      `{}`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, proto, err := unmarshallSchemaEventConfig([]byte(tt.data))
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedName, name)
				assert.Equal(t, tt.expectedProto, proto)
			}
		})
	}
}
