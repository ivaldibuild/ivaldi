// Package boundaries checks the import rules that keep the components in the
// ivaldi binary apart (ADR 0010). They share one Go module, so the compiler
// no longer stops a data-plane component from reaching into the control
// plane; the test in this package does.
//
// Its rules, for packages of this module:
//   - A component (gateway, worker, indexer) lives in cmd/<component> and
//     internal/<component>/..., and imports nothing else from this module.
//     What components share with each other or with the control plane
//     belongs in pkg/ (ADR 0006).
//   - The control plane (cmd/server and every other internal package) imports
//     no component.
//   - Only the root command, cmd, imports every component, to register its
//     subcommand.
package boundaries

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Components are the parts of the binary that run, or can run, outside the
// control plane: in the data plane, or in either plane like the gateway
// (ADR 0001, ADR 0002).
var Components = []string{"gateway", "worker", "indexer"}

// Violation is one import that breaks a rule.
type Violation struct {
	File   string
	Import string
	Rule   string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s imports %s: %s", v.File, v.Import, v.Rule)
}

// Check parses every Go file, tests included, under root, the directory of
// the module modulePath, and returns the imports that break a rule.
func Check(root, modulePath string) ([]Violation, error) {
	var out []Violation
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		from := owner(filepath.ToSlash(rel))
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			local, ok := strings.CutPrefix(imp, modulePath+"/")
			if !ok {
				continue
			}
			if rule := breaks(from, owner(local)); rule != "" {
				file, _ := filepath.Rel(root, path)
				out = append(out, Violation{File: filepath.ToSlash(file), Import: imp, Rule: rule})
			}
		}
		return nil
	})
	return out, err
}

// owner names what a package, given by its path within the module, belongs
// to: a component's name, "root" for the root command, or "control-plane".
func owner(rel string) string {
	parts := strings.Split(rel, "/")
	switch {
	case rel == "cmd":
		return "root"
	case len(parts) >= 2 && (parts[0] == "cmd" || parts[0] == "internal") && slices.Contains(Components, parts[1]):
		return parts[1]
	default:
		return "control-plane"
	}
}

func breaks(from, to string) string {
	switch {
	case from == "root" || from == to:
		return ""
	case from == "control-plane":
		return "the control plane must not import a component"
	case to == "control-plane":
		return "a component must not import the control plane; share code through pkg/"
	default:
		return "components must not import each other; share code through pkg/"
	}
}
