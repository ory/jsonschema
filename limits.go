package jsonschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
)

// ErrResourceLimit identifies an operation that exceeds its configured budget.
var ErrResourceLimit = errors.New("jsonschema: resource limit exceeded")

// ResourceLimitError identifies a budget without including schema or instance data.
type ResourceLimitError struct {
	Kind string
	// Validation reports that the limit was reached while validating an
	// instance, so the instance rather than the schema exceeded it.
	Validation bool
}

func (e *ResourceLimitError) Error() string { return ErrResourceLimit.Error() + ": " + e.Kind }
func (e *ResourceLimitError) Unwrap() error { return ErrResourceLimit }

// Limits bounds one compilation or validation operation and is safe to copy.
// Zero fields use the documented defaults when Compiler.Limits is non-nil.
// Extension callbacks and resource readers must also honor cancellation and bound their own work.
type Limits struct {
	// OnLimit receives each exceeded limit once per operation; returning nil continues evaluation.
	// Returning an error aborts evaluation or applies the exceeded diagnostic display cap.
	// The callback must support concurrent operations and receives their current context.
	OnLimit func(context.Context, error) error
	// MaxDepth limits nested traversal and evaluation calls, defaulting to 128.
	MaxDepth int
	// MaxNodes limits newly compiled schemas, defaulting to 10,000.
	MaxNodes int
	// MaxWork limits cumulative traversal, input, diagnostic byte, and matching work, defaulting to 10,000,000.
	MaxWork int
	// MaxRegexInstructions limits cumulative estimated regex instructions, defaulting to 100,000.
	MaxRegexInstructions int
	// MaxErrors limits constructed errors, including conditional branch failures, defaulting to 1,000.
	MaxErrors int
}

func normalizedLimits(l *Limits) *Limits {
	if l == nil {
		return nil
	}
	v := *l
	if v.MaxDepth <= 0 {
		v.MaxDepth = 128
	}
	if v.MaxNodes <= 0 {
		v.MaxNodes = 10000
	}
	if v.MaxWork <= 0 {
		v.MaxWork = 10000000
	}
	if v.MaxRegexInstructions <= 0 {
		v.MaxRegexInstructions = 100000
	}
	if v.MaxErrors <= 0 {
		v.MaxErrors = 1000
	}
	return &v
}

type budgetKey struct{}
type budgetAbort struct{ err error }
type compiledRegex struct {
	expression *regexp.Regexp
	cost       int
}
type budget struct {
	ctx                                           context.Context
	limits                                        *Limits
	depth, nodes, work, regexInstructions, errors int
	resources                                     map[*resource]bool
	ids                                           map[*resource]map[string]map[string]interface{}
	regexps                                       map[string]compiledRegex
	reported                                      map[string]error
	validation                                    bool
}

func newBudget(ctx context.Context, limits *Limits) *budget {
	return &budget{ctx: ctx, limits: normalizedLimits(limits), resources: make(map[*resource]bool), regexps: make(map[string]compiledRegex), reported: make(map[string]error)}
}

func compilationBudget(ctx context.Context) *budget { return ctx.Value(budgetKey{}).(*budget) }

func (b *budget) limit(kind string) error {
	if err, ok := b.reported[kind]; ok {
		return err
	}
	var err error = &ResourceLimitError{Kind: kind, Validation: b.validation}
	if b.limits.OnLimit != nil {
		err = b.limits.OnLimit(b.ctx, err)
	}
	b.reported[kind] = err
	return err
}
func (b *budget) stop(kind string) {
	if err := b.limit(kind); err != nil {
		panic(budgetAbort{err})
	}
	if err := b.ctx.Err(); err != nil {
		panic(budgetAbort{err})
	}
}

func saturatedAdd(a, b int) int {
	if b < 0 || a > math.MaxInt-b {
		return math.MaxInt
	}
	return a + b
}
func saturatedMultiply(a, b int) int {
	if a != 0 && b > math.MaxInt/a {
		return math.MaxInt
	}
	return a * b
}
func (b *budget) spend(n int) {
	if err := b.ctx.Err(); err != nil {
		panic(budgetAbort{err})
	}
	if b.limits == nil {
		return
	}
	if n < 0 || n > b.limits.MaxWork-b.work {
		b.stop("work")
	}
	b.work = saturatedAdd(b.work, n)
}
func (b *budget) enter() {
	b.spend(1)
	if b.limits != nil && b.depth >= b.limits.MaxDepth {
		b.stop("depth")
	}
	b.depth++
}
func (b *budget) leave() { b.depth-- }
func (b *budget) node() {
	b.spend(1)
	if b.limits != nil && b.nodes >= b.limits.MaxNodes {
		b.stop("compiled nodes")
	}
	b.nodes = saturatedAdd(b.nodes, 1)
}

func (b *budget) scan(v interface{}) {
	if b.limits == nil {
		b.spend(0)
		return
	}
	b.enter()
	defer b.leave()
	switch v := v.(type) {
	case map[string]interface{}:
		b.spend(len(v))
		for k, item := range v {
			b.spend(len(k))
			b.scan(item)
		}
	case []interface{}:
		b.spend(len(v))
		for _, item := range v {
			b.scan(item)
		}
	case string:
		b.spend(len(v))
	case json.Number:
		b.spend(len(v))
	}
}

// regex compiles pattern and returns its estimated matching cost per input byte,
// which is zero without limits.
func (b *budget) regex(pattern string) (*regexp.Regexp, int, error) {
	b.spend(len(pattern))
	if cached, ok := b.regexps[pattern]; ok {
		return cached.expression, cached.cost, nil
	}
	if b.limits == nil {
		expression, err := regexp.Compile(pattern)
		return expression, 0, err
	}
	tree, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, 0, err
	}
	cost := b.regexCost(tree)
	if cost >= b.limits.MaxRegexInstructions-b.regexInstructions {
		b.stop("regex instructions")
	}
	cost = saturatedAdd(cost, 1)
	b.regexInstructions = saturatedAdd(b.regexInstructions, cost)
	b.spend(cost)
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return nil, 0, err
	}
	b.regexps[pattern] = compiledRegex{expression, cost}
	return expression, cost, nil
}

func (b *budget) regexCost(tree *syntax.Regexp) int {
	b.enter()
	defer b.leave()
	limit := b.limits.MaxRegexInstructions
	cost := 1
	for _, sub := range tree.Sub {
		n := b.regexCost(sub)
		if n > limit-cost {
			b.stop("regex instructions")
		}
		cost = saturatedAdd(cost, n)
	}
	extra := 0
	switch tree.Op {
	case syntax.OpLiteral:
		extra = len(tree.Rune)
	case syntax.OpCapture:
		extra = 2
	case syntax.OpAlternate:
		extra = len(tree.Sub)
	case syntax.OpRepeat:
		repeats := tree.Max
		if repeats == -1 {
			repeats = tree.Min + 1
		}
		if repeats != 0 && cost > limit/repeats {
			b.stop("regex instructions")
		}
		cost = saturatedMultiply(cost, repeats)
		if repeats == 0 {
			cost = 1
		}
	}
	if extra > limit-cost {
		b.stop("regex instructions")
	}
	return saturatedAdd(cost, extra)
}

// match charges matching work by the cost recorded when the pattern was compiled,
// and costs the pattern again only when compilation recorded none.
func (b *budget) match(pattern *regexp.Regexp, cost int, value string) bool {
	if b.limits != nil {
		if cost == 0 {
			var err error
			if _, cost, err = b.regex(pattern.String()); err != nil {
				panic(err)
			}
		}
		if len(value) > (b.limits.MaxWork-b.work)/cost {
			b.stop("regex matching")
		}
		b.spend(saturatedMultiply(cost, len(value)))
	} else {
		b.spend(1)
	}
	return pattern.MatchString(value)
}

func (b *budget) errorf(schemaPtr, format string, args ...interface{}) *ValidationError {
	b.spend(1)
	b.spend(len(schemaPtr))
	b.spend(len(format))
	if b.limits != nil && b.errors >= b.limits.MaxErrors {
		b.stop("errors")
	}
	b.errors = saturatedAdd(b.errors, 1)
	if b.limits != nil {
		for i, arg := range args {
			switch arg := arg.(type) {
			case string:
				args[i] = b.detail(arg)
			case json.Number:
				args[i] = json.Number(b.detail(string(arg)))
			case *regexp.Regexp:
				args[i] = b.detail(arg.String())
			case map[string]interface{}, []interface{}:
				if displaySize(arg, maxDiagnosticDetail) > maxDiagnosticDetail {
					args[i] = "<value>"
				}
			}
			if text, ok := args[i].(string); ok {
				b.spend(6 * len(text))
			}
			b.spend(64)
		}
	}
	result := validationErrorf(schemaPtr, format, args...)
	result.Message = b.detail(result.Message)
	result.budget = b
	return result
}

func (b *budget) duplicate(values []interface{}) (int, int, bool) {
	b.spend(len(values))
	scalars := make(map[interface{}]int)
	var complex []int
	for i, value := range values {
		b.spend(1)
		switch jsonType(value) {
		case "array", "object":
			for _, previous := range complex {
				if b.equal(values[previous], value) {
					return previous, i, true
				}
			}
			complex = append(complex, i)
		default:
			key := value
			if jsonType(value) == "number" {
				// Numbers outside the big.Float range keep their literal key, matching equal.
				if parsed := b.number(value); parsed != nil {
					key = numericKey(parsed.Text('x', -1))
					if parsed.Sign() == 0 {
						key = numericKey("0")
					}
				}
			} else if text, ok := value.(string); ok {
				b.spend(len(text))
			}
			if previous, ok := scalars[key]; ok {
				return previous, i, true
			}
			scalars[key] = i
		}
	}
	return 0, 0, false
}

type numericKey string

// Diagnostic rendering bounds cap how much of a schema or instance an error
// message repeats. They truncate a message and never fail an operation.
const (
	maxDiagnosticDetail = 20 << 10
	maxDiagnosticNames  = 150
	maxDiagnosticBytes  = 128 << 10
	maxDiagnosticNodes  = 1000
	maxDiagnosticDepth  = 128
)

func (b *budget) detail(value string) string {
	if b != nil && b.limits != nil && len(value) > maxDiagnosticDetail {
		return value[:maxDiagnosticDetail] + "..."
	}
	return value
}

// displaySize estimates the bytes rendering v takes. It stops counting once
// the estimate exceeds limit, so its work is bounded by limit.
func displaySize(v interface{}, limit int) int {
	size := 2
	switch v := v.(type) {
	case map[string]interface{}:
		for key, item := range v {
			if size > limit {
				break
			}
			size += len(key) + 2 + displaySize(item, limit-size)
		}
	case []interface{}:
		for _, item := range v {
			if size > limit {
				break
			}
			size += 2 + displaySize(item, limit-size)
		}
	case string:
		size += len(v)
	case json.Number:
		size += len(v)
	default:
		size += 3
	}
	return size
}

func (b *budget) requiredError(missing []string) *ValidationError {
	displayCount := len(missing)
	if b.limits != nil && displayCount > maxDiagnosticNames {
		displayCount = maxDiagnosticNames
	}
	b.spend(displayCount)
	displayed := make([]string, displayCount)
	for i, property := range missing {
		if i < displayCount {
			text := b.detail(property)
			b.spend(6*len(text) + 2)
			displayed[i] = strconv.Quote(text)
		}
		missing[i] = b.escape(property)
	}
	for _, text := range displayed {
		b.spend(len(text) + 2)
	}
	result := b.errorf("required", "missing properties: %s", strings.Join(displayed, ", "))
	result.Context = &ValidationErrorContextRequired{Missing: missing}
	return result
}

func (b *budget) joinPtr(first, second string) string {
	if b != nil {
		b.spend(len(first))
		b.spend(len(second))
		b.spend(1)
	}
	return joinPtr(first, second)
}

func (b *budget) escape(value string) string {
	if b != nil {
		for i := 0; i < 8; i++ {
			b.spend(len(value))
		}
	}
	return escape(value)
}

func (b *budget) valueWork(v interface{}) {
	switch v := v.(type) {
	case string:
		b.spend(len(v))
	case json.Number:
		b.spend(len(v))
	case map[string]interface{}:
		b.spend(len(v))
	case []interface{}:
		b.spend(len(v))
	}
}

func (b *budget) number(value interface{}) *big.Float {
	parsed, ok := new(big.Float).SetString(b.numberText(value))
	if !ok && b.limits != nil {
		b.stop("number range")
	}
	return parsed
}

func (b *budget) numberText(value interface{}) string {
	text := fmt.Sprint(value)
	size := len(text)
	if b.limits != nil && size > 0 {
		if size > (b.limits.MaxWork-b.work)/size {
			b.stop("number parsing")
		}
		b.spend(saturatedMultiply(size, size))
	} else {
		b.spend(size)
	}
	return text
}
