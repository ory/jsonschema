// Package base64loader (standard encoding) implements loader.Loader for base64-encoded JSON url schemes.
//
// The package is typically only imported for the side effect of
// registering its Loaders.
//
// To use base64loader, link this package into your program:
//	import _ "github.com/ory/jsonschema/v3/base64loader"
//
package fileloader

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/ory/jsonschema/v3"
)

// Load implements jsonschema.Loader
func Load(url string) (_ io.ReadCloser, err error) {
	encoded := strings.TrimPrefix(url, "base64://")

	var raw []byte

	raw, err = base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("unable to decode std encoded base64 string: %s", err)
	}

	return ioutil.NopCloser(bytes.NewBuffer(raw)), nil
}

func init() {
	jsonschema.Loaders["base64"] = Load
}
