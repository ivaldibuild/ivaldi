package env

import (
	"reflect"
	"strings"
)

// tagInfo holds parsed information from an env struct tag.
type tagInfo struct {
	Name     string // The environment variable name.
	Required bool   // Field errors if the env var is not set.
	NotEmpty bool   // Field errors if the env var is set but empty.
	Expand   bool   // Expand ${VAR} references in the value.
}

// parseTag reads the struct tag for the given key and parses it into tagInfo.
//
// Supported formats:
//
//	env:"VAR_NAME"
//	env:"VAR_NAME,required"
//	env:"VAR_NAME,required,notEmpty"
//	env:"VAR_NAME,expand"
func parseTag(tag reflect.StructTag, tagKey string) (tagInfo, bool) {
	raw, ok := tag.Lookup(tagKey)
	if !ok {
		return tagInfo{}, false
	}

	parts := strings.Split(raw, ",")
	info := tagInfo{
		Name: strings.TrimSpace(parts[0]),
	}

	for _, opt := range parts[1:] {
		switch strings.TrimSpace(opt) {
		case "required":
			info.Required = true
		case "notEmpty":
			info.NotEmpty = true
		case "expand":
			info.Expand = true
		}
	}

	return info, true
}
