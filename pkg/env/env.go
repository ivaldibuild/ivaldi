// Package env loads configuration from YAML files and environment variables.
//
// It uses struct tags to map environment variables to struct fields.
// Environment variables always take precedence over YAML values.
//
// Usage:
//
//	var cfg Config
//	err := env.Load(&cfg, env.Options{YAMLPath: "config.yaml"})
package env

import (
	"fmt"
	"io"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

// Parse reads environment variables into the struct pointed to by v.
// v must be a non-nil pointer to a struct.
// Fields are matched using the "env" struct tag.
func Parse(v any, opts ...Options) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("env: Parse requires a non-nil pointer to a struct, got %T", v)
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("env: Parse requires a pointer to a struct, got pointer to %s", rv.Kind())
	}

	merged := mergeOptions(opts).withDefaults()

	if len(merged.DotEnvPaths) > 0 {
		if err := LoadDotEnv(merged.DotEnvPaths...); err != nil {
			return err
		}
	}

	p := &parser{opts: merged}
	return p.walkStruct(rv, merged.Prefix)
}

// Load loads configuration from an optional YAML file, then overlays environment variables.
// This is the recommended entry point for most applications.
func Load(v any, opts ...Options) error {
	merged := mergeOptions(opts).withDefaults()

	if merged.YAMLPath != "" {
		if err := LoadYAMLFile(merged.YAMLPath, v); err != nil {
			return fmt.Errorf("env: loading YAML file: %w", err)
		}
	}

	return Parse(v, merged)
}

// LoadYAML reads YAML from r and unmarshals it into v.
func LoadYAML(r io.Reader, v any) error {
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("env: decoding YAML: %w", err)
	}
	return nil
}

// LoadYAMLFile reads a YAML file at path and unmarshals it into v.
func LoadYAMLFile(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("env: opening YAML file: %w", err)
	}
	defer f.Close()
	return LoadYAML(f, v)
}
