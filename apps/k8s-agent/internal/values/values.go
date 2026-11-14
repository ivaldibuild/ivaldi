// Package values builds the values a Deployment is applied with: the chart's
// overlay files in order, with #{…} bindings resolved, then the variables
// that name a values path, then Ivaldi's generated values. It knows nothing
// about deployers: documents go in and a map comes out, so Helm, Argo CD, or
// Flux can use it alike.
package values

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"
)

// Document is one overlay values file from the chart.
type Document struct {
	Name    string
	Content []byte
}

// Variable is one variable's winning value for the Deployment. Value may
// bind other variables and system variables with #{Name}.
type Variable struct {
	Name string
	// ValuesPath, when set, is the dotted path the value is written to.
	ValuesPath string
	Value      string
}

// Image is one artifact as the Deployment runs it.
type Image struct {
	Repository string
	Digest     string
}

// Generated are the values Ivaldi adds under global.ivaldi.
type Generated struct {
	Application string
	Release     string
	Deployment  string
	Version     string
	Artifacts   map[string]Image
}

// Input is everything one Deployment's values are built from.
type Input struct {
	// Overlays are the selected overlay files, in the order they apply.
	Overlays []Document
	// System are Ivaldi's system variables, such as
	// Ivaldi.Environment.Slug, which overlays and variables may bind.
	System    map[string]string
	Variables []Variable
	Generated Generated
}

// Result is a built set of values.
type Result struct {
	Values map[string]any
}

// Build builds the values for in. It returns an error, never a partial
// result, so a Deployment that cannot be built fails before anything is
// applied.
func Build(_ context.Context, in Input) (Result, error) {
	bound, err := resolve(in.System, in.Variables)
	if err != nil {
		return Result{}, err
	}
	if err := checkPaths(in.Variables); err != nil {
		return Result{}, err
	}

	merged := map[string]any{}
	for _, doc := range in.Overlays {
		parsed, err := parse(doc)
		if err != nil {
			return Result{}, err
		}
		substituted, err := substituteTree(parsed, bound)
		if err != nil {
			return Result{}, fmt.Errorf("overlay %s: %w", doc.Name, err)
		}
		merged = merge(merged, substituted.(map[string]any))
	}

	// A variable with a values path beats any overlay's literal.
	for _, v := range in.Variables {
		if v.ValuesPath == "" {
			continue
		}
		if err := set(merged, v.ValuesPath, bound[v.Name]); err != nil {
			return Result{}, fmt.Errorf("variable %s: %w", v.Name, err)
		}
	}

	if err := setGenerated(merged, in.Generated.values()); err != nil {
		return Result{}, err
	}
	return Result{Values: merged}, nil
}

func (g Generated) values() map[string]any {
	artifacts := map[string]any{}
	for name, image := range g.Artifacts {
		artifacts[name] = map[string]any{
			"repository": image.Repository,
			"digest":     image.Digest,
			"image":      image.Repository + "@" + image.Digest,
		}
	}
	return map[string]any{
		"application": g.Application,
		"release":     g.Release,
		"deployment":  g.Deployment,
		"version":     g.Version,
		"artifacts":   artifacts,
	}
}

// parse reads one overlay, which must be a single YAML mapping or empty.
func parse(doc Document) (map[string]any, error) {
	var out any
	if err := yaml.Unmarshal(doc.Content, &out); err != nil {
		return nil, fmt.Errorf("overlay %s is not valid YAML: %w", doc.Name, err)
	}
	switch v := out.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return v, nil
	default:
		return nil, fmt.Errorf("overlay %s must be a mapping of values", doc.Name)
	}
}

// merge merges overlay into base: maps deep-merge, anything else, lists
// included, replaces. A null replaces too, so it removes an earlier
// overlay's value and is passed on for the deployer to remove a chart
// default.
func merge(base, overlay map[string]any) map[string]any {
	for key, value := range overlay {
		next, isMap := value.(map[string]any)
		prev, wasMap := base[key].(map[string]any)
		if isMap && wasMap {
			base[key] = merge(prev, next)
			continue
		}
		base[key] = value
	}
	return base
}

// set writes value at a dotted path, creating maps along it. It refuses to
// replace a value that is not a map on the way.
func set(m map[string]any, path string, value any) error {
	keys := strings.Split(path, ".")
	if slices.Contains(keys, "") {
		return fmt.Errorf("values path %q is not a dotted path", path)
	}
	for i, key := range keys[:len(keys)-1] {
		switch next := m[key].(type) {
		case map[string]any:
			m = next
		case nil:
			created := map[string]any{}
			m[key] = created
			m = created
		default:
			return fmt.Errorf("values path %q: %s is not a map", path, strings.Join(keys[:i+1], "."))
		}
	}
	last := keys[len(keys)-1]
	if existing, ok := m[last].(map[string]any); ok {
		if incoming, ok := value.(map[string]any); ok {
			m[last] = merge(existing, incoming)
			return nil
		}
	}
	m[last] = value
	return nil
}

// generatedPath is where Ivaldi's generated values go. It is Ivaldi's
// alone: overlays cannot add to it and variables cannot write into it.
const generatedPath = "global.ivaldi"

// setGenerated puts the generated values at global.ivaldi, replacing
// anything an overlay put there, so a chart never reads an artifact or a
// value that is not this Deployment's. Other keys under global are kept.
func setGenerated(m map[string]any, generated map[string]any) error {
	global, ok := m["global"].(map[string]any)
	switch {
	case ok:
	case m["global"] == nil:
		global = map[string]any{}
		m["global"] = global
	default:
		return fmt.Errorf("an overlay sets global to something that is not a map")
	}
	global["ivaldi"] = generated
	return nil
}

// checkPaths refuses two variables writing the same values path, or one
// path inside another, which would have one silently overwrite the other.
func checkPaths(vars []Variable) error {
	for i, a := range vars {
		if a.ValuesPath == "" {
			continue
		}
		if overlaps(a.ValuesPath, generatedPath) {
			return fmt.Errorf("variable %s writes %q, which is reserved for Ivaldi's generated values", a.Name, a.ValuesPath)
		}
		for _, b := range vars[i+1:] {
			if b.ValuesPath == "" {
				continue
			}
			if overlaps(a.ValuesPath, b.ValuesPath) {
				return fmt.Errorf("variables %s and %s write overlapping values paths %q and %q", a.Name, b.Name, a.ValuesPath, b.ValuesPath)
			}
		}
	}
	return nil
}

func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}
