package jsonschema

import (
	"context"
	"encoding/json"
	"regexp/syntax"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileCanceledContext(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"type":"string"}`)))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Compile(ctx, "schema.json")
	require.ErrorIs(t, err, context.Canceled)
}

func TestUniqueItemsReportsOneDuplicate(t *testing.T) {
	t.Parallel()
	s, err := CompileString(t.Context(), "schema.json", `{"uniqueItems":true}`)
	require.NoError(t, err)
	err = s.Validate(strings.NewReader(`[1,1.0,1,1]`))
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, "uniqueItems", strings.TrimPrefix(validation.SchemaPtr, "#/"))
	require.Empty(t, validation.Causes)
}

func TestCompilerLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		limits Limits
		schema string
	}{
		{"nodes", Limits{MaxNodes: 2}, `{"allOf":[true,true,true]}`},
		{"depth", Limits{MaxDepth: 4}, `{"allOf":[{"allOf":[{"allOf":[{"allOf":[true]}]}]}]}`},
		{"work", Limits{MaxWork: 10}, `{"enum":["one","two","three","four","five"]}`},
		{"pattern instructions", Limits{MaxRegexInstructions: 10}, `{"pattern":"(ab){20}"}`},
		{"property pattern instructions", Limits{MaxRegexInstructions: 10}, `{"patternProperties":{"(ab){20}":true}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := NewCompiler()
			c.Limits = &tc.limits
			require.NoError(t, c.AddResource("schema.json", strings.NewReader(tc.schema)))
			_, err := c.Compile(t.Context(), "schema.json")
			require.ErrorIs(t, err, ErrResourceLimit)
		})
	}
}

func TestValidationLimitsErrorCount(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxErrors: 32}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"items":{"type":"string"}}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	require.ErrorIs(t, s.ValidateInterface(make([]interface{}, 100)), ErrResourceLimit)
}

func TestValidationLimitsDepth(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxDepth: 32}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"$ref":"#/definitions/item","definitions":{"item":{"properties":{"next":{"$ref":"#/definitions/item"}}}}}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	var value interface{} = nil
	for i := 0; i < 40; i++ {
		value = map[string]interface{}{"next": value}
	}
	require.ErrorIs(t, s.ValidateInterface(value), ErrResourceLimit)
}

func TestCompilerLimitsFailedCache(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxNodes: 2}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"allOf":[true,true,true]}`)))
	for i := 0; i < 2; i++ {
		_, err := c.Compile(t.Context(), "schema.json")
		require.ErrorIs(t, err, ErrResourceLimit)
	}
}

func TestUniqueItemsScalarWork(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxWork: 20000}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"uniqueItems":true}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	values := make([]interface{}, 1000)
	for i := range values {
		values[i] = json.Number(strconv.Itoa(i))
	}
	require.NoError(t, s.ValidateInterface(values))
}

func TestUniqueItemsNumericRepresentations(t *testing.T) {
	t.Parallel()
	s, err := CompileString(t.Context(), "schema.json", `{"uniqueItems":true}`)
	require.NoError(t, err)
	for _, value := range []interface{}{json.Number("1.0"), float64(1), int(1), int32(1), int64(1)} {
		require.NotPanics(t, func() {
			var validation *ValidationError
			require.ErrorAs(t, s.ValidateInterface([]interface{}{json.Number("1"), value}), &validation)
			require.Equal(t, "#/uniqueItems", validation.SchemaPtr)
		})
	}
}

func TestValidationErrorDetailsBounded(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{}
	address := "schema-" + strings.Repeat("a", 4096) + ".json"
	value := strings.Repeat("v", 4096)
	require.NoError(t, c.AddResource(address, strings.NewReader(`{"const":"`+value+`"}`)))
	s, err := c.Compile(t.Context(), address)
	require.NoError(t, err)
	err = s.ValidateInterface("different")
	require.Error(t, err)
	require.Less(t, len(err.Error()), 2048)
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	require.Equal(t, s.URL, validation.SchemaURL)
}

func TestCompileContainerDepth256(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{`{"properties":{"next":`, `{"allOf":[`} {
		suffix := "}}"
		if strings.Contains(prefix, "allOf") {
			suffix = "]}"
		}
		c := NewCompiler()
		c.Limits = &Limits{MaxDepth: 1024}
		require.NoError(t, c.AddResource("schema.json", strings.NewReader(strings.Repeat(prefix, 127)+`true`+strings.Repeat(suffix, 127))))
		_, err := c.Compile(t.Context(), "schema.json")
		require.NoError(t, err)
	}
}

func TestValidationCycleLimitsAcrossBranches(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{
		`{"$ref":"#"}`,
		`{"not":{"$ref":"#"}}`,
		`{"anyOf":[{"$ref":"#"},true]}`,
		`{"if":{"$ref":"#"},"then":true,"else":true}`,
	} {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			c := NewCompiler()
			c.Limits = &Limits{MaxDepth: 32}
			require.NoError(t, c.AddResource("schema.json", strings.NewReader(schema)))
			s, err := c.Compile(t.Context(), "schema.json")
			require.NoError(t, err)
			require.ErrorIs(t, s.ValidateInterface(nil), ErrResourceLimit)
		})
	}
}

func TestValidationLimitsSharedReferences(t *testing.T) {
	t.Parallel()
	definitions := map[string]interface{}{"d0": true}
	for i := 1; i <= 16; i++ {
		ref := map[string]interface{}{"$ref": "#/definitions/d" + strconv.Itoa(i-1)}
		definitions["d"+strconv.Itoa(i)] = map[string]interface{}{"allOf": []interface{}{ref, ref}}
	}
	doc, err := json.Marshal(map[string]interface{}{"definitions": definitions, "$ref": "#/definitions/d16"})
	require.NoError(t, err)
	c := NewCompiler()
	c.Limits = &Limits{MaxDepth: 256, MaxWork: 100000}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(string(doc))))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	require.ErrorIs(t, s.ValidateInterface(nil), ErrResourceLimit)
}

func TestValidationCanceledContext(t *testing.T) {
	t.Parallel()
	s, err := CompileString(t.Context(), "schema.json", `true`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, s.ValidateInterfaceContext(ctx, nil), context.Canceled)
	require.ErrorIs(t, s.ValidateContext(ctx, strings.NewReader("null")), context.Canceled)
}

func TestCompilerLimitsAcrossResources(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxNodes: 8}
	for i := 0; i < 6; i++ {
		schema := `{"allOf":[true,{"$ref":"` + strconv.Itoa(i+1) + `.json"}]}`
		if i == 5 {
			schema = `true`
		}
		require.NoError(t, c.AddResource(strconv.Itoa(i)+".json", strings.NewReader(schema)))
	}
	_, err := c.Compile(t.Context(), "0.json")
	require.ErrorIs(t, err, ErrResourceLimit)
}

func TestUniqueItemsComplexWork(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxWork: 20000}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"uniqueItems":true}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	values := make([]interface{}, 200)
	for i := range values {
		values[i] = []interface{}{json.Number(strconv.Itoa(i))}
	}
	require.ErrorIs(t, s.ValidateInterface(values), ErrResourceLimit)
}

func TestCompilerErrorDetailsBounded(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{}
	address := "schema-" + strings.Repeat("a", 4096) + ".json"
	require.NoError(t, c.AddResource(address, strings.NewReader(`{"type":123}`)))
	_, err := c.Compile(t.Context(), address)
	require.Error(t, err)
	require.Less(t, len(err.Error()), 2048)
}

func TestCompilationCancellationFromExtension(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	c := NewCompiler()
	c.Extensions["cancel"] = Extension{Compile: func(_ CompilerContext, _ map[string]interface{}) (interface{}, error) { cancel(); return nil, nil }}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{}`)))
	_, err := c.Compile(ctx, "schema.json")
	require.ErrorIs(t, err, context.Canceled)
}

func TestValidationCancellationFromExtension(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	c := NewCompiler()
	c.Extensions["cancel"] = Extension{
		Compile:  func(_ CompilerContext, _ map[string]interface{}) (interface{}, error) { return true, nil },
		Validate: func(_ ValidationContext, _ interface{}, _ interface{}) error { cancel(); return nil },
	}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	require.ErrorIs(t, s.ValidateInterfaceContext(ctx, nil), context.Canceled)
}

func TestValidationRepeatedStringWork(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxWork: 10000}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"allOf":[{"minLength":1},{"minLength":1},{"minLength":1}]}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	require.ErrorIs(t, s.ValidateInterface(strings.Repeat("a", 4000)), ErrResourceLimit)
}

func TestLimitsSharedWithExtension(t *testing.T) {
	t.Parallel()
	extension := Extension{
		Compile: func(ctx CompilerContext, m map[string]interface{}) (interface{}, error) {
			child, ok := m["child"]
			if !ok {
				return nil, nil
			}
			return ctx.Compile(t.Context(), child)
		},
		Validate: func(ctx ValidationContext, compiled interface{}, value interface{}) error {
			return ctx.Validate(compiled.(*Schema), value)
		},
	}
	for _, tc := range []struct {
		name, schema string
		limits       Limits
		compile      bool
	}{
		{"compilation", `{"child":{"child":{"child":true}}}`, Limits{MaxNodes: 2}, true},
		{"validation", `{"child":{"child":{"items":{"type":"string"}}}}`, Limits{MaxErrors: 32}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := NewCompiler()
			c.Limits = &tc.limits
			c.Extensions["child"] = extension
			require.NoError(t, c.AddResource("schema.json", strings.NewReader(tc.schema)))
			s, err := c.Compile(t.Context(), "schema.json")
			if tc.compile {
				require.ErrorIs(t, err, ErrResourceLimit)
			} else {
				require.NoError(t, err)
				require.ErrorIs(t, s.ValidateInterface(make([]interface{}, 100)), ErrResourceLimit)
			}
		})
	}
}

func TestUniqueItemsScientificNumbers(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"uniqueItems":true}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	for _, values := range []string{`[1e1000000,10e999999]`, `[0,-0.0]`, `[1,1.0]`} {
		var validation *ValidationError
		require.ErrorAs(t, s.Validate(strings.NewReader(values)), &validation)
		require.Equal(t, "#/uniqueItems", validation.SchemaPtr)
	}
	require.NoError(t, s.Validate(strings.NewReader(`[1e1000000,1e999999,"1e1000000"]`)))
}

func TestCompiledLimitsSnapshot(t *testing.T) {
	t.Parallel()
	limits := &Limits{MaxErrors: 32}
	c := NewCompiler()
	c.Limits = limits
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"items":{"type":"string"}}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	limits.MaxErrors = 1000
	require.ErrorIs(t, s.ValidateInterface(make([]interface{}, 100)), ErrResourceLimit)
}

func TestCompilerUnknownDraftDetailsBounded(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"$schema":"`+strings.Repeat("s", 4096)+`"}`)))
	_, err := c.Compile(t.Context(), "schema.json")
	require.Error(t, err)
	require.Less(t, len(err.Error()), 1024)
}

func TestCompilerExtensionChildCancellation(t *testing.T) {
	t.Parallel()
	child, cancel := context.WithCancel(t.Context())
	cancel()
	c := NewCompiler()
	c.Extensions["child"] = Extension{Compile: func(ctx CompilerContext, _ map[string]interface{}) (interface{}, error) {
		return ctx.Compile(child, true)
	}}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{}`)))
	_, err := c.Compile(t.Context(), "schema.json")
	require.ErrorIs(t, err, context.Canceled)
}

func TestCompilerCumulativeRegexInstructions(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxRegexInstructions: 6}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"allOf":[{"pattern":"aa"},{"pattern":"bb"}]}`)))
	_, err := c.Compile(t.Context(), "schema.json")
	require.ErrorIs(t, err, ErrResourceLimit)
}

func TestValidationRegexMatchingWork(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxWork: 2000}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"pattern":"(a){32}"}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	require.ErrorIs(t, s.ValidateInterface(strings.Repeat("a", 32)), ErrResourceLimit)
}

func TestCompilerRegexInstructionUpperBound(t *testing.T) {
	t.Parallel()
	pattern := `[a-c]*|[0-9]*|[x-z]*`
	tree, err := syntax.Parse(pattern, syntax.Perl)
	require.NoError(t, err)
	program, err := syntax.Compile(tree.Simplify())
	require.NoError(t, err)
	c := NewCompiler()
	c.Limits = &Limits{MaxRegexInstructions: len(program.Inst) - 1}
	doc, err := json.Marshal(map[string]interface{}{"pattern": pattern})
	require.NoError(t, err)
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(string(doc))))
	_, err = c.Compile(t.Context(), "schema.json")
	require.ErrorIs(t, err, ErrResourceLimit)
}

func TestCompilerNumericRange(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"minimum":1e9999999999999}`)))
	_, err := c.Compile(t.Context(), "schema.json")
	require.ErrorIs(t, err, ErrResourceLimit)
}

func TestValidationNumericRange(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"minimum":0}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	require.NotPanics(t, func() {
		require.ErrorIs(t, s.Validate(strings.NewReader(`1e9999999999999`)), ErrResourceLimit)
	})
}

func TestNumericParsingWork(t *testing.T) {
	t.Parallel()
	for _, schema := range []string{`{"minimum":0}`, `{"type":"integer"}`} {
		c := NewCompiler()
		c.Limits = &Limits{MaxWork: 10000}
		require.NoError(t, c.AddResource("schema.json", strings.NewReader(schema)))
		s, err := c.Compile(t.Context(), "schema.json")
		require.NoError(t, err)
		require.ErrorIs(t, s.Validate(strings.NewReader(strings.Repeat("9", 128))), ErrResourceLimit)
	}
}
