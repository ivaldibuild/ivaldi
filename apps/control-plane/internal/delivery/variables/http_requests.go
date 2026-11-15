package variables

import "github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"

// ScopeBody names the Deployments a value applies to.
type ScopeBody struct {
	EnvironmentIDs []string `json:"environment_ids,omitempty" required:"false" doc:"Environments the value applies to; any when omitted"`
	TargetIDs      []string `json:"target_ids,omitempty" required:"false" doc:"Targets the value applies to; any when omitted"`
	ChannelIDs     []string `json:"channel_ids,omitempty" required:"false" doc:"The Application's Channels the value applies to; any when omitted. Not allowed on a Project's variable"`
}

// ValueBody is a variable's value as a request gives it.
type ValueBody struct {
	Value  string      `json:"value,omitempty" required:"false" doc:"A text variable's value, which may bind other variables with #{Name}"`
	Secret *SecretBody `json:"secret,omitempty" required:"false" doc:"A secret variable's value: where the secret is kept, never the secret itself"`
	Scope  *ScopeBody  `json:"scope,omitempty" required:"false" doc:"The Deployments the value applies to; every Deployment when omitted"`
}

// SecretBody says where a secret is kept.
type SecretBody struct {
	StoreID string `json:"store_id" required:"true" doc:"The Secret Store, in the variable's Workspace"`
	Path    string `json:"path" required:"true" minLength:"1" doc:"Where the secret is in the store, such as a Secret's name or a Vault path; may bind text variables with #{Name}"`
	Field   string `json:"field,omitempty" required:"false" doc:"One key of a map or JSON secret; the whole value when omitted"`
	Version string `json:"version,omitempty" required:"false" doc:"The store's version to deploy; the latest when omitted"`
}

func (b ValueBody) props() delivery.VariableValueProps {
	p := delivery.VariableValueProps{Value: b.Value}
	if b.Secret != nil {
		p.Secret = &delivery.SecretReference{StoreID: b.Secret.StoreID, Path: b.Secret.Path, Field: b.Secret.Field, Version: b.Secret.Version}
	}
	if b.Scope != nil {
		p.Scope = delivery.VariableScope{EnvironmentIDs: b.Scope.EnvironmentIDs, TargetIDs: b.Scope.TargetIDs, ChannelIDs: b.Scope.ChannelIDs}
	}
	return p
}

// VariableBody describes a variable to create.
type VariableBody struct {
	Name        string      `json:"name" required:"true" minLength:"1" maxLength:"253" doc:"The variable's name, unique within its owner whatever the case, such as Database.Host"`
	Description *string     `json:"description,omitempty" required:"false" doc:"What the variable is for"`
	Kind        string      `json:"kind,omitempty" required:"false" enum:"text,secret" doc:"What its values hold: text, or references to secrets in a Secret Store; text when omitted"`
	DeliverAs   string      `json:"deliver_as,omitempty" required:"false" enum:"secret,value" doc:"How a secret variable reaches the chart: in a Kubernetes Secret (the default), or written into the chart's values at values_path, where Helm's release history keeps it"`
	ValuesPath  *string     `json:"values_path,omitempty" required:"false" doc:"Dotted path in the chart's values the variable is written to, such as database.host; omit for a variable only used through bindings"`
	Values      []ValueBody `json:"values,omitempty" required:"false" doc:"The variable's values"`
}

func (b VariableBody) input(organizationID string, owner delivery.VariableOwner) delivery.CreateVariableInput {
	in := delivery.CreateVariableInput{
		OrganizationID: organizationID,
		Owner:          owner,
		Name:           b.Name,
		Kind:           delivery.VariableKind(b.Kind),
		DeliverAs:      delivery.VariableDeliverAs(b.DeliverAs),
	}
	if in.Kind == "" {
		in.Kind = delivery.VariableKindText
	}
	if b.Description != nil {
		in.Description = *b.Description
	}
	if b.ValuesPath != nil {
		in.ValuesPath = *b.ValuesPath
	}
	for _, value := range b.Values {
		in.Values = append(in.Values, value.props())
	}
	return in
}

type CreateProjectVariableRequest struct {
	ProjectID string `path:"project_id" doc:"The Project the variable belongs to"`
	Body      VariableBody
}

type ListProjectVariablesRequest struct {
	ProjectID string `path:"project_id" doc:"The Project to list variables of"`
}

type CreateApplicationVariableRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  VariableBody
}

type ListApplicationVariablesRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	IncludeInherited      bool   `query:"include_inherited" doc:"Also return the variables of the Application's Project, after its own"`
}

type VariableRequest struct {
	VariableID string `path:"variable_id" doc:"The variable's ID"`
}

type UpdateVariableRequest struct {
	VariableID string `path:"variable_id" doc:"The variable's ID"`
	Body       struct {
		Description *string `json:"description,omitempty" required:"false" doc:"New description; empty clears it"`
		ValuesPath  *string `json:"values_path,omitempty" required:"false" doc:"New values path; empty clears it"`
		DeliverAs   *string `json:"deliver_as,omitempty" required:"false" enum:"secret,value" doc:"How a secret variable reaches the chart"`
	}
}

func (r *UpdateVariableRequest) props() delivery.UpdateVariableProps {
	props := delivery.UpdateVariableProps{Description: r.Body.Description, ValuesPath: r.Body.ValuesPath}
	if r.Body.DeliverAs != nil {
		props.DeliverAs = new(delivery.VariableDeliverAs(*r.Body.DeliverAs))
	}
	return props
}

type AddVariableValueRequest struct {
	VariableID string `path:"variable_id" doc:"The variable's ID"`
	Body       ValueBody
}

type VariableValueRequest struct {
	VariableID string `path:"variable_id" doc:"The variable's ID"`
	ValueID    string `path:"value_id" doc:"The value's ID"`
}

type UpdateVariableValueRequest struct {
	VariableID string `path:"variable_id" doc:"The variable's ID"`
	ValueID    string `path:"value_id" doc:"The value's ID"`
	Body       ValueBody
}
