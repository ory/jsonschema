// Copyright 2017 Santhosh Kumar Tekuri. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsonschema

import (
	"fmt"
	"strings"
)

// InvalidJSONTypeError is the error type returned by ValidateInteface.
// this tells that specified go object is not valid jsonType.
type InvalidJSONTypeError string

func (e InvalidJSONTypeError) Error() string {
	return fmt.Sprintf("invalid jsonType: %s", string(e))
}

// SchemaError is the error type returned by Compile.
type SchemaError struct {
	// SchemaURL is the url to json-schema that filed to compile.
	// This is helpful, if your schema refers to external schemas
	SchemaURL string

	// Err is the error that occurred during compilation.
	// It could be ValidationError, because compilation validates
	// given schema against the json meta-schema
	Err error
}

func (se *SchemaError) Error() string {
	url := se.SchemaURL
	if validation, ok := se.Err.(*ValidationError); ok {
		url = validation.budget.detail(url)
	}
	return fmt.Sprintf("json-schema %q compilation failed. Reason:\n%s", url, se.Err)
}

// ValidationError is the error type returned by Validate.
type ValidationError struct {
	budget *budget

	// Message describes error
	Message string

	// InstancePtr is json-pointer which refers to json-fragment in json instance
	// that is not valid
	InstancePtr string

	// SchemaURL is the url to json-schema against which validation failed.
	// This is helpful, if your schema refers to external schemas
	SchemaURL string

	// SchemaPtr is json-pointer which refers to json-fragment in json schema
	// that failed to satisfy
	SchemaPtr string

	// Context represents error context for this specific validation error.
	Context ValidationErrorContext

	// Causes details the nested validation errors
	Causes []*ValidationError
}

func (ve *ValidationError) add(causes ...error) error {
	for _, cause := range causes {
		_ = addContext(ve.InstancePtr, ve.SchemaPtr, cause)
		ve.Causes = append(ve.Causes, cause.(*ValidationError))
	}
	return ve
}

// MessageFmt returns the Message formatted, but does not include child Cause messages.
func (ve *ValidationError) MessageFmt() string {
	return fmt.Sprintf("I[%s] S[%s] %s", ve.budget.detail(ve.InstancePtr), ve.budget.detail(ve.SchemaPtr), ve.Message)
}

func (ve *ValidationError) Error() string {
	if ve.budget != nil && ve.budget.limits != nil && !ve.budget.unboundedDisplay {
		return ve.boundedError()
	}
	msg := ve.MessageFmt()
	for _, c := range ve.Causes {
		for _, line := range strings.Split(c.Error(), "\n") {
			msg += "\n  " + line
		}
	}
	return msg
}

func (ve *ValidationError) boundedError() string {
	const maxBytes = 64 << 10
	maxDepth := min(128, ve.budget.limits.MaxDepth)
	maxNodes := min(1000, ve.budget.limits.MaxErrors)
	indentation := strings.Repeat(" ", 2*maxDepth)
	var output strings.Builder
	write := func(value string) bool {
		remaining := maxBytes - 3 - output.Len()
		if len(value) > remaining {
			output.WriteString(value[:remaining])
			output.WriteString("...")
			return false
		}
		output.WriteString(value)
		return true
	}
	type frame struct {
		err      *ValidationError
		next     int
		rendered bool
	}
	stack := []frame{{err: ve}}
	nodes := 0
	for len(stack) > 0 {
		current := &stack[len(stack)-1]
		if !current.rendered {
			if nodes >= maxNodes {
				output.WriteString("...")
				break
			}
			if nodes > 0 && !write("\n") {
				return output.String()
			}
			nodes++
			message := fmt.Sprintf("I[%s] S[%s] %s", ve.budget.detail(current.err.InstancePtr),
				ve.budget.detail(current.err.SchemaPtr), ve.budget.detail(current.err.Message))
			for {
				line, remaining, more := strings.Cut(message, "\n")
				if !write(indentation[:2*(len(stack)-1)]) || !write(line) {
					return output.String()
				}
				if !more {
					break
				}
				if !write("\n") {
					return output.String()
				}
				message = remaining
			}
			current.rendered = true
		}
		if current.next == len(current.err.Causes) {
			stack = stack[:len(stack)-1]
			continue
		}
		child := current.err.Causes[current.next]
		current.next++
		if child == nil {
			nodes++
			if nodes < maxNodes {
				continue
			}
		}
		if child == nil || len(stack) >= maxDepth {
			output.WriteString("...")
			break
		}
		stack = append(stack, frame{err: child})
	}
	return output.String()
}

func validationErrorf(schemaPtr string, format string, a ...interface{}) *ValidationError {
	return &ValidationError{Message: fmt.Sprintf(format, a...), SchemaPtr: schemaPtr}
}

func addContext(instancePtr, schemaPtr string, err error) error {
	ve := err.(*ValidationError)
	if ve.budget != nil {
		ve.budget.spend(1)
	}
	ve.InstancePtr = ve.budget.joinPtr(instancePtr, ve.InstancePtr)
	if len(ve.SchemaURL) == 0 {
		ve.SchemaPtr = ve.budget.joinPtr(schemaPtr, ve.SchemaPtr)
	}
	if required, ok := ve.Context.(*ValidationErrorContextRequired); ok {
		for i, missing := range required.Missing {
			required.Missing[i] = ve.budget.joinPtr(instancePtr, missing)
		}
	} else if ve.Context != nil {
		ve.Context.AddContext(instancePtr, ve.SchemaPtr)
	}
	for _, cause := range ve.Causes {
		_ = addContext(instancePtr, schemaPtr, cause)
	}
	return ve
}

func finishSchemaContext(err error, s *Schema) {
	ve := err.(*ValidationError)
	if ve.budget != nil {
		ve.budget.spend(1)
	}
	if len(ve.SchemaURL) == 0 {
		if ve.budget != nil {
			ve.budget.spend(len(s.URL))
		}
		ve.SchemaURL = s.URL
		ve.SchemaPtr = ve.budget.joinPtr(s.Ptr, ve.SchemaPtr)
		for _, cause := range ve.Causes {
			finishSchemaContext(cause, s)
		}
	}
}

func finishInstanceContext(err error) {
	ve := err.(*ValidationError)
	if ve.budget != nil {
		ve.budget.spend(1)
	}
	ve.InstancePtr = ve.budget.joinPtr("#", ve.InstancePtr)
	if required, ok := ve.Context.(*ValidationErrorContextRequired); ok {
		for i, missing := range required.Missing {
			required.Missing[i] = ve.budget.joinPtr("#", missing)
		}
	} else if ve.Context != nil {
		ve.Context.FinishInstanceContext()
	}
	for _, cause := range ve.Causes {
		finishInstanceContext(cause)
	}
}

func joinPtr(ptr1, ptr2 string) string {
	if len(ptr1) == 0 {
		return ptr2
	}
	if len(ptr2) == 0 {
		return ptr1
	}
	return ptr1 + "/" + ptr2
}
