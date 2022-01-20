// Package fileloader implements loader.Loader for file url schemes.
//
// The package is typically only imported for the side effect of
// registering its Loaders.
//
// To use httploader, link this package into your program:
//	import _ "github.com/ory/jsonschema/v3/fileloader"
//
package fileloader

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/ory/jsonschema/v3"
)

// Load implements jsonschema.Loader
func Load(ctx context.Context, url string) (io.ReadCloser, error) {
	f, err := os.Open(strings.TrimPrefix(url, "file://"))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func init() {
	jsonschema.Loaders["file"] = Load
}
