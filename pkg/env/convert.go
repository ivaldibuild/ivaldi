package env

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var durationType = reflect.TypeOf(time.Duration(0))

// setValue converts the raw string to the appropriate type and sets it on v.
func (p *parser) setValue(v reflect.Value, raw string, fieldName string, envKey string) error {
	// Special case: time.Duration (must check before Kind switch since it's int64 underneath).
	if v.Type() == durationType {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return &ParseError{FieldName: fieldName, EnvVar: envKey, Value: raw, TypeName: "time.Duration", Err: err}
		}
		v.Set(reflect.ValueOf(d))
		return nil
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, v.Type().Bits())
		if err != nil {
			return &ParseError{FieldName: fieldName, EnvVar: envKey, Value: raw, TypeName: v.Type().String(), Err: err}
		}
		v.SetInt(n)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, v.Type().Bits())
		if err != nil {
			return &ParseError{FieldName: fieldName, EnvVar: envKey, Value: raw, TypeName: v.Type().String(), Err: err}
		}
		v.SetUint(n)
		return nil

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, v.Type().Bits())
		if err != nil {
			return &ParseError{FieldName: fieldName, EnvVar: envKey, Value: raw, TypeName: v.Type().String(), Err: err}
		}
		v.SetFloat(f)
		return nil

	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return &ParseError{FieldName: fieldName, EnvVar: envKey, Value: raw, TypeName: "bool", Err: err}
		}
		v.SetBool(b)
		return nil

	case reflect.Slice:
		return p.setSlice(v, raw, fieldName, envKey)

	default:
		return &ParseError{
			FieldName: fieldName,
			EnvVar:    envKey,
			Value:     raw,
			TypeName:  v.Type().String(),
			Err:       fmt.Errorf("unsupported type %s", v.Type()),
		}
	}
}

// setSlice splits raw by the configured separator and sets each element on the slice.
func (p *parser) setSlice(v reflect.Value, raw string, fieldName string, envKey string) error {
	// Empty string → empty slice, not [""].
	if raw == "" {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return nil
	}

	parts := strings.Split(raw, p.opts.SliceSeparator)
	elemType := v.Type().Elem()
	slice := reflect.MakeSlice(v.Type(), 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		elem := reflect.New(elemType).Elem()
		if err := p.setValue(elem, part, fieldName, envKey); err != nil {
			return err
		}
		slice = reflect.Append(slice, elem)
	}

	v.Set(slice)
	return nil
}
