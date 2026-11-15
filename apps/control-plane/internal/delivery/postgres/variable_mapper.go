package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type variableMapper struct{}

func newVariableMapper() *variableMapper {
	return &variableMapper{}
}

// variableRows are the rows of one Variable.
type variableRows struct {
	variable *variableEntity
	values   []variableValueEntity
}

func (m *variableMapper) ToPersistence(v *delivery.Variable) variableRows {
	s := v.Snapshot()
	rows := variableRows{variable: &variableEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		ProjectID:      s.ProjectID,
		ApplicationID:  s.ApplicationID,
		Name:           s.Name,
		Description:    s.Description,
		Kind:           string(s.Kind),
		ValuesPath:     s.ValuesPath,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
		ArchivedAt:     s.ArchivedAt,
	}}
	if s.DeliverAs != nil {
		rows.variable.DeliverAs = new(string(*s.DeliverAs))
	}
	for _, value := range s.Values {
		rows.values = append(rows.values, variableValueEntity{
			ID:             value.ID,
			OrganizationID: s.OrganizationID,
			VariableID:     s.ID,
			TextValue:      new(value.Value),
			EnvironmentIDs: database.TextArray(value.Scope.EnvironmentIDs),
			TargetIDs:      database.TextArray(value.Scope.TargetIDs),
			ChannelIDs:     database.TextArray(value.Scope.ChannelIDs),
			CreatedAt:      value.CreatedAt,
			UpdatedAt:      value.UpdatedAt,
		})
	}
	return rows
}

// ToDomain builds a Variable from its row and its values in the order they
// were added.
func (m *variableMapper) ToDomain(rows variableRows) (*delivery.Variable, error) {
	values := make([]delivery.NewVariableValueProps, len(rows.values))
	for i, value := range rows.values {
		values[i] = delivery.NewVariableValueProps{
			ID: &value.ID,
			Scope: delivery.VariableScope{
				EnvironmentIDs: value.EnvironmentIDs,
				TargetIDs:      value.TargetIDs,
				ChannelIDs:     value.ChannelIDs,
			},
			CreatedAt: utc(value.CreatedAt),
			UpdatedAt: utc(value.UpdatedAt),
		}
		if value.TextValue != nil {
			values[i].Value = *value.TextValue
		}
	}
	e := rows.variable
	props := delivery.NewVariableProps{
		OrganizationID: e.OrganizationID,
		ProjectID:      e.ProjectID,
		ApplicationID:  e.ApplicationID,
		Name:           e.Name,
		Description:    e.Description,
		Kind:           delivery.VariableKind(e.Kind),
		ValuesPath:     e.ValuesPath,
		Values:         values,
		CreatedAt:      utc(e.CreatedAt),
		UpdatedAt:      utc(e.UpdatedAt),
		ArchivedAt:     utcPtr(e.ArchivedAt),
	}
	if e.DeliverAs != nil {
		props.DeliverAs = new(delivery.VariableDeliverAs(*e.DeliverAs))
	}
	return delivery.NewVariable(props, &e.ID)
}
