//go:build tools
// +build tools

package jsonschema

import (
	_ "golang.org/x/tools/cmd/goimports"

	_ "github.com/jandelgado/gcov2lcov"

	_ "github.com/ory/go-acc"
)
