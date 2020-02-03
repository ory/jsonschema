package jsonschema_test

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"github.com/ory/jsonschema/v3"
)

func TestErrorsContext(t *testing.T) {
	for k, tc := range []struct {
		path     string
		doc      string
		expected interface{}
	}{
		{
			path:     "testdata/errors/required.json#/0",
			doc:      `{}`,
			expected: &jsonschema.ValidationContextRequired{Missing: []string{"#/bar"}},
		},
		{
			path: "testdata/errors/required.json#/0",
			doc:  `{"bar":{}}`,
			expected: &jsonschema.ValidationContextRequired{
				Missing: []string{"#/bar/foo"},
			},
		},
		{
			path: "testdata/errors/required.json#/1",
			doc:  `{"object":{"object":{"foo":"foo"}}}`,
			expected: &jsonschema.ValidationContextRequired{
				Missing: []string{"#/object/object/bar"},
			},
		},
		{
			path: "testdata/errors/required.json#/1",
			doc:  `{"object":{"object":{"bar":"bar"}}}`,
			expected: &jsonschema.ValidationContextRequired{
				Missing: []string{"#/object/object/foo"},
			},
		},
		{
			path: "testdata/errors/required.json#/1",
			doc:  `{"object":{"object":{}}}`,
			expected: &jsonschema.ValidationContextRequired{
				Missing: []string{"#/object/object/foo", "#/object/object/bar"},
			},
		},
		{
			path: "testdata/errors/required.json#/1",
			doc:  `{"object":{}}`,
			expected: &jsonschema.ValidationContextRequired{
				Missing: []string{"#/object/object"},
			},
		},
		{
			path: "testdata/errors/required.json#/1",
			doc:  `{}`,
			expected: &jsonschema.ValidationContextRequired{
				Missing: []string{"#/object"},
			},
		},
	} {
		t.Run(fmt.Sprintf("case=%d", k), func(t *testing.T) {
			var (
				schema = jsonschema.MustCompile(tc.path)
				err    = schema.Validate(bytes.NewBufferString(tc.doc))
			)

			if err == nil {
				t.Errorf("Expected error but got nil")
				return
			}

			var (
				actual = err.(*jsonschema.ValidationError).Context
			)

			if !reflect.DeepEqual(tc.expected, actual) {
				t.Errorf("expected:\t%#v\n\tactual:\t%#v", tc.expected, actual)
			}
		})
	}
}
