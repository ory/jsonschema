package jsonschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitPolicyCompileContinues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		limits       Limits
		kind         string
	}{
		{"nodes", `{"allOf":[true,true,true]}`, Limits{MaxNodes: 1}, "compiled nodes"},
		{"depth", `{"allOf":[{"allOf":[true]}]}`, Limits{MaxDepth: 1}, "depth"},
		{"work", `{"type":"string"}`, Limits{MaxWork: 1}, "work"},
		{"regex", `{"pattern":"(ab){20}"}`, Limits{MaxRegexInstructions: 1}, "regex instructions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observed := map[string]int{}
			tc.limits.OnLimit = func(ctx context.Context, err error) error {
				require.Equal(t, t.Context(), ctx)
				var limit *ResourceLimitError
				require.ErrorAs(t, err, &limit)
				observed[limit.Kind]++
				return nil
			}
			c := NewCompiler()
			c.Limits = &tc.limits
			require.NoError(t, c.AddResource("schema.json", strings.NewReader(tc.schema)))
			s, err := c.Compile(t.Context(), "schema.json")
			require.NoError(t, err)
			require.NotNil(t, s)
			require.Equal(t, 1, observed[tc.kind])
			for _, count := range observed {
				require.Equal(t, 1, count)
			}
		})
	}
}

func TestLimitPolicyValidationContinues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		value        interface{}
		limits       Limits
		kind         string
		invalid      bool
	}{
		{"work", `{"type":"string"}`, "valid", Limits{MaxWork: 1}, "work", false},
		{"depth", `true`, []interface{}{[]interface{}{nil}}, Limits{MaxDepth: 1}, "depth", false},
		{"errors", `{"anyOf":[{"type":"number"},{"type":"number"},true]}`, "valid", Limits{MaxErrors: 1}, "errors", false},
		{"semantic error", `{"items":{"type":"string"}}`, []interface{}{nil, nil, nil}, Limits{MaxErrors: 1}, "errors", true},
		{"regex instructions", `{"pattern":"(ab){20}"}`, strings.Repeat("ab", 20), Limits{MaxRegexInstructions: 1}, "regex instructions", false},
		{"regex matching", `{"pattern":"(a){32}"}`, strings.Repeat("a", 32), Limits{MaxWork: 100}, "regex matching", false},
		{"number parsing", `{"minimum":0}`, json.Number(strings.Repeat("9", 128)), Limits{MaxWork: 1000}, "number parsing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := CompileString(t.Context(), "schema.json", tc.schema)
			require.NoError(t, err)
			observed := map[string]int{}
			tc.limits.OnLimit = func(ctx context.Context, err error) error {
				require.Equal(t, t.Context(), ctx)
				var limit *ResourceLimitError
				require.ErrorAs(t, err, &limit)
				observed[limit.Kind]++
				return nil
			}
			s.limits = normalizedLimits(&tc.limits)
			err = s.ValidateInterfaceContext(t.Context(), tc.value)
			if tc.invalid {
				var validation *ValidationError
				require.ErrorAs(t, err, &validation)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, observed[tc.kind])
			for _, count := range observed {
				require.Equal(t, 1, count)
			}
		})
	}
}

func TestLimitPolicyErrorAndCancellation(t *testing.T) {
	t.Parallel()
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "policy error", true: "cancellation"}[cancel], func(t *testing.T) {
			t.Parallel()
			ctx, stop := context.WithCancel(t.Context())
			t.Cleanup(stop)
			sentinel := errors.New("policy rejected")
			s, err := CompileString(t.Context(), "schema.json", `true`)
			require.NoError(t, err)
			s.limits = &Limits{MaxWork: 1, OnLimit: func(context.Context, error) error {
				if cancel {
					stop()
					return nil
				}
				return sentinel
			}}
			err = s.ValidateInterfaceContext(ctx, []interface{}{nil, nil})
			if cancel {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, sentinel)
			}
		})
	}
}

func TestLimitPolicyPreservesDiagnostics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, schema string
		value        interface{}
		kinds        []string
		contains     string
	}{
		{"detail", `{"const":"` + strings.Repeat("v", 300) + `"}`, "different", []string{"diagnostic detail"}, strings.Repeat("v", 300)},
		{"required", `{"required":["p0","p1","p2","p3","p4","p5","p6","p7","p8"]}`, map[string]interface{}{}, []string{"diagnostic properties"}, "p8"},
		{"render bytes", `{"items":{"type":"string"}}`, make([]interface{}, 1500), []string{"diagnostic nodes", "diagnostic bytes"}, "#/1499"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observed := map[string]int{}
			c := NewCompiler()
			c.Limits = &Limits{MaxErrors: 10000, MaxWork: 100000000, OnLimit: func(_ context.Context, err error) error {
				var limit *ResourceLimitError
				require.ErrorAs(t, err, &limit)
				observed[limit.Kind]++
				return nil
			}}
			require.NoError(t, c.AddResource("schema.json", strings.NewReader(tc.schema)))
			s, err := c.Compile(t.Context(), "schema.json")
			require.NoError(t, err)
			clear(observed)
			err = s.ValidateInterfaceContext(t.Context(), tc.value)
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			for _, kind := range tc.kinds {
				require.Equal(t, 1, observed[kind], kind)
			}
			before := len(observed)
			require.Contains(t, err.Error(), tc.contains)
			require.Equal(t, before, len(observed))
			for _, count := range observed {
				require.Equal(t, 1, count)
			}
		})
	}
}

func TestResourceLimitsSnapshot(t *testing.T) {
	t.Parallel()
	c := NewCompiler()
	c.Limits = &Limits{MaxWork: 12345}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`true`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	snapshot := s.ResourceLimits()
	snapshot.MaxWork = 1
	require.Equal(t, 12345, s.ResourceLimits().MaxWork)
	require.NoError(t, s.ValidateInterfaceContext(t.Context(), "hello"))
}

func TestLimitPolicyNumericRange(t *testing.T) {
	t.Parallel()
	s, err := CompileString(t.Context(), "schema.json", `{"minimum":0}`)
	require.NoError(t, err)
	observed := 0
	s.limits = &Limits{OnLimit: func(_ context.Context, err error) error {
		var limit *ResourceLimitError
		require.ErrorAs(t, err, &limit)
		if limit.Kind == "number range" {
			observed++
		}
		return nil
	}}
	require.NotPanics(t, func() {
		err = s.ValidateContext(t.Context(), strings.NewReader(`1e9999999999999`))
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrResourceLimit)
	})
	require.Equal(t, 1, observed)
}

func TestLimitPolicyUnconstrainedNumericRange(t *testing.T) {
	t.Parallel()
	s, err := CompileString(t.Context(), "schema.json", `{"type":"number"}`)
	require.NoError(t, err)
	s.limits = &Limits{OnLimit: func(context.Context, error) error { return nil }}
	require.NoError(t, s.ValidateContext(t.Context(), strings.NewReader(`1e9999999999999`)))
}

func TestLimitPolicyConcurrentOperationContexts(t *testing.T) {
	t.Parallel()
	type observationKey struct{}
	compiled := 0
	c := NewCompiler()
	c.Limits = &Limits{MaxWork: 1, OnLimit: func(ctx context.Context, err error) error {
		count := ctx.Value(observationKey{}).(*int)
		*count++
		return nil
	}}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"type":"string"}`)))
	s, err := c.Compile(context.WithValue(t.Context(), observationKey{}, &compiled), "schema.json")
	require.NoError(t, err)
	require.Equal(t, 1, compiled)
	for i := 0; i < 8; i++ {
		t.Run("operation", func(t *testing.T) {
			t.Parallel()
			observed := 0
			ctx := context.WithValue(t.Context(), observationKey{}, &observed)
			require.NoError(t, s.ValidateInterfaceContext(ctx, "valid"))
			require.Equal(t, 1, observed)
			require.Equal(t, 1, compiled)
		})
	}
}

func TestLimitPolicyReportsOnlyExceededDiagnostics(t *testing.T) {
	t.Parallel()
	large := make([]interface{}, 32)
	for i := range large {
		large[i] = fmt.Sprintf("%s%02d", strings.Repeat("v", 16), i)
	}
	largeEnum, err := json.Marshal(map[string]interface{}{"enum": large})
	require.NoError(t, err)
	extension := Extension{
		Compile: func(_ CompilerContext, m map[string]interface{}) (interface{}, error) {
			if _, ok := m["unexpected"]; ok {
				return true, nil
			}
			return nil, nil
		},
		Validate: func(ctx ValidationContext, _ interface{}, v interface{}) error {
			return ctx.Error("unexpected", "unexpected %v", v)
		},
	}
	for _, tc := range []struct {
		name, schema string
		value        interface{}
		observed     map[string]int
		contains     string
	}{
		{"small enum", `{"enum":["a","b"]}`, "c", map[string]int{}, `value must be one of "a", "b"`},
		{"large enum", string(largeEnum), "c", map[string]int{"diagnostic enum": 1, "diagnostic detail": 1}, large[31].(string)},
		{"small value", `{"unexpected":true}`, map[string]interface{}{"k": "v"}, map[string]int{}, "map[k:v]"},
		{"large value", `{"unexpected":true}`, large, map[string]int{"diagnostic value": 1, "diagnostic detail": 1}, large[31].(string)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			observed := map[string]int{}
			c := NewCompiler()
			c.Extensions["unexpected"] = extension
			c.Limits = &Limits{OnLimit: func(_ context.Context, err error) error {
				var limit *ResourceLimitError
				require.ErrorAs(t, err, &limit)
				observed[limit.Kind]++
				return nil
			}}
			require.NoError(t, c.AddResource("schema.json", strings.NewReader(tc.schema)))
			s, err := c.Compile(t.Context(), "schema.json")
			require.NoError(t, err)
			err = s.ValidateInterfaceContext(t.Context(), tc.value)
			var validation *ValidationError
			require.ErrorAs(t, err, &validation)
			require.Contains(t, err.Error(), tc.contains)
			require.Equal(t, tc.observed, observed)
		})
	}
}

func TestLimitPolicyValidationReusesCompiledPatterns(t *testing.T) {
	t.Parallel()
	observed := map[string]int{}
	c := NewCompiler()
	c.Limits = &Limits{MaxRegexInstructions: 1, OnLimit: func(_ context.Context, err error) error {
		var limit *ResourceLimitError
		require.ErrorAs(t, err, &limit)
		observed[limit.Kind]++
		return nil
	}}
	require.NoError(t, c.AddResource("schema.json", strings.NewReader(`{"anyOf":[{"type":"string","pattern":"(ab){20}"},{"type":"object","patternProperties":{"(cd){20}":true}}]}`)))
	s, err := c.Compile(t.Context(), "schema.json")
	require.NoError(t, err)
	require.Equal(t, map[string]int{"regex instructions": 1}, observed)
	for _, value := range []interface{}{strings.Repeat("ab", 20), map[string]interface{}{strings.Repeat("cd", 20): true}} {
		clear(observed)
		require.NoError(t, s.ValidateInterfaceContext(t.Context(), value))
		require.Empty(t, observed)
	}
}

func BenchmarkValidatePatterns(b *testing.B) {
	const schema = `{"type":"object","properties":{"email":{"type":"string","pattern":"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"},"name":{"type":"string","pattern":"^[\\p{L} .'-]{1,128}$"}},"patternProperties":{"^x-":{"type":"string"}}}`
	value := map[string]interface{}{"email": "user@example.com", "name": "Jane Doe", "x-source": "import"}
	for _, tc := range []struct {
		name   string
		limits *Limits
	}{
		{"unlimited", nil},
		{"limited", &Limits{OnLimit: func(context.Context, error) error { return nil }}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := NewCompiler()
			c.Limits = tc.limits
			if err := c.AddResource("schema.json", strings.NewReader(schema)); err != nil {
				b.Fatal(err)
			}
			s, err := c.Compile(b.Context(), "schema.json")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := s.ValidateInterfaceContext(b.Context(), value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
