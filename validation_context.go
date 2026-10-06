package jsonschema

// ValidationErrorContext
type ValidationErrorContext interface {
	AddContext(instancePtr, schemaPtr string)
	FinishInstanceContext()
}

// ValidationErrorContextRequired is used as error context when one or more required properties are missing.
type ValidationErrorContextRequired struct {
	// Missing contains JSON Pointers to all missing properties.
	Missing []string
}

func (r *ValidationErrorContextRequired) AddContext(instancePtr, _ string) {
	for k, p := range r.Missing {
		r.Missing[k] = joinPtr(instancePtr, p)
	}
}

func (r *ValidationErrorContextRequired) FinishInstanceContext() {
	for k, p := range r.Missing {
		if len(p) == 0 {
			r.Missing[k] = "#"
		} else {
			r.Missing[k] = "#/" + p
		}
	}
}
