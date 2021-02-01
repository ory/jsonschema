package fileloader_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/ory/jsonschema/v3"
	_ "github.com/ory/jsonschema/v3/base64loader"
	"github.com/stretchr/testify/require"
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
		c, err := jsonschema.Compile("base64://" + enc.EncodeToString([]byte(schema)))
		require.NoError(t, err)
		require.EqualError(t, c.Validate(bytes.NewBufferString(`{"bar": 1234}`)), "I[#/bar] S[#/properties/bar/type] expected string, but got number")
	}
}
