package env

import (
	"os"
	"reflect"
)

// parser holds the configuration for a parsing session.
type parser struct {
	opts Options
}

// walkStruct recursively processes a struct value, reading env vars for tagged fields.
func (p *parser) walkStruct(v reflect.Value, prefix string) error {
	t := v.Type()
	var errs []error

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldVal := v.Field(i)

		// Skip unexported fields.
		if !field.IsExported() {
			continue
		}

		// Skip interface{} / any fields — no way to determine target type.
		if fieldVal.Kind() == reflect.Interface {
			continue
		}

		tag, hasTag := parseTag(field.Tag, p.opts.TagName)

		// No env tag: if the field is a struct (or *struct), recurse into it.
		if !hasTag {
			if isStructLike(fieldVal) {
				if err := p.walkStruct(deref(fieldVal), prefix); err != nil {
					errs = append(errs, unwrapAggregate(err)...)
				}
			}
			continue
		}

		// Tag present but no name: recurse into nested struct.
		if tag.Name == "" && isStructLike(fieldVal) {
			if err := p.walkStruct(deref(fieldVal), prefix); err != nil {
				errs = append(errs, unwrapAggregate(err)...)
			}
			continue
		}

		envKey := prefix + tag.Name

		// Look up the env var.
		raw, found := p.opts.Source(envKey)

		// Apply envDefault if not found — but only to zero-valued fields,
		// so a value already set from YAML (or by the caller) survives an
		// absent env var. Layering is default < YAML < env.
		if !found {
			if defaultVal, hasDefault := field.Tag.Lookup(p.opts.DefaultTagName); hasDefault && fieldVal.IsZero() {
				raw = defaultVal
				found = true
			}
		}

		// Handle "required" option.
		if tag.Required && !found {
			errs = append(errs, &RequiredError{EnvVar: envKey})
			continue
		}

		// Handle "notEmpty" option.
		if tag.NotEmpty && found && raw == "" {
			errs = append(errs, &NotEmptyError{EnvVar: envKey})
			continue
		}

		if !found {
			continue
		}

		// Handle "expand" option.
		if tag.Expand {
			raw = os.ExpandEnv(raw)
		}

		// Set the field value.
		if err := p.setField(fieldVal, raw, field.Name, envKey); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return &AggregateError{Errors: errs}
	}
	return nil
}

// setField handles pointer wrapping before delegating to setValue.
func (p *parser) setField(fieldVal reflect.Value, raw string, fieldName string, envKey string) error {
	if fieldVal.Type().Kind() == reflect.Ptr {
		elemType := fieldVal.Type().Elem()
		newVal := reflect.New(elemType)
		if err := p.setValue(newVal.Elem(), raw, fieldName, envKey); err != nil {
			return err
		}
		fieldVal.Set(newVal)
		return nil
	}
	return p.setValue(fieldVal, raw, fieldName, envKey)
}

// deref follows pointers, allocating as needed, and returns the underlying value.
func deref(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}
	return v
}

// isStructLike returns true if v is a struct or a pointer (chain) to a struct.
func isStructLike(v reflect.Value) bool {
	t := v.Type()
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}

// unwrapAggregate flattens an AggregateError into its individual errors,
// or returns the error as a single-element slice.
func unwrapAggregate(err error) []error {
	if agg, ok := err.(*AggregateError); ok {
		return agg.Errors
	}
	return []error{err}
}
