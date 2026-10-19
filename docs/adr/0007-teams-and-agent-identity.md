# ADR 0007: Teams and durable Agent identity

- Status: Proposed
- Date: 2026-10-18
- Amends: [ADR 0003](0003-identity-access-and-policy.md), which requires every agent Session to have a delegating human

## Context

Ivaldi is a workspace where humans and agents build and run software together. Work needs owners: someone must approve a production Deployment, respond when it degrades, and pick up the Work Item that follows an incident. In Ivaldi, those owners are teams that include both humans and agents.

Two gaps stand in the way:

- **There is no Team.** IAM has groups ([IAM data model](../design/iam-data-model.md)), but they exist to receive grants, and they are often synced from a customer's identity provider through SCIM. Ownership is a different question from access: a group answers "who may do this", while a Team answers "who is responsible for this".
- **An agent has no lasting identity.** IAM's principals are users, service accounts, agent Sessions, and workloads ([ADR 0003](0003-identity-access-and-policy.md)). An agent Session exists for one piece of work and then expires. Nothing represents "the release agent" across Sessions, so an agent cannot be a named member of a team, build up a history, or be given its own grants.

## Decision

### Teams are their own capability

Teams live in `apps/control-plane/internal/team`. A Team belongs to an Organization and has a `slug`, a display name, a description, and members. A member is either a user or an Agent, with a role on the Team (`lead` or `member`).

**Ownership is recorded by the capability that owns the thing.** The workspace capability stores a Project's owning Team ([workspace data model](../design/workspace-data-model.md)), and the delivery capability stores an Application's, which defaults to its Project's ([delivery data model](../design/delivery-data-model.md)). Later capabilities do the same for what they own. The Team capability does not keep a list of what each Team owns, so no capability has to query another's tables ([ADR 0005](0005-data-model-ownership.md)).

**Ownership means responsibility, not access.** The owning Team is where work is routed: approval requests, alerts about degraded Deployments, findings on observed Deployments, and Work Items created from incidents. Ownership grants nothing. Access still comes only from IAM grants, which keeps access separate from everything else ([ADR 0003](0003-identity-access-and-policy.md)).

**Each Team has a matching IAM group, so it can receive grants.** The Team capability registers a group with IAM and keeps its membership in sync, the same way Environments are registered as IAM resources. When an Application is created, the delivery capability can also grant its owning Team the usual roles on the Application's Project. That is a separate grant that can be changed later, not a side effect of ownership. SCIM-synced groups are unchanged; team groups are marked as managed by the Team capability, and only that capability can change their membership.

### Agents get a durable identity

IAM gains a new principal kind, `agent`, with an **agents** subtype table. An Agent is a lasting identity such as `release-agent` or `triage-agent`. It has a slug, a display name, and a status, and it can belong to Teams and receive grants.

IAM holds only the identity. How an Agent runs (its harness such as Claude Code, Codex, or OpenCode, its model, its instructions, and its tools) is configuration owned by the orchestration capability, not by IAM.

**Every Agent belongs to at least one Team.** That Team is accountable for the Agent, and it is where questions about the Agent's work are routed.

**Every agent Session runs as an Agent.** `agent_sessions` gains the ID of the Agent the Session runs as. A Session's effective access becomes the intersection of:

- the delegating principal's access, when the Session has a delegator,
- the Agent's own access, from its grants and its Teams' grants, and
- the Session's scope,

and policy then decides as before. An Agent therefore never gives a human more access than they already have, and a human's Session never gives an Agent more access than it was granted.

### Agents can start work alone, but a human approves anything that ships

An Agent can start a Session with **no human delegator**: for example, when an incident event, a schedule, or a new Work Item triggers it. Such a Session is **autonomous**. Its access is the Agent's access intersected with the Session's scope, and it records what started it.

An autonomous Session can investigate, change code in its sandbox, run checks, open pull requests, create Work Items, and prepare draft Releases. It cannot **release a change** without a human's approval. Releasing a change means any of these actions:

- merging a Change into a branch other than the Session's own working branches;
- publishing a Release;
- executing a Deployment to any Environment, including a rollback.

For these actions, policy always returns "requires approval" when the Session is autonomous. This is a built-in rule. An Organization's policies can add further requirements, but they cannot remove this one.

The approval must come from a human user, never from an Agent or a service account. The approver must hold the access the action needs, so approving does not let anyone ship something they could not ship themselves. The approval is recorded by the policy capability and linked to the Session. For what ships, the approving human becomes the accountable person in the provenance chain. Before anything ships, the Agent's Team is accountable.

Sessions with a human delegator are unchanged for these actions: they follow the Organization's policies, which may or may not require approval.

### Every agent Session needs a human's approval to change settings

Settings change how Ivaldi behaves for everyone in their scope, for example which Lifecycle new Channels follow ([workspace data model](../design/workspace-data-model.md)). So any agent Session, **with or without a delegator**, needs a human's approval to change or remove a setting of any kind, at any level. This is a second built-in policy rule that an Organization's policies can add to but not remove.

The approver must be a human user who could make the change themselves. In a Session with a delegator, the delegator can approve. The pending change is held by the policy capability with the value it proposes, and it applies only if the setting has not changed since the request. If it has, the approval lapses and the Agent must ask again, so nobody approves a change against a value they did not see.

### Teams and Agents are domain terms

Team and Agent are added to the domain vocabulary in `AGENTS.md`. Agent is already listed there, and this ADR defines it as the durable identity; one run of an Agent is a Session.

## Consequences

- Every piece of delivery work has an accountable owner, and agents can be named owners rather than hidden inside a human's Session.
- Work routing (approvals, alerts, incident Work Items) has one source to read from: the owning Team, recorded by the owning capability.
- An Agent's actions can be traced across Sessions to one identity, which supports review and learning from its history.
- IAM changes: a new principal kind and subtype table, an Agent reference on `agent_sessions`, and team-managed groups. These need new IAM migrations; existing migrations are not edited.
- Authorization for agent Sessions gets one more input, the Agent's own grants. The authorizer must load them, and its tests must cover all three parts of the intersection, including Sessions with no delegator.
- `agent_sessions.delegator_principal_id` becomes nullable, through a new IAM migration.
- Agents can do useful work, such as triaging an incident overnight, without waiting for a human to start them. Nothing they produce reaches a shared branch, a published Release, or an Environment until a human approves it.
- During an incident, an autonomous Agent can prepare a rollback but cannot apply it alone. Rollbacks wait for a human, which costs time when every minute counts. This is accepted for now; see the open questions.
- Every Change that ships is still traceable to a person, either the Session's delegator or the human who approved the release.
- Agents can propose configuration changes but never apply one alone, so how Ivaldi behaves is always set by a person.
- Teams depend on IAM for grants through their group, but IAM does not depend on Teams.

## Alternatives considered

- **Teams as IAM groups with agent members.** Rejected: it mixes product ownership with access groups that are often synced from an identity provider, and IAM would start holding responsibilities that are not access.
- **Ownership grants access.** Rejected: it would bury access decisions in ownership, against [ADR 0003](0003-identity-access-and-policy.md). It would also force Applications to become IAM resources so grants could attach to them.
- **Agents only as Sessions.** Rejected: an agent would have no identity between Sessions, could not be a named Team member, and could not hold grants of its own.
- **Agents as service accounts.** Rejected: service accounts are for integrations with API tokens. Agents run Sessions, with or without a delegator, and they need their own place in authorization and audit.
- **Autonomous Sessions borrow a human delegator**, such as the Agent's Team lead. Rejected: the human would not have started or seen the work, so the provenance chain would name the wrong person.
- **Autonomous Sessions may release to low-risk Environments without approval.** Rejected for now: "any change that ships needs a human" is a rule people can rely on without reading policy. Relaxing it later is easier than tightening it.

## Open questions

- **What may start an autonomous Session.** Which triggers are allowed (incident events, schedules, Work Item assignment), and who may configure them?
- **Emergency rollback.** Should there be a narrow exception, for example rolling back to the previous healthy Release in an Environment, if policy allows it explicitly? Or should the human approval stay absolute?
- **Approval routing.** Approvals go to the owning Team of the thing being shipped, such as the Application. If no one approves in time, should the request expire or escalate?
- **Shared ownership.** Is one owning Team per Application enough, or are co-owners or a separate on-call Team needed?
- **Team scope.** Teams belong to the Organization here, so one Team can own Applications in several Workspaces. Do some customers need Workspace-scoped Teams?
- **Syncing Teams from an identity provider.** Should Teams be importable from identity-provider groups, while still allowing Agents as members?
