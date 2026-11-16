package values

import (
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/binding"
)

// variables returns vars as the bindings package takes them.
func variables(vars []Variable) []binding.Variable {
	out := make([]binding.Variable, len(vars))
	for i, v := range vars {
		out[i] = binding.Variable{Name: v.Name, Value: v.Value}
	}
	return out
}

// resolve returns every name an overlay may bind, system variables and
// variables alike, with the variables' own bindings substituted. A cycle or
// an undefined name fails by name.
func resolve(system map[string]string, vars []Variable) (map[string]string, error) {
	return binding.Resolve(system, variables(vars))
}

// Bind fills the #{Name} bindings in texts from the system variables and
// the variables, whose own bindings are resolved first. It is how a secret's
// path names its Environment before the secret is read. An undefined name
// or a cycle fails by name.
func Bind(system map[string]string, vars []Variable, texts []string) ([]string, error) {
	return binding.Bind(system, variables(vars), texts)
}

// substituteTree substitutes bindings in every string scalar of a parsed
// document, recording each name it binds in used. Keys are left alone, and
// a binding always yields a string, so it cannot change the document's
// structure.
func substituteTree(node any, bound map[string]string, used map[string]bool) (any, error) {
	lookup := func(name string) (string, error) {
		used[name] = true
		return binding.Lookup(bound)(name)
	}
	switch v := node.(type) {
	case string:
		return binding.Substitute(v, lookup)
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
