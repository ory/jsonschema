package base64loader_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ory/jsonschema/v3"
)

func TestLoad(t *testing.T) {
	schema := `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "bar": {
      "type": "string"
    }
  },
  "required": [
    "bar"
  ]
}`

	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
		base64.RawStdEncoding,
	} {
		c, err := jsonschema.Compile(context.Background(), "base64://"+enc.EncodeToString([]byte(schema)))
		require.NoError(t, err)
		require.EqualError(t, c.Validate(bytes.NewBufferString(`{"bar": 1234}`)), "I[#/bar] S[#/properties/bar/type] expected string, but got number")
	}
}
