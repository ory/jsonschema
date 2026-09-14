// Copyright 2017 Santhosh Kumar Tekuri. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonschema

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A Schema represents compiled version of json-schema.
type Schema struct {
	limits *Limits

	URL string // absolute url of the resource.
	Ptr string // json-pointer to schema. always starts with `#`.

	// type agnostic validations
	format    func(interface{}) bool
	Format    string
	Always    *bool         // always pass/fail. used when booleans are used as schemas in draft-07.
	Ref       *Schema       // reference to actual schema. if not nil, all the remaining fields are ignored.
	Types     []string      // allowed types.
	Constant  []interface{} // first element in slice is constant value. note: slice is used to capture nil constant.
	Enum      []interface{} // allowed values.
	enumError string        // error message for enum fail. captured here to avoid constructing error message every time.
	Not       *Schema
	AllOf     []*Schema
	AnyOf     []*Schema
	OneOf     []*Schema
	If        *Schema
	Then      *Schema // nil, when If is nil.
	Else      *Schema // nil, when If is nil.

	// object validations
	MinProperties        int      // -1 if not specified.
	MaxProperties        int      // -1 if not specified.
	Required             []string // list of required properties.
	Properties           map[string]*Schema
	PropertyNames        *Schema
	RegexProperties      bool // property names must be valid regex. used only in draft4 as workaround in metaschema.
	PatternProperties    map[*regexp.Regexp]*Schema
	AdditionalProperties interface{}            // nil or false or *Schema.
	Dependencies         map[string]interface{} // value is *Schema or []string.

	// array validations
	MinItems        int // -1 if not specified.
	MaxItems        int // -1 if not specified.
	UniqueItems     bool
	Items           interface{} // nil or *Schema or []*Schema
	AdditionalItems interface{} // nil or bool or *Schema.
	Contains        *Schema

	// string validations
	MinLength        int // -1 if not specified.
	MaxLength        int // -1 if not specified.
	Pattern          *regexp.Regexp
	ContentEncoding  string
	decoder          func(string) ([]byte, error)
	ContentMediaType string
	mediaType        func([]byte) error

	// number validators
	Minimum          *big.Float
	ExclusiveMinimum *big.Float
	Maximum          *big.Float
	ExclusiveMaximum *big.Float
	MultipleOf       *big.Float

	// annotations. captured only when Compiler.ExtractAnnotations is true.
	Title       string
	Description string
	Default     interface{}
	ReadOnly    bool
	WriteOnly   bool
	Examples    []interface{}

	// user defined extensions
	Extensions map[string]interface{}
	extensions map[string]func(ctx ValidationContext, s interface{}, v interface{}) error
}

// Compile parses json-schema at given url returns, if successful,
// a Schema object that can be used to match against json.
//
// Returned error can be *SchemaError
func Compile(ctx context.Context, url string) (*Schema, error) {
	return NewCompiler().Compile(ctx, url)
}

// MustCompile is like Compile but panics if the url cannot be compiled to *Schema.
// It simplifies safe initialization of global variables holding compiled Schemas.
func MustCompile(ctx context.Context, url string) *Schema {
	return NewCompiler().MustCompile(ctx, url)
}

// CompileString parses and compiles the given schema with given base url.
func CompileString(ctx context.Context, url, schema string) (*Schema, error) {
	c := NewCompiler()
	if err := c.AddResource(url, strings.NewReader(schema)); err != nil {
		return nil, err
	}
	return c.Compile(ctx, url)
}

// ResourceLimits returns an independent copy of the compiled resource policy.
func (s *Schema) ResourceLimits() *Limits {
	if s.limits == nil {
		return nil
	}
	limits := *s.limits
	return &limits
}

// Validate validates the given json data, against the json-schema.
//
// Returned error can be *ValidationError.
func (s *Schema) Validate(r io.Reader) error {
	return s.ValidateContext(context.Background(), r)
}

// ValidateContext validates JSON from r with cancellation and the compiled limits.
// The reader must independently honor cancellation and bound input bytes.
func (s *Schema) ValidateContext(ctx context.Context, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	doc, err := DecodeJSON(r)
	if err != nil {
		return err
	}
	return s.ValidateInterfaceContext(ctx, doc)
}

// ValidateInterface validates a value decoded with DecodeJSON against this schema.
func (s *Schema) ValidateInterface(doc interface{}) error {
	return s.ValidateInterfaceContext(context.Background(), doc)
}

// ValidateInterfaceContext validates a decoded JSON value with cancellation and the compiled limits.
func (s *Schema) ValidateInterfaceContext(ctx context.Context, doc interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			switch r := r.(type) {
			case InvalidJSONTypeError:
				err = r
			case budgetAbort:
				err = r.err
			default:
				panic(r)
			}
		}
	}()
	b := newBudget(ctx, s.limits)
	defer func() { b.finished = true }()
	b.scan(doc)
	if err := s.validate(doc, b); err != nil {
		finishSchemaContext(err, s)
		finishInstanceContext(err)
		b.observeError(err)
		b.spend(0)
		return err
	}
	b.spend(0)
	return nil
}

// validate validates given value v with this schema.
func (s *Schema) validate(v interface{}, b *budget) error {
	b.enter()
	defer b.leave()
	b.valueWork(v)
	if s.Always != nil {
		if !*s.Always {
			return b.errorf("", "always fail")
		}
		return nil
	}

	if s.Ref != nil {
		if err := s.Ref.validate(v, b); err != nil {
			finishSchemaContext(err, s.Ref)
			var refURL string
			if s.URL == s.Ref.URL {
				refURL = s.Ref.Ptr
			} else {
				refURL = s.Ref.URL + s.Ref.Ptr
			}
			return b.errorf("$ref", "doesn't validate with %q", refURL).add(err)
		}

		// All other properties in a "$ref" object MUST be ignored
		return nil
	}

	if len(s.Types) > 0 {
		vType := jsonType(v)
		matched := false
		for _, t := range s.Types {
			b.spend(1)
			if vType == t {
				matched = true
				break
			} else if t == "integer" && vType == "number" {
				if _, ok := new(big.Int).SetString(b.numberText(v), 10); ok {
					matched = true
					break
				}
			}
		}
		if !matched {
			return b.errorf("type", "expected %s, but got %s", strings.Join(s.Types, " or "), vType)
		}
	}

	var errors []error

	if len(s.Constant) > 0 {
		if !b.equal(v, s.Constant[0]) {
			switch jsonType(s.Constant[0]) {
			case "object", "array":
				errors = append(errors, b.errorf("const", "const failed"))
			default:
				errors = append(errors, b.errorf("const", "value must be %#v", s.Constant[0]))
			}
		}
	}

	if len(s.Enum) > 0 {
		matched := false
		for _, item := range s.Enum {
			b.spend(1)
			if b.equal(v, item) {
				matched = true
				break
			}
		}
		if !matched {
			errors = append(errors, b.errorf("enum", "%s", s.enumError))
		}
	}

	formatValid := true
	if s.format != nil {
		if s.Format == "regex" && b.limits != nil {
			pattern, ok := v.(string)
			if !ok {
				formatValid = false
			} else {
				_, err := b.regex(pattern)
				formatValid = err == nil
			}
		} else {
			formatValid = s.format(v)
		}
	}
	if !formatValid {
		errors = append(errors, b.errorf("format", "%q is not valid %q", v, s.Format))
	}

	if s.Not != nil && s.Not.validate(v, b) == nil {
		errors = append(errors, b.errorf("not", "not failed"))
	}

	for i, sch := range s.AllOf {
		b.spend(1)
		if err := sch.validate(v, b); err != nil {
			errors = append(errors, b.errorf("allOf/"+strconv.Itoa(i), "allOf failed").add(err))
		}
	}

	if len(s.AnyOf) > 0 {
		matched := false
		var causes []error
		for i, sch := range s.AnyOf {
			b.spend(1)
			if err := sch.validate(v, b); err == nil {
				matched = true
				break
			} else {
				causes = append(causes, addContext("", strconv.Itoa(i), err))
			}
		}
		if !matched {
			errors = append(errors, b.errorf("anyOf", "anyOf failed").add(causes...))
		}
	}

	if len(s.OneOf) > 0 {
		matched := -1
		var causes []error
		for i, sch := range s.OneOf {
			b.spend(1)
			if err := sch.validate(v, b); err == nil {
				if matched == -1 {
					matched = i
				} else {
					errors = append(errors, b.errorf("oneOf", "valid against schemas at indexes %d and %d", matched, i))
					break
				}
			} else {
				causes = append(causes, addContext("", strconv.Itoa(i), err))
			}
		}
		if matched == -1 {
			errors = append(errors, b.errorf("oneOf", "oneOf failed").add(causes...))
		}
	}

	if s.If != nil {
		if s.If.validate(v, b) == nil {
			if s.Then != nil {
				if err := s.Then.validate(v, b); err != nil {
					errors = append(errors, b.errorf("then", "if-then failed").add(err))
				}
			}
		} else {
			if s.Else != nil {
				if err := s.Else.validate(v, b); err != nil {
					errors = append(errors, b.errorf("else", "if-else failed").add(err))
				}
			}
		}
	}

	switch v := v.(type) {
	case map[string]interface{}:
		if s.MinProperties != -1 && len(v) < s.MinProperties {
			errors = append(errors, b.errorf("minProperties", "minimum %d properties allowed, but found %d properties", s.MinProperties, len(v)))
		}
		if s.MaxProperties != -1 && len(v) > s.MaxProperties {
			errors = append(errors, b.errorf("maxProperties", "maximum %d properties allowed, but found %d properties", s.MaxProperties, len(v)))
		}
		if len(s.Required) > 0 {
			var missing []string
			for _, pname := range s.Required {
				b.spend(1)
				if _, ok := v[pname]; !ok {
					b.spend(len(pname))
					missing = append(missing, pname)
				}
			}
			if len(missing) > 0 {
				errors = append(errors, b.requiredError(missing))
			}
		}

		var additionalProps map[string]struct{}
		if s.AdditionalProperties != nil {
			additionalProps = make(map[string]struct{}, len(v))
			for pname := range v {
				b.spend(1)
				additionalProps[pname] = struct{}{}
			}
		}

		if len(s.Properties) > 0 {
			for pname, pschema := range s.Properties {
				b.spend(1)
				if pvalue, ok := v[pname]; ok {
					delete(additionalProps, pname)
					if err := pschema.validate(pvalue, b); err != nil {
						name := b.escape(pname)
						errors = append(errors, addContext(name, b.joinPtr("properties", name), err))
					}
				}
			}
		}

		if s.PropertyNames != nil {
			for pname := range v {
				b.spend(1)
				if err := s.PropertyNames.validate(pname, b); err != nil {
					errors = append(errors, addContext(b.escape(pname), "propertyNames", err))
				}
			}
		}

		if s.RegexProperties {
			for pname := range v {
				b.spend(1)
				if _, err := b.regex(pname); err != nil {
					errors = append(errors, b.errorf("", "patternProperty %q is not valid regex", pname))
				}
			}
		}
		for pattern, pschema := range s.PatternProperties {
			b.spend(1)
			for pname, pvalue := range v {
				if b.match(pattern, pname) {
					delete(additionalProps, pname)
					if err := pschema.validate(pvalue, b); err != nil {
						errors = append(errors, addContext(b.escape(pname), b.joinPtr("patternProperties", b.escape(pattern.String())), err))
					}
				}
			}
		}
		if s.AdditionalProperties != nil {
			if _, ok := s.AdditionalProperties.(bool); ok {
				if len(additionalProps) != 0 {
					pnames := make([]string, 0)
					for pname := range additionalProps {
						b.spend(1)
						pnames = append(pnames, strconv.Quote(b.detail(pname)))
						if b.limits != nil && len(pnames) == 8 && len(additionalProps) > 8 && b.displayLimit("diagnostic properties") {
							break
						}
					}
					errors = append(errors, b.errorf("additionalProperties", "additionalProperties %s not allowed", strings.Join(pnames, ", ")))
				}
			} else {
				schema := s.AdditionalProperties.(*Schema)
				for pname := range additionalProps {
					b.spend(1)
					if pvalue, ok := v[pname]; ok {
						if err := schema.validate(pvalue, b); err != nil {
							errors = append(errors, addContext(b.escape(pname), "additionalProperties", err))
						}
					}
				}
			}
		}
		for dname, dvalue := range s.Dependencies {
			b.spend(1)
			if _, ok := v[dname]; ok {
				switch dvalue := dvalue.(type) {
				case *Schema:
					if err := dvalue.validate(v, b); err != nil {
						errors = append(errors, addContext("", b.joinPtr("dependencies", b.escape(dname)), err))
					}
				case []string:
					for i, pname := range dvalue {
						b.spend(1)
						if _, ok := v[pname]; !ok {
							errors = append(errors, b.errorf(b.joinPtr(b.joinPtr("dependencies", b.escape(dname)), strconv.Itoa(i)), "property %q is required, if %q property exists", pname, dname))
						}
					}
				}
			}
		}

	case []interface{}:
		if s.MinItems != -1 && len(v) < s.MinItems {
			errors = append(errors, b.errorf("minItems", "minimum %d items allowed, but found %d items", s.MinItems, len(v)))
		}
		if s.MaxItems != -1 && len(v) > s.MaxItems {
			errors = append(errors, b.errorf("maxItems", "maximum %d items allowed, but found %d items", s.MaxItems, len(v)))
		}
		if s.UniqueItems {
			if first, second, ok := b.duplicate(v); ok {
				errors = append(errors, b.errorf("uniqueItems", "items at index %d and %d are equal", first, second))
			}
		}
		switch items := s.Items.(type) {
		case *Schema:
			for i, item := range v {
				b.spend(1)
				if err := items.validate(item, b); err != nil {
					errors = append(errors, addContext(strconv.Itoa(i), "items", err))
				}
			}
		case []*Schema:
			if additionalItems, ok := s.AdditionalItems.(bool); ok {
				if !additionalItems && len(v) > len(items) {
					errors = append(errors, b.errorf("additionalItems", "only %d items are allowed, but found %d items", len(items), len(v)))
				}
			}
			for i, item := range v {
				b.spend(1)
				if i < len(items) {
					if err := items[i].validate(item, b); err != nil {
						errors = append(errors, addContext(strconv.Itoa(i), "items/"+strconv.Itoa(i), err))
					}
				} else if sch, ok := s.AdditionalItems.(*Schema); ok {
					if err := sch.validate(item, b); err != nil {
						errors = append(errors, addContext(strconv.Itoa(i), "additionalItems", err))
					}
				} else {
					break
				}
			}
		}
		if s.Contains != nil {
			matched := false
			var causes []error
			for i, item := range v {
				b.spend(1)
				if err := s.Contains.validate(item, b); err != nil {
					causes = append(causes, addContext(strconv.Itoa(i), "", err))
				} else {
					matched = true
					break
				}
			}
			if !matched {
				errors = append(errors, b.errorf("contains", "contains failed").add(causes...))
			}
		}

	case string:
		if s.MinLength != -1 || s.MaxLength != -1 {
			length := utf8.RuneCount([]byte(v))
			if s.MinLength != -1 && length < s.MinLength {
				errors = append(errors, b.errorf("minLength", "length must be >= %d, but got %d", s.MinLength, length))
			}
			if s.MaxLength != -1 && length > s.MaxLength {
				errors = append(errors, b.errorf("maxLength", "length must be <= %d, but got %d", s.MaxLength, length))
			}
		}
		if s.Pattern != nil && !b.match(s.Pattern, v) {
			errors = append(errors, b.errorf("pattern", "does not match pattern %q", s.Pattern))
		}

		decoded := s.ContentEncoding == ""
		var content []byte
		if s.decoder != nil {
			decodedBytes, err := s.decoder(v)
			if err != nil {
				errors = append(errors, b.errorf("contentEncoding", "%q is not %s encoded", v, s.ContentEncoding))
			} else {
				content, decoded = decodedBytes, true
			}
		}
		if decoded && s.mediaType != nil {
			if s.decoder == nil {
				content = []byte(v)
			}
			if err := s.mediaType(content); err != nil {
				errors = append(errors, b.errorf("contentMediaType", "value is not of mediatype %q", s.ContentMediaType))
			}
		}

	case json.Number, float64, int, int32, int64:
		num := b.number(v)
		if num == nil && (s.Minimum != nil || s.ExclusiveMinimum != nil || s.Maximum != nil || s.ExclusiveMaximum != nil || s.MultipleOf != nil) {
			panic(InvalidJSONTypeError("number outside supported range"))
		}
		if s.Minimum != nil && num.Cmp(s.Minimum) < 0 {
			errors = append(errors, b.errorf("minimum", "must be >= %v but found %v", s.Minimum, v))
		}
		if s.ExclusiveMinimum != nil && num.Cmp(s.ExclusiveMinimum) <= 0 {
			errors = append(errors, b.errorf("exclusiveMinimum", "must be > %v but found %v", s.ExclusiveMinimum, v))
		}
		if s.Maximum != nil && num.Cmp(s.Maximum) > 0 {
			errors = append(errors, b.errorf("maximum", "must be <= %v but found %v", s.Maximum, v))
		}
		if s.ExclusiveMaximum != nil && num.Cmp(s.ExclusiveMaximum) >= 0 {
			errors = append(errors, b.errorf("exclusiveMaximum", "must be < %v but found %v", s.ExclusiveMaximum, v))
		}
		if s.MultipleOf != nil {
			if q := new(big.Float).Quo(num, s.MultipleOf); !q.IsInt() {
				errors = append(errors, b.errorf("multipleOf", "%v not multipleOf %v", v, s.MultipleOf))
			}
		}
	}

	for name, cs := range s.Extensions {
		b.spend(1)
		validate := s.extensions[name]
		if err := validate(ValidationContext{budget: b}, cs, v); err != nil {
			errors = append(errors, err)
		}
	}

	b.spend(0)
	switch len(errors) {
	case 0:
		return nil
	case 1:
		return errors[0]
	default:
		return b.errorf("", "validation failed").add(errors...)
	}
}

// jsonType returns the json type of given value v.
//
// It panics if the given value is not valid json value
func jsonType(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number, float64, int, int32, int64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	}
	panic(InvalidJSONTypeError(fmt.Sprintf("%T", v)))
}

// equals tells if given two json values are equal or not.
func (b *budget) equal(v1, v2 interface{}) bool {
	b.enter()
	defer b.leave()
	b.valueWork(v1)
	b.valueWork(v2)
	v1Type := jsonType(v1)
	if v1Type != jsonType(v2) {
		return false
	}
	switch v1Type {
	case "array":
		arr1, arr2 := v1.([]interface{}), v2.([]interface{})
		if len(arr1) != len(arr2) {
			return false
		}
		for i := range arr1 {
			if !b.equal(arr1[i], arr2[i]) {
				return false
			}
		}
		return true
	case "object":
		obj1, obj2 := v1.(map[string]interface{}), v2.(map[string]interface{})
		if len(obj1) != len(obj2) {
			return false
		}
		for k, v1 := range obj1 {
			if v2, ok := obj2[k]; ok {
				if !b.equal(v1, v2) {
					return false
				}
			} else {
				return false
			}
		}
		return true
	case "number":
		num1 := b.number(v1)
		num2 := b.number(v2)
		if num1 == nil || num2 == nil {
			panic(InvalidJSONTypeError("number outside supported range"))
		}
		return num1.Cmp(num2) == 0
	default:
		return v1 == v2
	}
}

// escape converts given token to valid json-pointer token
func escape(token string) string {
	token = strings.Replace(token, "~", "~0", -1)
	token = strings.Replace(token, "/", "~1", -1)
	return url.PathEscape(token)
}
