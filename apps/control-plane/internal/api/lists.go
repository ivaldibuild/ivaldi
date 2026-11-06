package api

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// Lists in responses are never null: an empty list is []. The description
// says so (arrays are not nullable), and emptyLists makes every response keep
// it, so clients never check a list for null, whatever a domain snapshot
// leaves nil.
func init() {
	huma.DefaultArrayNullable = false
}

// emptyLists is a huma transformer that returns v with every nil slice in
// it, at any depth, replaced by an empty one. It copies what it changes and
// never modifies v.
func emptyLists(_ huma.Context, _ string, v any) (any, error) {
	if v == nil {
		return v, nil
	}
	out := withEmptyLists(reflect.ValueOf(v))
	return out.Interface(), nil
}

func withEmptyLists(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		inner := withEmptyLists(v.Elem())
		p := reflect.New(inner.Type())
		p.Elem().Set(inner)
		return p
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		inner := withEmptyLists(v.Elem())
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := range v.NumField() {
			if !out.Field(i).CanSet() {
				continue
			}
			out.Field(i).Set(withEmptyLists(v.Field(i)))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			// json.RawMessage is a []byte holding JSON; a nil one is null.
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return v
			}
			return reflect.MakeSlice(v.Type(), 0, 0)
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(withEmptyLists(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), withEmptyLists(iter.Value()))
		}
		return out
	}
	return v
}
