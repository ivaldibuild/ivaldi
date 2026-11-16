package delivery

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/binding"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// SystemVariablePrefix starts the names of the variables Ivaldi defines for
// every Deployment, such as Ivaldi.Environment.Slug. No variable may use it.
const SystemVariablePrefix = "Ivaldi."

// generatedValuesPath is where the agent writes Ivaldi's generated values.
// No variable may write there.
const generatedValuesPath = "global.ivaldi"

var (
	variableNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	valuesPathPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)
)

// Variable is a named piece of configuration that a Project's Applications,
// or one Application, deploy with (docs/design/deployment-variables.md). It
// owns its values, each scoped to the Deployments it applies to, and they
// change only through its methods, which keep any two from tying.
type Variable struct {
	aggregate.Root
	organizationID string
	projectID      *string
	applicationID  *string
	name           string
	description    *string
	kind           VariableKind
	valuesPath     *string
	deliverAs      *VariableDeliverAs
	values         []*VariableValue
	createdAt      time.Time
	updatedAt      time.Time
	archivedAt     *time.Time
}

// SecretStorePrefix is the ID prefix of the credentials capability's Secret
// Stores, which a secret value names without a foreign key.
const SecretStorePrefix = "sst"

// SecretReference is where a secret value is kept: never the value itself,
// which only the runtime agent reads (ADR 0004).
type SecretReference struct {
	StoreID string `json:"store_id"`
	// Path is a Kubernetes Secret's name, an AWS secret's name or ARN, a path
	// in a Vault mount, or a Doppler secret's name. It may bind text and
	// system variables, so one value can serve every Environment.
	Path string `json:"path"`
	// Field is one key of a map or JSON secret; empty for the whole value.
	Field string `json:"field"`
	// Version pins the store's version; empty means the latest when deployed.
	Version string `json:"version"`
}

// VariableValue is one value of a Variable and the Deployments it applies to.
type VariableValue struct {
	aggregate.Entity
	text      string
	secret    *SecretReference
	scope     VariableScope
	createdAt time.Time
	updatedAt time.Time
}

// VariableScope names the Deployments a value applies to. The names in one
// dimension are alternatives and every dimension that has names must match;
// an empty dimension matches anything.
type VariableScope struct {
	EnvironmentIDs []string `json:"environment_ids"`
	TargetIDs      []string `json:"target_ids"`
	ChannelIDs     []string `json:"channel_ids"`
}

// VariableSnapshot is a Variable's state, for repositories, audit records,
// and events. Its JSON form is part of the Variable events' contract.
type VariableSnapshot struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	// Exactly one of ProjectID and ApplicationID is set: the owner.
	ProjectID     *string                 `json:"project_id"`
	ApplicationID *string                 `json:"application_id"`
	Name          string                  `json:"name"`
	Description   *string                 `json:"description"`
	Kind          VariableKind            `json:"kind" enum:"text,secret"`
	ValuesPath    *string                 `json:"values_path"`
	DeliverAs     *VariableDeliverAs      `json:"deliver_as" enum:"secret,value"`
	Values        []VariableValueSnapshot `json:"values"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	ArchivedAt    *time.Time              `json:"archived_at"`
}

// VariableValueSnapshot is a value's state.
type VariableValueSnapshot struct {
	ID string `json:"id"`
	// Value is a text variable's value, which may contain bindings.
	Value string `json:"value"`
	// Secret is a secret variable's value: where the secret is kept.
	Secret    *SecretReference `json:"secret"`
	Scope     VariableScope    `json:"scope"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// NewVariableProps describes a Variable. A new one leaves the timestamps
// nil; a repository loading a stored one sets them.
type NewVariableProps struct {
	OrganizationID string
	// Exactly one of ProjectID and ApplicationID names the owner.
	ProjectID     *string
	ApplicationID *string
	Name          string
	Description   *string
	Kind          VariableKind
	ValuesPath    *string
	DeliverAs     *VariableDeliverAs
	Values        []NewVariableValueProps
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
	ArchivedAt    *time.Time
}

// NewVariableValueProps describes a value given to NewVariable. A new one
// leaves ID and the timestamps nil; a stored one sets them.
type NewVariableValueProps struct {
	ID        *string
	Value     string
	Secret    *SecretReference
	Scope     VariableScope
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// VariableValueProps is the content of a value added to or changed in a Variable.
type VariableValueProps struct {
	// Value is a text variable's value; Secret a secret variable's.
	Value  string
	Secret *SecretReference
	Scope  VariableScope
}

// UpdateVariableProps changes the fields that are set. An empty Description
// or ValuesPath removes it. A Variable's name and kind never change, since
// bindings and Release snapshots refer to them.
type UpdateVariableProps struct {
	Description *string
	ValuesPath  *string
	// DeliverAs changes how a secret variable reaches the chart.
	DeliverAs *VariableDeliverAs
}

// NewVariable returns a Variable, with new IDs for it and its values unless
// they are given.
func NewVariable(props NewVariableProps, variableID *string) (*Variable, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	projectID, applicationID := optional.OfPtr(props.ProjectID), optional.OfPtr(props.ApplicationID)
	switch {
	case (projectID == nil) == (applicationID == nil):
		return nil, NewErrInvalidInput("a variable belongs to a project or to an application, not both")
	case projectID != nil && !id.Valid(workspace.Project{}.Prefix(), *projectID):
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a project ID", *projectID))
	case applicationID != nil && !id.Valid(Application{}.Prefix(), *applicationID):
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not an application ID", *applicationID))
	}
	root, err := newRoot(Variable{}.Prefix(), variableID)
	if err != nil {
		return nil, err
	}
	if err := validateVariableName(props.Name); err != nil {
		return nil, err
	}
	if props.Kind != VariableKindText && props.Kind != VariableKindSecret {
		return nil, NewErrInvalidInput(fmt.Sprintf("variable kind %q must be text or secret", props.Kind))
	}
	valuesPath := optional.OfPtr(props.ValuesPath)
	if err := validateValuesPath(valuesPath); err != nil {
		return nil, err
	}
	deliverAs, err := normalizeDeliverAs(props.Kind, optional.OfPtr(props.DeliverAs), valuesPath)
	if err != nil {
		return nil, err
	}
	if len(props.Values) > maxVariableValues {
		return nil, NewErrInvalidInput(fmt.Sprintf("a variable can have at most %d values", maxVariableValues))
	}

	now := clock.Now()
	v := &Variable{
		Root:           root,
		organizationID: props.OrganizationID,
		projectID:      projectID,
		applicationID:  applicationID,
		name:           props.Name,
		description:    optional.OfPtr(props.Description),
		kind:           props.Kind,
		valuesPath:     valuesPath,
		deliverAs:      deliverAs,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
		archivedAt:     clock.CopyTime(props.ArchivedAt),
	}
	for _, p := range props.Values {
		entity, err := newEntity(VariableValue{}.Prefix(), p.ID)
		if err != nil {
			return nil, err
		}
		content, err := v.normalizeValue(VariableValueProps{Value: p.Value, Secret: p.Secret, Scope: p.Scope})
		if err != nil {
			return nil, err
		}
		value := &VariableValue{
			Entity:    entity,
			text:      content.Value,
			secret:    content.Secret,
			scope:     content.Scope,
			createdAt: timeOr(p.CreatedAt, now),
			updatedAt: timeOr(p.UpdatedAt, now),
		}
		if err := v.checkNoTie(value); err != nil {
			return nil, err
		}
		v.values = append(v.values, value)
	}
	if v.IsNew() {
		v.RecordEvent(VariableCreated{v.Snapshot()})
	}
	return v, nil
}

func validateVariableName(name string) error {
	switch {
	case name == "" || len(name) > maxVariableNameLength || !variableNamePattern.MatchString(name):
		return NewErrInvalidInput(fmt.Sprintf("a variable's name must be 1-%d letters, digits, dots, hyphens, and underscores", maxVariableNameLength))
	case strings.HasPrefix(strings.ToLower(name), strings.ToLower(SystemVariablePrefix)):
		return NewErrInvalidInput(fmt.Sprintf("names starting with %q are reserved for system variables", SystemVariablePrefix))
	}
	return nil
}

func validateValuesPath(path *string) error {
	switch {
	case path == nil:
		return nil
	case len(*path) > maxValuesPathLength || !valuesPathPattern.MatchString(*path):
		return NewErrInvalidInput(fmt.Sprintf("a values path must be dot-separated keys of letters, digits, hyphens, and underscores, at most %d characters", maxValuesPathLength))
	case ValuesPathsOverlap(*path, generatedValuesPath):
		return NewErrInvalidInput(fmt.Sprintf("values path %q is reserved for Ivaldi's generated values", *path))
	}
	return nil
}

// normalizeDeliverAs returns how a variable of the kind reaches the chart: a
// secret as a Kubernetes Secret unless it asks to be written into the
// chart's values, which needs a path to write it at; a text variable has no
// choice to make.
func normalizeDeliverAs(kind VariableKind, deliverAs *VariableDeliverAs, valuesPath *string) (*VariableDeliverAs, error) {
	if kind == VariableKindText {
		if deliverAs != nil {
			return nil, NewErrInvalidInput("only a secret variable says how it is delivered")
		}
		return nil, nil
	}
	if deliverAs == nil {
		return new(VariableDeliverAsSecret), nil
	}
	switch *deliverAs {
	case VariableDeliverAsSecret:
	case VariableDeliverAsValue:
		if valuesPath == nil {
			return nil, NewErrInvalidInput("a secret delivered as a value needs a values path to be written to")
		}
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("deliver_as %q must be secret or value", *deliverAs))
	}
	return deliverAs, nil
}

// normalizeSecret checks a secret variable's value: a reference, with no
// text beside it.
func normalizeSecret(p VariableValueProps) (*SecretReference, error) {
	if p.Value != "" {
		return nil, NewErrInvalidInput("a secret variable's value is a reference to a secret store, never the secret itself")
	}
	if p.Secret == nil {
		return nil, NewErrInvalidInput("a secret variable's value needs a secret store and a path")
	}
	ref := *p.Secret
	switch {
	case !id.Valid(SecretStorePrefix, ref.StoreID):
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a secret store ID", ref.StoreID))
	case ref.Path == "" || utf8.RuneCountInString(ref.Path) > maxSecretPathLength:
		return nil, NewErrInvalidInput(fmt.Sprintf("a secret's path must be 1-%d characters", maxSecretPathLength))
	case utf8.RuneCountInString(ref.Field) > maxSecretFieldLength || utf8.RuneCountInString(ref.Version) > maxSecretFieldLength:
		return nil, NewErrInvalidInput(fmt.Sprintf("a secret's field and version can have at most %d characters", maxSecretFieldLength))
	}
	if _, err := BindingNames(ref.Path); err != nil {
		return nil, err
	}
	return &ref, nil
}

func copySecret(ref *SecretReference) *SecretReference {
	if ref == nil {
		return nil
	}
	c := *ref
	return &c
}

// ValuesPathsOverlap reports whether writing one path would overwrite the
// other: they are equal, or one is a prefix of the other at a key boundary.
func ValuesPathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}

// normalizeValue checks a value and returns it with its scope sorted and
// without duplicates, so equal values store equal rows.
func (v *Variable) normalizeValue(p VariableValueProps) (VariableValueProps, error) {
	var secret *SecretReference
	if v.kind == VariableKindSecret {
		ref, err := normalizeSecret(p)
		if err != nil {
			return VariableValueProps{}, err
		}
		secret = ref
	} else {
		if p.Secret != nil {
			return VariableValueProps{}, NewErrInvalidInput("only a secret variable's value refers to a secret store")
		}
		if utf8.RuneCountInString(p.Value) > maxVariableValueLength {
			return VariableValueProps{}, NewErrInvalidInput(fmt.Sprintf("a value can have at most %d characters", maxVariableValueLength))
		}
		if _, err := BindingNames(p.Value); err != nil {
			return VariableValueProps{}, err
		}
	}
	if v.projectID != nil && len(p.Scope.ChannelIDs) > 0 {
		return VariableValueProps{}, NewErrInvalidInput("a project's variable cannot be scoped to channels, which belong to one application")
	}
	scope := VariableScope{}
	for _, d := range []struct {
		name, prefix string
		in           []string
		out          *[]string
	}{
		{"environment", Environment{}.Prefix(), p.Scope.EnvironmentIDs, &scope.EnvironmentIDs},
		{"target", Target{}.Prefix(), p.Scope.TargetIDs, &scope.TargetIDs},
		{"channel", Channel{}.Prefix(), p.Scope.ChannelIDs, &scope.ChannelIDs},
	} {
		if len(d.in) > maxScopeNames {
			return VariableValueProps{}, NewErrInvalidInput(fmt.Sprintf("a value can be scoped to at most %d %ss", maxScopeNames, d.name))
		}
		for _, scoped := range d.in {
			if !id.Valid(d.prefix, scoped) {
				return VariableValueProps{}, NewErrInvalidInput(fmt.Sprintf("%q is not a %s ID", scoped, d.name))
			}
		}
		*d.out = slices.Compact(sortedStrings(d.in))
		if *d.out == nil {
			*d.out = []string{}
		}
	}
	return VariableValueProps{Value: p.Value, Secret: secret, Scope: scope}, nil
}

// checkNoTie refuses a value that could tie with another for some
// Deployment: both scope the same dimensions, and share a name in each. The
// most specific value wins a Deployment, so two equally specific values that
// both match would leave the winner undefined.
func (v *Variable) checkNoTie(candidate *VariableValue) error {
	for _, other := range v.values {
		if other.ID() == candidate.ID() {
			continue
		}
		if overlap, ties := candidate.scope.tiesWith(other.scope); ties {
			if overlap == "" {
				return NewErrInvalidInput(fmt.Sprintf("variable %s already has a value with no scope", v.name))
			}
			return NewErrInvalidInput(fmt.Sprintf("variable %s already has a value for %s, so which one a deployment gets would be undefined", v.name, overlap))
		}
	}
	return nil
}

// tiesWith reports whether two scopes could match one Deployment with equal
// specificity, and names what they share.
func (s VariableScope) tiesWith(o VariableScope) (string, bool) {
	var shared []string
	for _, d := range []struct {
		name string
		a, b []string
	}{
		{"target", s.TargetIDs, o.TargetIDs},
		{"environment", s.EnvironmentIDs, o.EnvironmentIDs},
		{"channel", s.ChannelIDs, o.ChannelIDs},
	} {
		if (len(d.a) == 0) != (len(d.b) == 0) {
			return "", false
		}
		if len(d.a) == 0 {
			continue
		}
		i := slices.IndexFunc(d.a, func(name string) bool { return slices.Contains(d.b, name) })
		if i < 0 {
			return "", false
		}
		shared = append(shared, d.name+" "+d.a[i])
	}
	return strings.Join(shared, " and "), true
}

// BindingNames returns the names a text value binds with #{Name}, in order
// of first use. ##{ writes a literal #{. It follows the agent's syntax,
// which fills the bindings in when a Release is deployed.
func BindingNames(text string) ([]string, error) {
	names, err := binding.Names(text)
	switch {
	case errors.Is(err, binding.ErrUnclosed):
		return nil, NewErrInvalidInput("the value has an unclosed #{")
	case errors.Is(err, binding.ErrEmpty):
		return nil, NewErrInvalidInput("the value has an empty #{}")
	case err != nil:
		return nil, NewErrInvalidInput(err.Error())
	}
	return names, nil
}

func (Variable) Prefix() string      { return "var" }
func (VariableValue) Prefix() string { return "vvl" }

func (v *Variable) OrganizationID() string { return v.organizationID }
func (v *Variable) ProjectID() *string     { return copyString(v.projectID) }
func (v *Variable) ApplicationID() *string { return copyString(v.applicationID) }
func (v *Variable) Name() string           { return v.name }
func (v *Variable) Description() *string   { return copyString(v.description) }
func (v *Variable) Kind() VariableKind     { return v.kind }
func (v *Variable) ValuesPath() *string    { return copyString(v.valuesPath) }

// SecretStoreIDs returns the Secret Stores the Variable's values refer to.
func (v *Variable) SecretStoreIDs() []string {
	var out []string
	for _, value := range v.values {
		if value.secret != nil && !slices.Contains(out, value.secret.StoreID) {
			out = append(out, value.secret.StoreID)
		}
	}
	return out
}

func (v *Variable) CreatedAt() time.Time   { return v.createdAt }
func (v *Variable) UpdatedAt() time.Time   { return v.updatedAt }
func (v *Variable) ArchivedAt() *time.Time { return clock.CopyTime(v.archivedAt) }

func (v *Variable) Archived() bool { return v.archivedAt != nil }

// Source says which kind of owner the Variable has.
func (v *Variable) Source() VariableSource {
	if v.applicationID != nil {
		return VariableSourceApplication
	}
	return VariableSourceProject
}

// Values returns the Variable's values in the order they were added.
func (v *Variable) Values() []VariableValueSnapshot {
	out := make([]VariableValueSnapshot, len(v.values))
	for i, value := range v.values {
		out[i] = value.snapshot()
	}
	return out
}

// Value returns one of the Variable's values.
func (v *Variable) Value(valueID string) (VariableValueSnapshot, error) {
	i, err := v.valueIndex(valueID)
	if err != nil {
		return VariableValueSnapshot{}, err
	}
	return v.values[i].snapshot(), nil
}

// Scope returns every Environment, Target, and Channel any value names.
func (v *Variable) Scope() VariableScope {
	var all VariableScope
	for _, value := range v.values {
		all.EnvironmentIDs = append(all.EnvironmentIDs, value.scope.EnvironmentIDs...)
		all.TargetIDs = append(all.TargetIDs, value.scope.TargetIDs...)
		all.ChannelIDs = append(all.ChannelIDs, value.scope.ChannelIDs...)
	}
	return VariableScope{
		EnvironmentIDs: slices.Compact(sortedStrings(all.EnvironmentIDs)),
		TargetIDs:      slices.Compact(sortedStrings(all.TargetIDs)),
		ChannelIDs:     slices.Compact(sortedStrings(all.ChannelIDs)),
	}
}

func (s VariableScope) clone() VariableScope {
	return VariableScope{
		EnvironmentIDs: slices.Clone(s.EnvironmentIDs),
		TargetIDs:      slices.Clone(s.TargetIDs),
		ChannelIDs:     slices.Clone(s.ChannelIDs),
	}
}

func (s VariableScope) equal(o VariableScope) bool {
	return slices.Equal(s.EnvironmentIDs, o.EnvironmentIDs) &&
		slices.Equal(s.TargetIDs, o.TargetIDs) &&
		slices.Equal(s.ChannelIDs, o.ChannelIDs)
}

func (value *VariableValue) snapshot() VariableValueSnapshot {
	return VariableValueSnapshot{
		ID:        value.ID(),
		Value:     value.text,
		Secret:    copySecret(value.secret),
		Scope:     value.scope.clone(),
		CreatedAt: value.createdAt,
		UpdatedAt: value.updatedAt,
	}
}

func (v *Variable) Snapshot() VariableSnapshot {
	return VariableSnapshot{
		ID:             v.ID(),
		OrganizationID: v.organizationID,
		ProjectID:      copyString(v.projectID),
		ApplicationID:  copyString(v.applicationID),
		Name:           v.name,
		Description:    copyString(v.description),
		Kind:           v.kind,
		ValuesPath:     copyString(v.valuesPath),
		DeliverAs:      optional.OfPtr(v.deliverAs),
		Values:         v.Values(),
		CreatedAt:      v.createdAt,
		UpdatedAt:      v.updatedAt,
		ArchivedAt:     clock.CopyTime(v.archivedAt),
	}
}

func (v *Variable) checkActive() error {
	if v.Archived() {
		return fmt.Errorf("%w: variable %s", ErrArchived, v.ID())
	}
	return nil
}

// Update changes an active Variable. Nothing changes if any field is
// invalid. The caller checks the new values path against the other
// variables its Applications deploy with.
func (v *Variable) Update(props UpdateVariableProps) error {
	if err := v.checkActive(); err != nil {
		return err
	}
	valuesPath := v.valuesPath
	if props.ValuesPath != nil {
		valuesPath = optional.OfPtr(props.ValuesPath)
		if err := validateValuesPath(valuesPath); err != nil {
			return err
		}
	}
	deliverAs := v.deliverAs
	if props.DeliverAs != nil {
		deliverAs = props.DeliverAs
	}
	deliverAs, err := normalizeDeliverAs(v.kind, deliverAs, valuesPath)
	if err != nil {
		return err
	}
	changed := anyTrue(
		setOptional(&v.description, props.Description),
		setOptional(&v.valuesPath, props.ValuesPath),
		!equalPtr(v.deliverAs, deliverAs),
	)
	v.deliverAs = deliverAs
	v.updatedAt = clock.Now()
	if changed {
		v.RecordEvent(VariableUpdated{v.Snapshot()})
	}
	return nil
}

// AddValue adds a value and returns it. The caller checks that what its
// scope names is active and the owner's to use.
func (v *Variable) AddValue(props VariableValueProps) (VariableValueSnapshot, error) {
	if err := v.checkActive(); err != nil {
		return VariableValueSnapshot{}, err
	}
	if len(v.values) >= maxVariableValues {
		return VariableValueSnapshot{}, NewErrInvalidInput(fmt.Sprintf("a variable can have at most %d values", maxVariableValues))
	}
	content, err := v.normalizeValue(props)
	if err != nil {
		return VariableValueSnapshot{}, err
	}
	entity, err := newEntity(VariableValue{}.Prefix(), nil)
	if err != nil {
		return VariableValueSnapshot{}, err
	}
	now := clock.Now()
	value := &VariableValue{Entity: entity, text: content.Value, secret: content.Secret, scope: content.Scope, createdAt: now, updatedAt: now}
	if err := v.checkNoTie(value); err != nil {
		return VariableValueSnapshot{}, err
	}
	v.values = append(slices.Clone(v.values), value)
	v.updatedAt = now
	v.RecordEvent(VariableValueAdded{Variable: v.Snapshot(), ValueID: value.ID()})
	return value.snapshot(), nil
}

// UpdateValue replaces a value's content and scope.
func (v *Variable) UpdateValue(valueID string, props VariableValueProps) error {
	if err := v.checkActive(); err != nil {
		return err
	}
	i, err := v.valueIndex(valueID)
	if err != nil {
		return err
	}
	content, err := v.normalizeValue(props)
	if err != nil {
		return err
	}
	now := clock.Now()
	updated := *v.values[i]
	updated.text = content.Value
	updated.secret = content.Secret
	updated.scope = content.Scope
	updated.updatedAt = now
	if err := v.checkNoTie(&updated); err != nil {
		return err
	}
	changed := v.values[i].text != updated.text || !equalPtr(v.values[i].secret, updated.secret) || !v.values[i].scope.equal(updated.scope)
	values := slices.Clone(v.values)
	values[i] = &updated
	v.values = values
	v.updatedAt = now
	if changed {
		v.RecordEvent(VariableValueUpdated{Variable: v.Snapshot(), ValueID: valueID})
	}
	return nil
}

// RemoveValue removes a value.
func (v *Variable) RemoveValue(valueID string) error {
	if err := v.checkActive(); err != nil {
		return err
	}
	i, err := v.valueIndex(valueID)
	if err != nil {
		return err
	}
	v.values = slices.Delete(slices.Clone(v.values), i, i+1)
	v.updatedAt = clock.Now()
	v.RecordEvent(VariableValueRemoved{Variable: v.Snapshot(), ValueID: valueID})
	return nil
}

func (v *Variable) valueIndex(valueID string) (int, error) {
	i := slices.IndexFunc(v.values, func(value *VariableValue) bool { return value.ID() == valueID })
	if i < 0 {
		return 0, fmt.Errorf("%w: value %s in variable %s", ErrNotFound, valueID, v.ID())
	}
	return i, nil
}

// Archive archives the Variable, which frees its name. Releases that
// snapshotted it keep their copy. Archiving an archived one does nothing.
func (v *Variable) Archive() {
	if !v.Archived() {
		now := clock.Now()
		v.archivedAt = &now
		v.updatedAt = now
		v.RecordEvent(VariableArchived{v.Snapshot()})
	}
}

// CheckDeployable checks the variables one Application deploys with: its
// own and its Project's, all active. An Application's variable shadows its
// Project's variable of the same name only in value, so the two must agree
// on everything else, and no two variables may write overlapping values
// paths, since one would silently overwrite the other.
func CheckDeployable(project, application []*Variable) error {
	byName := make(map[string]*Variable, len(project))
	for _, p := range project {
		byName[strings.ToLower(p.name)] = p
	}
	effective := make([]*Variable, 0, len(project)+len(application))
	for _, a := range application {
		p, shadows := byName[strings.ToLower(a.name)]
		if shadows {
			if a.kind != p.kind || !equalPtr(a.valuesPath, p.valuesPath) || !equalPtr(a.deliverAs, p.deliverAs) {
				return NewErrInvalidInput(fmt.Sprintf(
					"application variable %s shadows the project's variable %s, so both must have the same kind, values path, and delivery", a.name, p.name))
			}
			delete(byName, strings.ToLower(a.name))
		}
		effective = append(effective, a)
	}
	for _, p := range project {
		if _, kept := byName[strings.ToLower(p.name)]; kept {
			effective = append(effective, p)
		}
	}
	// A secret never hides inside another value: only a chart's overlay
	// values files may bind one.
	secrets := map[string]string{}
	for _, v := range effective {
		if v.kind == VariableKindSecret {
			secrets[strings.ToLower(v.name)] = v.name
		}
	}
	for _, v := range effective {
		for _, value := range v.values {
			text := value.text
			if value.secret != nil {
				text = value.secret.Path
			}
			names, err := BindingNames(text)
			if err != nil {
				return err
			}
			for _, name := range names {
				if secret, ok := secrets[strings.ToLower(name)]; ok {
					return NewErrInvalidInput(fmt.Sprintf(
						"variable %s binds the secret %s; only a chart's values files may bind a secret", v.name, secret))
				}
			}
		}
	}
	for i, a := range effective {
		if a.valuesPath == nil {
			continue
		}
		for _, b := range effective[i+1:] {
			if b.valuesPath != nil && ValuesPathsOverlap(*a.valuesPath, *b.valuesPath) {
				return NewErrInvalidInput(fmt.Sprintf(
					"variables %s and %s write overlapping values paths %q and %q", a.name, b.name, *a.valuesPath, *b.valuesPath))
			}
		}
	}
	return nil
}
