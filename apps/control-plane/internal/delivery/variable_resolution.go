package delivery

import (
	"fmt"
	"slices"
	"strings"
)

// SystemVariableNames are the variables the runtime agent defines for every
// Deployment, which a value may bind but no variable may redefine.
var SystemVariableNames = []string{
	"Ivaldi.Application.Id",
	"Ivaldi.Cluster.Slug",
	"Ivaldi.Deployment.Id",
	"Ivaldi.Environment.Id",
	"Ivaldi.Environment.Slug",
	"Ivaldi.Release.Id",
	"Ivaldi.Release.Version",
	"Ivaldi.Target.Id",
	"Ivaldi.Target.Namespace",
	"Ivaldi.Target.Slug",
}

// DeploymentScope is what a Deployment's variables are resolved for: its
// Target, the Target's Environment, and its Release's Channel.
type DeploymentScope struct {
	TargetID      string
	EnvironmentID string
	ChannelID     string
}

// ResolvedValue is a value that matches a Deployment, and whose it is.
type ResolvedValue struct {
	Value  string         `json:"value"`
	Scope  VariableScope  `json:"scope"`
	Source VariableSource `json:"source" enum:"project,application"`
}

// ResolvedVariable is a variable as one Deployment gets it: the value that
// wins, and the matching values it beat. A variable none of whose values
// match has no winner, and the Deployment does not get it.
type ResolvedVariable struct {
	Name       string             `json:"name"`
	Kind       VariableKind       `json:"kind" enum:"text,secret"`
	ValuesPath *string            `json:"values_path"`
	DeliverAs  *VariableDeliverAs `json:"deliver_as" enum:"secret,value"`
	Winner     *ResolvedValue     `json:"winner"`
	Beaten     []ResolvedValue    `json:"beaten"`
}

// matches reports whether a scope applies to a Deployment: every dimension
// that names something names the Deployment's.
func (s VariableScope) matches(d DeploymentScope) bool {
	in := func(names []string, name string) bool { return len(names) == 0 || slices.Contains(names, name) }
	return in(s.TargetIDs, d.TargetID) && in(s.EnvironmentIDs, d.EnvironmentID) && in(s.ChannelIDs, d.ChannelID)
}

// specificity orders matching values: a value scoped to a Target beats one
// that is not, then one scoped to an Environment, then one scoped to a
// Channel. On equal scope an Application's value beats its Project's.
func (v ResolvedValue) specificity() int {
	n := 0
	if len(v.Scope.TargetIDs) > 0 {
		n += 8
	}
	if len(v.Scope.EnvironmentIDs) > 0 {
		n += 4
	}
	if len(v.Scope.ChannelIDs) > 0 {
		n += 2
	}
	if v.Source == VariableSourceApplication {
		n++
	}
	return n
}

// ResolveVariables picks, for each variable in a revision, the value a
// Deployment gets. An Application's variable and the Project's variable it
// shadows are one variable, whose values compete. The result is by name.
// Saving refuses two values of one owner that could tie, so the order is
// total.
func ResolveVariables(variables []SnapshotVariable, scope DeploymentScope) []ResolvedVariable {
	byName := map[string]*ResolvedVariable{}
	var order []string
	for _, v := range variables {
		key := strings.ToLower(v.Name)
		r, ok := byName[key]
		// The Application's spelling of a shared name is the one bindings use.
		if !ok || v.Source == VariableSourceApplication {
			named := ResolvedVariable{Name: v.Name, Kind: v.Kind, ValuesPath: copyString(v.ValuesPath), DeliverAs: v.DeliverAs, Beaten: []ResolvedValue{}}
			if ok {
				named.Beaten = r.Beaten
			} else {
				order = append(order, key)
			}
			r = &named
			byName[key] = r
		}
		for _, value := range v.Values {
			if value.Scope.matches(scope) {
				r.Beaten = append(r.Beaten, ResolvedValue{Value: value.Value, Scope: value.Scope.clone(), Source: v.Source})
			}
		}
	}
	slices.Sort(order)
	out := make([]ResolvedVariable, 0, len(order))
	for _, key := range order {
		r := *byName[key]
		slices.SortStableFunc(r.Beaten, func(a, b ResolvedValue) int { return b.specificity() - a.specificity() })
		if len(r.Beaten) > 0 {
			r.Winner, r.Beaten = &r.Beaten[0], r.Beaten[1:]
		}
		out = append(out, r)
	}
	return out
}

// CheckBindings checks that the values a Deployment gets can be filled in:
// every #{Name} a winning text value binds is a system variable or a text
// variable the Deployment gets, and no variables bind each other in a
// cycle. The agent would fail the Deployment on either; this says so before
// it is requested.
func CheckBindings(resolved []ResolvedVariable) error {
	binds := map[string][]string{}
	for _, name := range SystemVariableNames {
		binds[name] = nil
	}
	for _, r := range resolved {
		if r.Winner == nil || r.Kind != VariableKindText {
			continue
		}
		names, err := BindingNames(r.Winner.Value)
		if err != nil {
			return NewErrInvalidInput(fmt.Sprintf("variable %s: %s", r.Name, err.(*ErrInvalidInput).Message))
		}
		binds[r.Name] = names
	}
	const visiting, done = 1, 2
	state := map[string]int{}
	var visit func(name string, path []string) error
	visit = func(name string, path []string) error {
		switch state[name] {
		case done:
			return nil
		case visiting:
			return NewErrInvalidInput("variables bind each other in a cycle: " + strings.Join(append(path, name), " → "))
		}
		state[name] = visiting
		for _, bound := range binds[name] {
			if _, defined := binds[bound]; !defined {
				return NewErrInvalidInput(fmt.Sprintf("variable %s binds #{%s}, which has no value for this deployment", name, bound))
			}
			if err := visit(bound, append(path, name)); err != nil {
				return err
			}
		}
		state[name] = done
		return nil
	}
	for _, r := range resolved {
		if _, ok := binds[r.Name]; ok {
			if err := visit(r.Name, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// AssignmentVariables returns the text variables an agent is sent for a
// Deployment: each variable's winning value.
func AssignmentVariables(resolved []ResolvedVariable) []AssignmentVariable {
	out := []AssignmentVariable{}
	for _, r := range resolved {
		if r.Winner == nil || r.Kind != VariableKindText {
			continue
		}
		v := AssignmentVariable{Name: r.Name, Source: r.Winner.Source, Kind: r.Kind, Value: r.Winner.Value}
		if r.ValuesPath != nil {
			v.ValuesPath = *r.ValuesPath
		}
		out = append(out, v)
	}
	return out
}
