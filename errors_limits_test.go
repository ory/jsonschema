package jsonschema

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidationLimitsPreservePointers(t *testing.T) {
	t.Parallel()
	name := strings.Repeat("a", 258) + "/~"
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprintf("required=%t", required), func(t *testing.T) {
			t.Parallel()
			c := NewCompiler()
			c.Limits = &Limits{}
			document := map[string]interface{}{"type": "object", "properties": map[string]interface{}{name: map[string]interface{}{"type": "string"}}}
			value := map[string]interface{}{name: 42}
			if required {
				document["required"] = []string{name}
				value = map[string]interface{}{}
			}
			raw, err := json.Marshal(document)
			require.NoError(t, err)
			require.NoError(t, c.AddResource("schema.json", strings.NewReader(string(raw))))
			s, err := c.Compile(t.Context(), "schema.json")
			require.NoError(t, err)
			var validation *ValidationError
			require.ErrorAs(t, s.ValidateInterfaceContext(t.Context(), value), &validation)
			pointer := "#/" + strings.Repeat("a", 258) + "~1~0"
			if required {
				require.Equal(t, "#", validation.InstancePtr)
				require.Equal(t, "#/required", validation.SchemaPtr)
				require.Equal(t, []string{pointer}, validation.Context.(*ValidationErrorContextRequired).Missing)
			} else {
				require.Equal(t, pointer, validation.InstancePtr)
				require.Equal(t, "#/properties/"+strings.Repeat("a", 258)+"~1~0/type", validation.SchemaPtr)
			}
		})
	}
}

func TestValidationLimitsPreserveRequiredNames(t *testing.T) {
	t.Parallel()
	names := make([]string, 12)
	pointers := make([]string, len(names))
	for i := range names {
		names[i] = fmt.Sprintf("field%d", i)
		pointers[i] = "#/" + names[i]
	}
	raw, err := json.Marshal(map[string]interface{}{"required": names})
	require.NoError(t, err)
	c := NewCompiler()
	c.Limits = &Limits{}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(string(raw))))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	var validation *ValidationError
	require.ErrorAs(t, s.ValidateInterfaceContext(t.Context(), map[string]interface{}{}), &validation)
	require.Equal(t, pointers, validation.Context.(*ValidationErrorContextRequired).Missing)
}

func TestValidationPointerWorkLimit(t *testing.T) {
	t.Parallel()
	name := strings.Repeat("p", 512)
	var document interface{} = map[string]interface{}{"type": "string"}
	var value interface{} = 42
	for i := 0; i < 24; i++ {
		document = map[string]interface{}{"properties": map[string]interface{}{name: document}}
		value = map[string]interface{}{name: value}
	}
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	c := NewCompiler()
	c.Limits = &Limits{}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(string(raw))))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	s.limits.MaxWork = 100000
	require.ErrorIs(t, s.ValidateInterfaceContext(t.Context(), value), ErrResourceLimit)
}

func newNestedValidationError(t *testing.T, depth int) *ValidationError {
	t.Helper()
	name := strings.Repeat("p", 260)
	definitions := make(map[string]interface{}, depth)
	for i := 0; i < depth; i++ {
		var next interface{} = false
		if i+1 < depth {
			next = map[string]interface{}{"$ref": "#/definitions/" + name + fmt.Sprint(i+1)}
		}
		definitions[name+fmt.Sprint(i)] = next
	}
	raw, err := json.Marshal(map[string]interface{}{
		"definitions": definitions,
		"properties":  map[string]interface{}{name: map[string]interface{}{"$ref": "#/definitions/" + name + "0"}},
	})
	require.NoError(t, err)
	c := NewCompiler()
	c.Limits = &Limits{MaxDepth: 512}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(string(raw))))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	var validation *ValidationError
	require.ErrorAs(t, s.ValidateInterfaceContext(t.Context(), map[string]interface{}{name: true}), &validation)
	return validation
}

func TestValidationDiagnosticRenderingLimits(t *testing.T) {
	small := newNestedValidationError(t, 40)
	large := newNestedValidationError(t, 80)
	smallAllocations := testing.AllocsPerRun(3, func() { _ = small.Error() })
	largeAllocations := testing.AllocsPerRun(3, func() { _ = large.Error() })
	t.Logf("40 levels: %.0f allocations; 80 levels: %.0f allocations", smallAllocations, largeAllocations)
	require.LessOrEqual(t, len(large.Error()), 64<<10)
	require.Less(t, largeAllocations, 3*smallAllocations)
	require.Less(t, largeAllocations, float64(1000))
}

func TestValidationDiagnosticShortFormat(t *testing.T) {
	t.Parallel()
	var rendered []string
	for _, limits := range []*Limits{nil, {}} {
		c := NewCompiler()
		c.Limits = limits
		require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"properties":{"outer":{"allOf":[{"type":"string"}]}}}`)))
		s, err := c.Compile(t.Context(), "schema.json")
		require.NoError(t, err)
		err = s.ValidateInterfaceContext(t.Context(), map[string]interface{}{"outer": false})
		require.Error(t, err)
		rendered = append(rendered, err.Error())
	}
	require.Equal(t, rendered[0], rendered[1])
}
