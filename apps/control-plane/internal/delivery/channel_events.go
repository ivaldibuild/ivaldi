package delivery

// Events a Channel records. Each carries the Channel, with its rules, as it
// is after the change; the audit record keeps the state before it. Rule
// events also name the rule.

// ChannelCreated is recorded when a Channel is created.
type ChannelCreated struct {
	Channel ChannelSnapshot `json:"channel"`
}

// ChannelUpdated is recorded when a Channel's slug, display name, or
// description changes.
type ChannelUpdated struct {
	Channel ChannelSnapshot `json:"channel"`
}

// ChannelLifecycleChanged is recorded when a Channel follows another Lifecycle.
type ChannelLifecycleChanged struct {
	Channel             ChannelSnapshot `json:"channel"`
	PreviousLifecycleID string          `json:"previous_lifecycle_id"`
}

// ChannelRuleAdded is recorded when a rule is added to a Channel.
type ChannelRuleAdded struct {
	Channel ChannelSnapshot `json:"channel"`
	RuleID  string          `json:"rule_id"`
}

// ChannelRuleUpdated is recorded when a rule's kind, scope, or configuration changes.
type ChannelRuleUpdated struct {
	Channel ChannelSnapshot `json:"channel"`
	RuleID  string          `json:"rule_id"`
}

// ChannelRuleRemoved is recorded when a rule is removed. The Channel no
// longer holds it.
type ChannelRuleRemoved struct {
	Channel ChannelSnapshot `json:"channel"`
	RuleID  string          `json:"rule_id"`
}

// ChannelMadeDefault is recorded when a Channel becomes its Application's default.
type ChannelMadeDefault struct {
	Channel ChannelSnapshot `json:"channel"`
}

// ChannelDefaultCleared is recorded when a Channel stops being its
// Application's default, because another became it.
type ChannelDefaultCleared struct {
	Channel ChannelSnapshot `json:"channel"`
}

// ChannelArchived is recorded when a Channel is archived.
type ChannelArchived struct {
	Channel ChannelSnapshot `json:"channel"`
}

// ChannelRestored is recorded when an archived Channel is restored.
type ChannelRestored struct {
	Channel ChannelSnapshot `json:"channel"`
}

func (ChannelCreated) EventName() string          { return "delivery.v1.channel.created" }
func (ChannelUpdated) EventName() string          { return "delivery.v1.channel.updated" }
func (ChannelLifecycleChanged) EventName() string { return "delivery.v1.channel.lifecycle_changed" }
func (ChannelRuleAdded) EventName() string        { return "delivery.v1.channel.rule_added" }
func (ChannelRuleUpdated) EventName() string      { return "delivery.v1.channel.rule_updated" }
func (ChannelRuleRemoved) EventName() string      { return "delivery.v1.channel.rule_removed" }
func (ChannelMadeDefault) EventName() string      { return "delivery.v1.channel.made_default" }
func (ChannelDefaultCleared) EventName() string   { return "delivery.v1.channel.default_cleared" }
func (ChannelArchived) EventName() string         { return "delivery.v1.channel.archived" }
func (ChannelRestored) EventName() string         { return "delivery.v1.channel.restored" }
