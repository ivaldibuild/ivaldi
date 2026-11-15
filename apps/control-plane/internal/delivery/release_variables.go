package delivery

import (
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// ReleaseVariablesStatus says whether a revision is the one Deployments of
// its Release use.
type ReleaseVariablesStatus string

const (
	// ReleaseVariablesCurrent is the revision new Deployments use. A Release
	// has at most one.
	ReleaseVariablesCurrent ReleaseVariablesStatus = "current"
	// ReleaseVariablesSuperseded is a revision a later one replaced. The
	// Deployments that used it keep referring to it.
	ReleaseVariablesSuperseded ReleaseVariablesStatus = "superseded"
)

// ReleaseVariables is one revision of the variables a Release deploys with:
// a snapshot of its Application's variables and its Project's, taken when
// the Release was created or its variables were refreshed
// (docs/design/deployment-variables.md). Its content never changes, so
// redeploying a Release, or rolling back to it, uses the configuration it
// was released with, not whatever the Application holds that day.
type ReleaseVariables struct {
	aggregate.Root
	organizationID string
	applicationID  string
	releaseID      string
	revision       int
	status         ReleaseVariablesStatus
	variables      []SnapshotVariable
	takenBy        *string
	createdAt      time.Time
	updatedAt      time.Time
}

// SnapshotVariable is a variable as a revision holds it, with every value
// it had. An Application's variable and the Project's variable it shadows
// are both kept, each with its source.
type SnapshotVariable struct {
	Name       string             `json:"name"`
	Source     VariableSource     `json:"source" enum:"project,application"`
	Kind       VariableKind       `json:"kind" enum:"text,secret"`
	ValuesPath *string            `json:"values_path"`
	DeliverAs  *VariableDeliverAs `json:"deliver_as" enum:"secret,value"`
	Values     []SnapshotValue    `json:"values"`
}

// SnapshotValue is one value of a variable in a revision.
type SnapshotValue struct {
	Value string `json:"value"`
	// Secret is a secret variable's reference; a revision never holds a
	// secret's value.
	Secret *SecretReference `json:"secret"`
	Scope  VariableScope    `json:"scope"`
}

// ReleaseVariablesSnapshot is a revision's state, for repositories, audit
// records, and events. Its JSON form is part of the revision events'
// contract.
type ReleaseVariablesSnapshot struct {
	ID                 string                 `json:"id"`
	OrganizationID     string                 `json:"organization_id"`
	ApplicationID      string                 `json:"application_id"`
	ReleaseID          string                 `json:"release_id"`
	Revision           int                    `json:"revision"`
	Status             ReleaseVariablesStatus `json:"status" enum:"current,superseded"`
	Variables          []SnapshotVariable     `json:"variables"`
	TakenByPrincipalID *string                `json:"taken_by_principal_id"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

// NewReleaseVariablesProps describes a revision. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewReleaseVariablesProps struct {
	OrganizationID string
	ApplicationID  string
	ReleaseID      string
	// Revision counts a Release's revisions from 1.
	Revision int
	// Status defaults to current.
	Status    ReleaseVariablesStatus
	Variables []SnapshotVariable
	// TakenByPrincipalID is who created or refreshed the Release; nil when
	// Ivaldi observed it.
	TakenByPrincipalID *string
	CreatedAt          *time.Time
	UpdatedAt          *time.Time
}

// NewReleaseVariables returns a revision, with a new ID unless one is given.
func NewReleaseVariables(props NewReleaseVariablesProps, revisionID *string) (*ReleaseVariables, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !id.Valid(Application{}.Prefix(), props.ApplicationID) {
		return nil, NewErrInvalidInput("a release's variables need an application ID")
	}
	if !id.Valid(Release{}.Prefix(), props.ReleaseID) {
		return nil, NewErrInvalidInput("a release's variables need a release ID")
	}
	if props.Revision < 1 {
		return nil, NewErrInvalidInput("revisions count from 1")
	}
	if props.Status == "" {
		props.Status = ReleaseVariablesCurrent
	}
	if props.Status != ReleaseVariablesCurrent && props.Status != ReleaseVariablesSuperseded {
		return nil, NewErrInvalidInput(fmt.Sprintf("revision status %q must be current or superseded", props.Status))
	}
	root, err := newRoot(ReleaseVariables{}.Prefix(), revisionID)
	if err != nil {
		return nil, err
	}
	now := clock.Now()
	rv := &ReleaseVariables{
		Root:           root,
		organizationID: props.OrganizationID,
		applicationID:  props.ApplicationID,
		releaseID:      props.ReleaseID,
		revision:       props.Revision,
		status:         props.Status,
		variables:      copySnapshotVariables(props.Variables),
		takenBy:        optional.OfPtr(props.TakenByPrincipalID),
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
	}
	if rv.IsNew() {
		rv.RecordEvent(ReleaseVariablesTaken{rv.Snapshot()})
	}
	return rv, nil
}

func copySnapshotVariables(in []SnapshotVariable) []SnapshotVariable {
	out := make([]SnapshotVariable, len(in))
	for i, v := range in {
		out[i] = SnapshotVariable{
			Name: v.Name, Source: v.Source, Kind: v.Kind,
			ValuesPath: copyString(v.ValuesPath), DeliverAs: optional.OfPtr(v.DeliverAs),
			Values: make([]SnapshotValue, len(v.Values)),
		}
		for j, value := range v.Values {
			out[i].Values[j] = SnapshotValue{Value: value.Value, Secret: copySecret(value.Secret), Scope: value.Scope.clone()}
		}
	}
	return out
}

// SnapshotVariables returns what a revision holds for an Application: its
// own active variables, then its Project's. It checks them once more, as
// the variables may have changed since each was saved.
func SnapshotVariables(project, application []*Variable) ([]SnapshotVariable, error) {
	if err := CheckDeployable(project, application); err != nil {
		return nil, err
	}
	out := make([]SnapshotVariable, 0, len(project)+len(application))
	for _, v := range slices.Concat(application, project) {
		s := v.Snapshot()
		values := make([]SnapshotValue, len(s.Values))
		for i, value := range s.Values {
			values[i] = SnapshotValue{Value: value.Value, Secret: value.Secret, Scope: value.Scope}
		}
		out = append(out, SnapshotVariable{
			Name: s.Name, Source: v.Source(), Kind: s.Kind,
			ValuesPath: s.ValuesPath, DeliverAs: s.DeliverAs, Values: values,
		})
	}
	return out, nil
}

func (ReleaseVariables) Prefix() string { return "vsn" }

func (rv *ReleaseVariables) OrganizationID() string         { return rv.organizationID }
func (rv *ReleaseVariables) ApplicationID() string          { return rv.applicationID }
func (rv *ReleaseVariables) ReleaseID() string              { return rv.releaseID }
func (rv *ReleaseVariables) Revision() int                  { return rv.revision }
func (rv *ReleaseVariables) Status() ReleaseVariablesStatus { return rv.status }
func (rv *ReleaseVariables) CreatedAt() time.Time           { return rv.createdAt }
func (rv *ReleaseVariables) UpdatedAt() time.Time           { return rv.updatedAt }

// Variables returns the variables the revision holds.
func (rv *ReleaseVariables) Variables() []SnapshotVariable {
	return copySnapshotVariables(rv.variables)
}

func (rv *ReleaseVariables) Snapshot() ReleaseVariablesSnapshot {
	return ReleaseVariablesSnapshot{
		ID:                 rv.ID(),
		OrganizationID:     rv.organizationID,
		ApplicationID:      rv.applicationID,
		ReleaseID:          rv.releaseID,
		Revision:           rv.revision,
		Status:             rv.status,
		Variables:          rv.Variables(),
		TakenByPrincipalID: copyString(rv.takenBy),
		CreatedAt:          rv.createdAt,
		UpdatedAt:          rv.updatedAt,
	}
}

// Supersede marks the revision replaced by a later one. Superseding a
// superseded revision does nothing.
func (rv *ReleaseVariables) Supersede() {
	if rv.status != ReleaseVariablesSuperseded {
		rv.status = ReleaseVariablesSuperseded
		rv.updatedAt = clock.Now()
		rv.RecordEvent(ReleaseVariablesRevisionSuperseded{rv.Snapshot()})
	}
}
