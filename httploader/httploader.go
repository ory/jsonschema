// Copyright 2017 Santhosh Kumar Tekuri. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package httploader implements loader.Loader for http/https url.
//
// The package is typically only imported for the side effect of
// registering its Loaders.
//
// To use httploader, link this package into your program:
//
//	import _ "github.com/ory/jsonschema/v3/httploader"
package httploader

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/ory/jsonschema/v3"
)

type key string

const ContextKey key = "github.com/ory/jsonschema/v3/httploader.HTTPClient"

// Load implements jsonschemav2.Loader
func Load(ctx context.Context, url string) (io.ReadCloser, error) {
	var hc *retryablehttp.Client
	if v := ctx.Value(ContextKey); v == nil {
		return nil, fmt.Errorf("expected a client to be set for %s but received nil", ContextKey)
	} else if c, ok := v.(*retryablehttp.Client); ok {
		hc = c
	} else {
		return nil, fmt.Errorf("invalid context value for %s expected %T but got: %T", ContextKey, new(retryablehttp.Client), v)
	}

	req, err := retryablehttp.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s returned status code %d", url, resp.StatusCode)
	}

	return resp.Body, nil
}

func init() {
	jsonschema.Loaders["http"] = Load
	jsonschema.Loaders["https"] = Load
}
