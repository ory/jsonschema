// Copyright 2017 Santhosh Kumar Tekuri. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonschema

import (
	"context"
	"strings"
)

// Draft4 respresents http://json-schema.org/specification-links.html#draft-4
var Draft4 = &Draft{id: "id", version: 4, url: "http://json-schema.org/draft-04/schema", data: `{
		"$schema": "http://json-schema.org/draft-04/schema#",
		"description": "Core schema meta-schema",
		"definitions": {
		    "schemaArray": {
		        "type": "array",
		        "minItems": 1,
		        "items": { "$ref": "#" }
		    },
		    "positiveInteger": {
		        "type": "integer",
		        "minimum": 0
		    },
		    "positiveIntegerDefault0": {
		        "allOf": [ { "$ref": "#/definitions/positiveInteger" }, { "default": 0 } ]
		    },
		    "simpleTypes": {
		        "enum": [ "array", "boolean", "integer", "null", "number", "object", "string" ]
		    },
		    "stringArray": {
		        "type": "array",
		        "items": { "type": "string" },
		        "minItems": 1,
		        "uniqueItems": true
		    }
		},
		"type": "object",
		"properties": {
		    "id": {
		        "type": "string",
		        "format": "uriref"
		    },
		    "$schema": {
		        "type": "string",
		        "format": "uri"
		    },
		    "title": {
		        "type": "string"
		    },
		    "description": {
		        "type": "string"
		    },
		    "default": {},
		    "multipleOf": {
		        "type": "number",
		        "minimum": 0,
		        "exclusiveMinimum": true
		    },
		    "maximum": {
		        "type": "number"
		    },
		    "exclusiveMaximum": {
		        "type": "boolean",
		        "default": false
		    },
		    "minimum": {
		        "type": "number"
		    },
		    "exclusiveMinimum": {
		        "type": "boolean",
		        "default": false
		    },
		    "maxLength": { "$ref": "#/definitions/positiveInteger" },
		    "minLength": { "$ref": "#/definitions/positiveIntegerDefault0" },
		    "pattern": {
		        "type": "string",
		        "format": "regex"
		    },
		    "additionalItems": {
		        "anyOf": [
		            { "type": "boolean" },
		            { "$ref": "#" }
		        ],
		        "default": {}
		    },
		    "items": {
		        "anyOf": [
		            { "$ref": "#" },
		            { "$ref": "#/definitions/schemaArray" }
		        ],
		        "default": {}
		    },
		    "maxItems": { "$ref": "#/definitions/positiveInteger" },
		    "minItems": { "$ref": "#/definitions/positiveIntegerDefault0" },
		    "uniqueItems": {
		        "type": "boolean",
		        "default": false
		    },
		    "maxProperties": { "$ref": "#/definitions/positiveInteger" },
		    "minProperties": { "$ref": "#/definitions/positiveIntegerDefault0" },
		    "required": { "$ref": "#/definitions/stringArray" },
		    "additionalProperties": {
		        "anyOf": [
		            { "type": "boolean" },
		            { "$ref": "#" }
		        ],
		        "default": {}
		    },
		    "definitions": {
		        "type": "object",
		        "additionalProperties": { "$ref": "#" },
		        "default": {}
		    },
		    "properties": {
		        "type": "object",
		        "additionalProperties": { "$ref": "#" },
		        "default": {}
		    },
		    "patternProperties": {
		        "type": "object",
		        "regexProperties": true,
		        "additionalProperties": { "$ref": "#" },
		        "default": {}
		    },
		    "regexProperties": { "type": "boolean" },
		    "dependencies": {
		        "type": "object",
		        "additionalProperties": {
		            "anyOf": [
		                { "$ref": "#" },
		                { "$ref": "#/definitions/stringArray" }
		            ]
		        }
		    },
		    "enum": {
		        "type": "array",
		        "minItems": 1,
		        "uniqueItems": true
		    },
		    "type": {
		        "anyOf": [
		            { "$ref": "#/definitions/simpleTypes" },
		            {
		                "type": "array",
		                "items": { "$ref": "#/definitions/simpleTypes" },
		                "minItems": 1,
		                "uniqueItems": true
		            }
		        ]
		    },
		    "allOf": { "$ref": "#/definitions/schemaArray" },
		    "anyOf": { "$ref": "#/definitions/schemaArray" },
		    "oneOf": { "$ref": "#/definitions/schemaArray" },
		    "not": { "$ref": "#" },
		    "format": { "type": "string" },
		    "$ref": { "type": "string" }
		},
		"dependencies": {
		    "exclusiveMaximum": [ "maximum" ],
		    "exclusiveMinimum": [ "minimum" ]
		},
		"default": {}
	}`}

func init() {
	ctx := context.Background()
	c := NewCompiler()
	err := c.AddResource(Draft4.url, strings.NewReader(Draft4.data))
	if err != nil {
		panic(err)
	}
	Draft4.meta = c.MustCompile(ctx, Draft4.url)
}
