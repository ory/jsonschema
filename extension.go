// Copyright 2017 Santhosh Kumar Tekuri. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonschema

import "context"

// Extension is used to define additional keywords to standard jsonschema.
// An extension can implement more than one keyword.
//
// Extensions are registered in Compiler.Extensions map.
type Extension struct {
	// Meta captures the metaschema for the new keywords.
	// This is used to validate the schema before calling Compile.
	Meta *Schema

	// Compile compiles the schema m and returns its compiled representation.
	// if the schema m does not contain the keywords defined by this extension,
	// compiled representation nil should be returned.
	Compile func(ctx CompilerContext, m map[string]interface{}) (interface{}, error)

	// Validate validates the json value v with compiled representation s.
	// This is called only when compiled representation is not nil. Returned
	// error must be *ValidationError
	Validate func(ctx ValidationContext, s interface{}, v interface{}) error
}

// CompilerContext provides additional context required in compiling for extension.
type CompilerContext struct {
	c      *Compiler
	r      *resource
	base   string
	budget *budget
}

// Compile compiles given value v into *Schema. This is useful in implementing
// keyword like allOf/oneOf
func (ctx CompilerContext) Compile(c context.Context, v interface{}) (*Schema, error) {
	if err := c.Err(); err != nil {
		panic(budgetAbort{err})
	}
	return ctx.c.compile(context.WithValue(c, budgetKey{}, ctx.budget), ctx.r, nil, ctx.base, v)
}

// CompileRef compiles the schema referenced by ref uri
func (ctx CompilerContext) CompileRef(c context.Context, ref string) (*Schema, error) {
	if err := c.Err(); err != nil {
		panic(budgetAbort{err})
	}
	b, _ := split(ctx.base)
	return ctx.c.compileRef(context.WithValue(c, budgetKey{}, ctx.budget), ctx.r, b, ref)
}

// ValidationContext provides additional context required in validating for extension.
type ValidationContext struct{ budget *budget }

// Validate validates schema s with value v. Extension must use this method instead of
// *Schema.ValidateInterface method. This will be useful in implementing keywords like
// allOf/oneOf
func (ctx ValidationContext) Validate(s *Schema, v interface{}) error {
	return s.validate(v, ctx.budget)
}

// Error used to construct validation error by extensions. schemaPtr is relative json pointer.
func (ctx ValidationContext) Error(schemaPtr string, format string, a ...interface{}) *ValidationError {
	if ctx.budget == nil {
		return validationErrorf(schemaPtr, format, a...)
	}
	return ctx.budget.errorf(schemaPtr, format, a...)
}

// Group is used by extensions to group multiple errors as causes to parent error.
// This is useful in implementing keywords like allOf where each schema specified
// in allOf can result a validationErrorf.
func (ValidationError) Group(parent *ValidationError, causes ...error) error {
	return parent.add(causes...)
}
