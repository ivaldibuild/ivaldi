package delivery

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ivaldibuild/ivaldi/pkg/binding"
)

// SystemVariableNames are the variables the runtime agent defines for every
// Deployment, which a value may bind but no variable may redefine.
var SystemVariableNames = binding.SystemNames

// DeploymentScope is what a Deployment's variables are resolved for: its
// Target, the Target's Environment, and its Release's Channel.
type DeploymentScope struct {
	TargetID      string
	EnvironmentID string
	ChannelID     string
}

// ResolvedValue is a value that matches a Deployment, and whose it is.
type ResolvedValue struct {
	Value string `json:"value"`
	// Secret is a secret variable's reference, shown in place of a value.
	Secret *SecretReference `json:"secret"`
	Scope  VariableScope    `json:"scope"`
	Source VariableSource   `json:"source" enum:"project,application"`
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
				r.Beaten = append(r.Beaten, ResolvedValue{Value: value.Value, Secret: copySecret(value.Secret), Scope: value.Scope.clone(), Source: v.Source})
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
// every #{Name} a winning text value, or a winning secret's path, binds is
// a system variable or a text variable the Deployment gets, and no
// variables bind each other in a cycle. The agent would fail the Deployment on either; this says so before
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
	for _, r := range resolved {
		if r.Winner == nil || r.Winner.Secret == nil {
			continue
		}
		names, err := BindingNames(r.Winner.Secret.Path)
		if err != nil {
			return NewErrInvalidInput(fmt.Sprintf("variable %s: %s", r.Name, err.(*ErrInvalidInput).Message))
		}
		for _, bound := range names {
			if _, defined := binds[bound]; !defined {
				return NewErrInvalidInput(fmt.Sprintf("the path of secret %s binds #{%s}, which has no value for this deployment", r.Name, bound))
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

// SecretStoreRef is a Secret Store as delivery needs it: enough to check a
// secret value may use it and to tell an agent how to read it. The
// credentials capability owns Secret Stores.
type SecretStoreRef struct {
	ID          string
	WorkspaceID string
	DisplayName string
	// Settings is what an agent is sent; it never carries a credential.
	Settings StoreSettings
	// EnvironmentIDs are the Environments whose Deployments may read the
	// store; empty means every Environment in its Workspace.
	EnvironmentIDs []string
	Archived       bool
	// Unusable says why no Deployment can read the store now, such as a
	// federated store on an installation with no federation keys; empty
	// when it can be read.
	Unusable string
}

// Allows reports whether Deployments to an Environment may read the store.
func (s SecretStoreRef) Allows(environmentID string) bool {
	return len(s.EnvironmentIDs) == 0 || slices.Contains(s.EnvironmentIDs, environmentID)
}

// SecretStoreIDs returns the stores the secrets a Deployment gets are in.
func SecretStoreIDs(resolved []ResolvedVariable) []string {
	var out []string
	for _, r := range resolved {
		if r.Winner != nil && r.Winner.Secret != nil && !slices.Contains(out, r.Winner.Secret.StoreID) {
			out = append(out, r.Winner.Secret.StoreID)
		}
	}
	return out
}

// AgentSecrets is what a Cluster's agent last said it can read secrets
// with: the kinds of store, the access modes it can open them in, and the
// names of the credentials its operator configured. An agent that named no
// mode opens stores in agent mode only.
type AgentSecrets struct {
	Kinds       []string
	Modes       []string
	Credentials []string
}

// opens reports whether the agent can open stores in mode. An empty mode
// is agent mode, as an empty access is.
func (a AgentSecrets) opens(mode StoreAccessMode) bool {
	if mode == "" {
		mode = StoreAccessAgent
	}
	if len(a.Modes) == 0 {
		return mode == StoreAccessAgent
	}
	return slices.Contains(a.Modes, string(mode))
}

// CheckSecretStores checks that the secrets a Deployment to an Environment
// gets are in active stores that Environment may read, of kinds and modes
// its Cluster's agent can use, with the credentials they name.
func CheckSecretStores(resolved []ResolvedVariable, stores map[string]SecretStoreRef, environmentID string, agent AgentSecrets) error {
	for _, r := range resolved {
		if r.Winner == nil || r.Winner.Secret == nil {
			continue
		}
		store, ok := stores[r.Winner.Secret.StoreID]
		switch {
		case !ok:
			return NewErrInvalidInput(fmt.Sprintf("the secret store of %s no longer exists", r.Name))
		case store.Archived:
			return NewErrInvalidInput(fmt.Sprintf("secret store %s, which holds %s, is archived", store.DisplayName, r.Name))
		case !store.Allows(environmentID):
			return NewErrInvalidInput(fmt.Sprintf("secret store %s, which holds %s, is not allowed for this environment", store.DisplayName, r.Name))
		case store.Unusable != "":
			return NewErrInvalidInput(fmt.Sprintf("secret store %s, which holds %s, cannot be read: %s", store.DisplayName, r.Name, store.Unusable))
		case !slices.Contains(agent.Kinds, string(store.Settings.Kind)):
			return NewErrInvalidInput(fmt.Sprintf("the cluster's agent cannot read %s secret stores, which %s needs", store.Settings.Kind, r.Name))
		case !agent.opens(store.Settings.Access.Mode):
			return NewErrInvalidInput(fmt.Sprintf("the cluster's agent cannot open secret stores in %s mode, which secret store %s needs for %s; upgrade the agent",
				store.Settings.Access.Mode, store.DisplayName, r.Name))
		case store.Settings.Access.Credential != "" && !slices.Contains(agent.Credentials, store.Settings.Access.Credential):
			return NewErrInvalidInput(fmt.Sprintf("the cluster's agent has no credential named %s, which secret store %s needs for %s; its operator configures credentials on the agent",
				store.Settings.Access.Credential, store.DisplayName, r.Name))
		}
	}
	return nil
}

// AssignmentSecrets returns the secrets an agent is sent for a Deployment:
// each secret variable's winning reference, with how to reach its store.
func AssignmentSecrets(resolved []ResolvedVariable, stores map[string]SecretStoreRef) []AssignmentSecret {
	out := []AssignmentSecret{}
	for _, r := range resolved {
		if r.Winner == nil || r.Winner.Secret == nil {
			continue
		}
		ref := r.Winner.Secret
		s := AssignmentSecret{
			Name: r.Name, Source: r.Winner.Source, DeliverAs: VariableDeliverAsSecret,
			Store: stores[ref.StoreID].Settings,
			Ref:   SecretRef{Path: ref.Path, Field: ref.Field, Version: ref.Version},
		}
		if r.DeliverAs != nil {
			s.DeliverAs = *r.DeliverAs
		}
		if r.ValuesPath != nil {
			s.ValuesPath = *r.ValuesPath
		}
		out = append(out, s)
	}
	return out
}

// ChangedVariables returns the names of the variables two revisions hold
// differently: added, removed, or with other values, by name.
func ChangedVariables(before, after []SnapshotVariable) []string {
	index := func(variables []SnapshotVariable) map[string]SnapshotVariable {
		out := map[string]SnapshotVariable{}
		for _, v := range variables {
			out[string(v.Source)+"/"+v.Name] = v
		}
		return out
	}
	old, next := index(before), index(after)
	var changed []string
	add := func(name string) {
		if !slices.Contains(changed, name) {
			changed = append(changed, name)
		}
	}
	for key, v := range old {
		if other, ok := next[key]; !ok || !sameSnapshotVariable(v, other) {
			add(v.Name)
		}
	}
	for key, v := range next {
		if _, ok := old[key]; !ok {
			add(v.Name)
		}
	}
	slices.Sort(changed)
	return changed
}

func sameSnapshotVariable(a, b SnapshotVariable) bool {
	if a.Kind != b.Kind || !equalPtr(a.ValuesPath, b.ValuesPath) || !equalPtr(a.DeliverAs, b.DeliverAs) || len(a.Values) != len(b.Values) {
		return false
	}
	for i := range a.Values {
		if a.Values[i].Value != b.Values[i].Value || !equalPtr(a.Values[i].Secret, b.Values[i].Secret) || !a.Values[i].Scope.equal(b.Values[i].Scope) {
			return false
		}
	}
	return true
}

// ChangedWinners returns the names of the variables a Deployment would get
// differently from two revisions: another value or secret reference, or
// one where it got none. It is what a refresh changes for a Target.
func ChangedWinners(before, after []SnapshotVariable, scope DeploymentScope) []string {
	index := func(variables []SnapshotVariable) map[string]ResolvedVariable {
		out := map[string]ResolvedVariable{}
		for _, r := range ResolveVariables(variables, scope) {
			if r.Winner != nil {
				out[strings.ToLower(r.Name)] = r
			}
		}
		return out
	}
	old, next := index(before), index(after)
	var changed []string
	for key, r := range old {
		other, ok := next[key]
		if !ok || !sameWinner(r, other) {
			changed = append(changed, r.Name)
		}
	}
	for key, r := range next {
		if _, ok := old[key]; !ok {
			changed = append(changed, r.Name)
		}
	}
	slices.Sort(changed)
	return changed
}

func sameWinner(a, b ResolvedVariable) bool {
	return a.Kind == b.Kind && equalPtr(a.ValuesPath, b.ValuesPath) && equalPtr(a.DeliverAs, b.DeliverAs) &&
		a.Winner.Value == b.Winner.Value && equalPtr(a.Winner.Secret, b.Winner.Secret)
}

// DeploymentSystem is what the system variables stand for in d: the values
// the agent fills them in with, from the same records.
func DeploymentSystem(d *Deployment, rel *Release, target *Target, env *Environment, clusterSlug string) binding.System {
	return binding.System{
		ApplicationID:   d.ApplicationID(),
		ClusterSlug:     clusterSlug,
		DeploymentID:    d.ID(),
		EnvironmentID:   env.ID(),
		EnvironmentSlug: env.Slug(),
		ReleaseID:       rel.ID(),
		ReleaseVersion:  rel.Version(),
		TargetID:        target.ID(),
		TargetNamespace: target.Locator()[LocatorNamespace],
		TargetSlug:      target.Slug(),
	}
}

// SecretPaths returns, by variable name, the path each secret a Deployment
// gets is read from: its reference's path with the bindings filled in from
// system and the Deployment's text variables. A rollback reads what the
// Deployment it restores read, given in restored, so a slug renamed since, or
// a binding of the Deployment's own ID, cannot point it elsewhere. A pin that
// recorded the reference's path unchanged, as Deployments did before paths
// were filled in here, is filled in again.
func SecretPaths(resolved []ResolvedVariable, system binding.System, restored []SecretVersion) (map[string]string, error) {
	var vars []binding.Variable
	for _, v := range AssignmentVariables(resolved) {
		vars = append(vars, binding.Variable{Name: v.Name, Value: v.Value})
	}
	bound, err := binding.Resolve(system.Map(), vars)
	if err != nil {
		return nil, err
	}
	read := map[string]string{}
	for _, pin := range restored {
		read[pin.Name] = pin.Path
	}
	out := map[string]string{}
	for _, r := range resolved {
		if r.Winner == nil || r.Winner.Secret == nil {
			continue
		}
		template := r.Winner.Secret.Path
		if path, ok := read[r.Name]; ok && path != "" && path != template {
			out[r.Name] = path
			continue
		}
		path, err := binding.Substitute(template, binding.Lookup(bound))
		if err != nil {
			return nil, fmt.Errorf("secret %s: %w", r.Name, err)
		}
		out[r.Name] = path
	}
	return out, nil
}
