package values

import (
	"fmt"
	"strings"
)

// resolve returns every name an overlay may bind, system variables and
// variables alike, with the variables' own bindings substituted. A cycle or
// an undefined name fails by name.
func resolve(system map[string]string, vars []Variable) (map[string]string, error) {
	raw := map[string]string{}
	for _, v := range vars {
		if _, ok := system[v.Name]; ok {
			return nil, fmt.Errorf("variable %s has the name of a system variable", v.Name)
		}
		if _, ok := raw[v.Name]; ok {
			return nil, fmt.Errorf("variable %s is defined twice", v.Name)
		}
		raw[v.Name] = v.Value
	}

	bound := make(map[string]string, len(system)+len(raw))
	for name, value := range system {
		bound[name] = value
	}
	visiting := map[string]bool{}
	var visit func(name string, path []string) (string, error)
	visit = func(name string, path []string) (string, error) {
		if value, ok := bound[name]; ok {
			return value, nil
		}
		value, ok := raw[name]
		if !ok {
			return "", fmt.Errorf("#{%s} is not defined", name)
		}
		if visiting[name] {
			return "", fmt.Errorf("variables bind each other in a cycle: %s", strings.Join(append(path, name), " → "))
		}
		visiting[name] = true
		out, err := substitute(value, func(ref string) (string, error) { return visit(ref, append(path, name)) })
		if err != nil {
			return "", fmt.Errorf("variable %s: %w", name, err)
		}
		visiting[name] = false
		bound[name] = out
		return out, nil
	}
	for _, v := range vars {
		if _, err := visit(v.Name, nil); err != nil {
			return nil, err
		}
	}
	return bound, nil
}

// substitute replaces each #{Name} in s with lookup(Name). ##{ writes a
// literal #{.
func substitute(s string, lookup func(string) (string, error)) (string, error) {
	if !strings.Contains(s, "#{") {
		return s, nil
	}
	var out strings.Builder
	for {
		i := strings.Index(s, "#{")
		if i < 0 {
			out.WriteString(s)
			return out.String(), nil
		}
		if i > 0 && s[i-1] == '#' {
			out.WriteString(s[:i-1])
			out.WriteString("#{")
			s = s[i+2:]
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 0 {
			return "", fmt.Errorf("%q has an unclosed #{", s)
		}
		name := strings.TrimSpace(s[i+2 : i+end])
		if name == "" {
			return "", fmt.Errorf("%q has an empty #{}", s)
		}
		value, err := lookup(name)
		if err != nil {
			return "", err
		}
		out.WriteString(s[:i])
		out.WriteString(value)
		s = s[i+end+1:]
	}
}

// substituteTree substitutes bindings in every string scalar of a parsed
// document, recording each name it binds in used. Keys are left alone, and
// a binding always yields a string, so it cannot change the document's
// structure.
func substituteTree(node any, bound map[string]string, used map[string]bool) (any, error) {
	lookup := func(name string) (string, error) {
		value, ok := bound[name]
		if !ok {
			return "", fmt.Errorf("#{%s} is not defined", name)
		}
		used[name] = true
		return value, nil
	}
	switch v := node.(type) {
	case string:
		return substitute(v, lookup)
	case map[string]any:
		for key, child := range v {
			out, err := substituteTree(child, bound, used)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			v[key] = out
		}
		return v, nil
	case []any:
		for i, child := range v {
			out, err := substituteTree(child, bound, used)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			v[i] = out
		}
		return v, nil
	default:
		return v, nil
	}
}
