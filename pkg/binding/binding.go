// Package binding fills in the #{Name} bindings a variable's text value, an
// overlay value, or a secret's path may hold. The control plane, which
// checks and records what a Deployment reads, and the runtime agent, which
// reads it, both use it, so the two never disagree on the syntax or on what
// a binding stands for.
//
// #{Name} binds the system variable or text variable Name, with the space
// around the name trimmed. ##{ writes a literal #{.
package binding

import (
	"errors"
	"fmt"
	"strings"
)

// ErrUnclosed is a #{ with no } after it.
var ErrUnclosed = errors.New("unclosed #{")

// ErrEmpty is a #{} that names nothing.
var ErrEmpty = errors.New("empty #{}")

// System variable names: what the runtime agent defines for every
// Deployment, which a value may bind but no variable may redefine.
const (
	ApplicationID   = "Ivaldi.Application.Id"
	ClusterSlug     = "Ivaldi.Cluster.Slug"
	DeploymentID    = "Ivaldi.Deployment.Id"
	EnvironmentID   = "Ivaldi.Environment.Id"
	EnvironmentSlug = "Ivaldi.Environment.Slug"
	ReleaseID       = "Ivaldi.Release.Id"
	ReleaseVersion  = "Ivaldi.Release.Version"
	TargetID        = "Ivaldi.Target.Id"
	TargetNamespace = "Ivaldi.Target.Namespace"
	TargetSlug      = "Ivaldi.Target.Slug"
)

// SystemNames lists the system variable names, sorted.
var SystemNames = []string{
	ApplicationID, ClusterSlug, DeploymentID, EnvironmentID, EnvironmentSlug,
	ReleaseID, ReleaseVersion, TargetID, TargetNamespace, TargetSlug,
}

// System is what the system variables stand for in one Deployment.
type System struct {
	ApplicationID   string
	ClusterSlug     string
	DeploymentID    string
	EnvironmentID   string
	EnvironmentSlug string
	ReleaseID       string
	ReleaseVersion  string
	TargetID        string
	TargetNamespace string
	TargetSlug      string
}

// Map returns the system variables by name.
func (s System) Map() map[string]string {
	return map[string]string{
		ApplicationID:   s.ApplicationID,
		ClusterSlug:     s.ClusterSlug,
		DeploymentID:    s.DeploymentID,
		EnvironmentID:   s.EnvironmentID,
		EnvironmentSlug: s.EnvironmentSlug,
		ReleaseID:       s.ReleaseID,
		ReleaseVersion:  s.ReleaseVersion,
		TargetID:        s.TargetID,
		TargetNamespace: s.TargetNamespace,
		TargetSlug:      s.TargetSlug,
	}
}

// Variable is a text variable a binding may name. Its value may bind
// others.
type Variable struct {
	Name  string
	Value string
}

// Names returns the names text binds, in order of first use.
func Names(text string) ([]string, error) {
	var names []string
	_, err := Substitute(text, func(name string) (string, error) {
		if !contains(names, name) {
			names = append(names, name)
		}
		return "", nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// Substitute replaces each #{Name} in text with lookup(Name), and each ##{
// with a literal #{.
func Substitute(text string, lookup func(name string) (string, error)) (string, error) {
	if !strings.Contains(text, "#{") {
		return text, nil
	}
	var out strings.Builder
	for s := text; ; {
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
			return "", fmt.Errorf("%q has an %w", text, ErrUnclosed)
		}
		name := strings.TrimSpace(s[i+2 : i+end])
		if name == "" {
			return "", fmt.Errorf("%q has an %w", text, ErrEmpty)
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

// Escape returns text written so that Substitute gives it back unchanged:
// every #{ becomes ##{. It is how a path already filled in is sent where
// bindings are filled in again.
func Escape(text string) string {
	return strings.ReplaceAll(text, "#{", "##{")
}

// Resolve returns every name a binding may name, the system variables and
// the variables alike, with the variables' own bindings filled in. An
// undefined name or a cycle fails by name.
func Resolve(system map[string]string, vars []Variable) (map[string]string, error) {
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
		out, err := Substitute(value, func(ref string) (string, error) { return visit(ref, append(path, name)) })
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

// Lookup returns a lookup for Substitute over names Resolve returned, which
// fails on a name that is not there.
func Lookup(bound map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		value, ok := bound[name]
		if !ok {
			return "", fmt.Errorf("#{%s} is not defined", name)
		}
		return value, nil
	}
}

// Bind fills the bindings in texts from the system variables and the
// variables, whose own bindings are filled in first.
func Bind(system map[string]string, vars []Variable, texts []string) ([]string, error) {
	bound, err := Resolve(system, vars)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(texts))
	for i, text := range texts {
		if out[i], err = Substitute(text, Lookup(bound)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
