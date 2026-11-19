# Notifications

- Status: Draft for review
- Date: 2026-11-18
- Related: [ADR 0005](../adr/0005-data-model-ownership.md), [ADR 0007](../adr/0007-teams-and-agent-identity.md), [ADR 0011](../adr/0011-transactional-outbox.md), [approvals](approvals.md), [delivery data model](delivery-data-model.md), [#82](https://github.com/ivaldibuild/ivaldi/issues/82)

This document defines how the control plane tells people about work that waits for them. The first such work is an approval request ([approvals](approvals.md)): when a Deployment waits for a human, the people who could approve it should hear about it rather than find it by looking. [ADR 0007](../adr/0007-teams-and-agent-identity.md) routes approval requests, alerts about degraded Deployments, and findings on observed Deployments to the owning Team, and leaves telling people to "the capability that sends the notification" ([delivery data model](delivery-data-model.md)). This is that capability: `notifications`, in `apps/control-plane/internal/notifications`.

## Decisions this model is built on

- **Notifications react to domain events, and own nothing else.** Capabilities already write their events to the outbox in the transaction that changes the object ([ADR 0011](../adr/0011-transactional-outbox.md)), and `serve`'s relay publishes them to the `ivaldi:control-plane:events` stream. The notifications capability consumes that stream with its own consumer group, in `serve`, as the webhook consumer reads the gateway's stream. No capability calls it, so telling people never slows or fails the change that caused it.
- **A notification is one message to one person.** It is a record the person reads in the web app, and possibly an email. It is derived: the approval request stays the source of truth for what waits, and a notification only points at it.
- **Only people who could act are told.** For a waiting approval request, recipients are the users who could decide it now, as `can_decide` works it out ([approvals](approvals.md#reading-one-request)): they hold every requirement, and are neither the requester nor the person the requesting Session acts for. Telling someone who cannot act would teach people to ignore the messages.
- **The owning Team comes first.** The request's `owner_team_id` names the Team the subject belonged to when it was asked. Its members who are users, and could decide, are told. Ownership is routing, not access, so members who could not decide are not told, and people outside the Team who could decide are not told either while someone in the Team can.
- **When the Team cannot act, the Organization's owners and admins are told.** If the request has no owning Team, or none of its members could decide it, the recipients are the Organization's owners and admins who could. If none of them could either, nobody is notified; the request still shows on `GET /v1/approvals` for anyone who could decide it, and that gap is logged.
- **The requester's side hears the outcome.** When a request is approved or rejected, the person the requesting Session acts for is told, with the decider's note. An agent learns the outcome through the API, from its subject or its request. When a user asked directly, that user is told.
- **A redelivered event adds no notification.** The stream delivers at least once. A notification is unique per event and recipient, so handling an event again adds none. An email that was sent is recorded, so it is not sent again, with one exception: if the process stops after the SMTP server accepts a message but before it is recorded, the message is sent again. SMTP offers no way to submit a message idempotently, so a rare duplicate email is accepted rather than a missed one.
- **A closed request's notifications are resolved.** When a request is approved, rejected, or cancelled, the notifications that asked people to decide it are marked resolved, so the inbox does not keep asking. They are kept, read or not.
- **Two channels to start: the inbox and email.** The inbox needs nothing outside Ivaldi. Email goes over SMTP, is off until an operator configures it, and never carries more than the inbox does. Chat and webhook channels per Team are deferred: their URLs are credentials, which belong to the credentials capability ([ADR 0004](../adr/0004-credentials-and-keys.md)).
- **Approval requests name themselves.** An inbox item or an email needs a line such as "Deploy api 1.4.0 to production / eu-west". The summary a subject's capability stores is typed per subject, and notifications should not know every subject type. So an approval request gains a `title`, written by the subject's capability when it opens the request, as it writes the summary. For a Deployment, delivery writes the Application's, Environment's, and Target's names and the Release's version, as they were when it was asked.
- **Notifications are contact records, not changes.** Creating, sending, and reading them is not an action anyone authorizes, so, like agent reports and last-used times, they record no domain event and no audit record. Sending an email is logged.
- **Conventions match the other capabilities.** Tables live in the `notifications` schema, every table has `organization_id`, and IDs are prefixed ULIDs stored as `varchar(30)`. References to other capabilities have no foreign key ([ADR 0005](../adr/0005-data-model-ownership.md)).

  | Table | Prefix |
  |---|---|
  | notifications | `ntf` |

## Entities

- **notifications**: one message to one person. Its fields:
  - `recipient_principal_id` is the user it is for.
  - `kind` is `approval_requested` or `approval_decided`, so far.
  - `subject_type` and `subject_id` name what it is about: `policy.approval_request` and the request's ID, for both kinds.
  - `event_id` is the outbox event that caused it. With the recipient, it is unique.
  - `title` and `body` are what the person reads: the request's title, and for a decision its outcome and note. They are copied when the notification is made, so the inbox needs no call into other capabilities.
  - `link` is the path in the web app the notification opens, such as `/approvals/apr_…`. Email turns it into a full address with `external_url`.
  - `owner_team_id` is the Team the request was routed to, when it was routed through one.
  - `read_at` is set when the recipient reads it, and `resolved_at` when what it asked for no longer waits.
  - `email_status` is `pending`, `sent`, or `not_sent` (email is off, or the recipient has no address), with `emailed_at` once sent.
  - `created_at`.

A notification is never deleted for now; pruning old ones waits for the worker's scheduled jobs (see [Deferred](#deferred)).

## How the main flows work

### An approval request is opened

On `policy.v1.approval_request.created`:

1. The handler reads the request through policy. If it is no longer pending, it stops: it was decided or cancelled before the event was handled.
2. It finds the recipients:
   - the owning Team's members who are users, through `team.Directory`;
   - of those, the ones who could decide the request, through a policy lookup that applies `can_decide` to a list of principals;
   - if that leaves no one, the Organization's owners and admins, from IAM, filtered the same way.
3. In one transaction, it inserts an `approval_requested` notification for each recipient, skipping any that exist for this event and recipient. It then reads the request again: if it closed meanwhile, it resolves them and stops.
4. If email is on, it reads the recipients' addresses from IAM and sends one email to each notification of this event whose `email_status` is `pending`, marking each `sent` once the SMTP server accepts it. Recipients without an address are marked `not_sent`.

If an email fails to send, the handler stops sending, since the server is most likely down, and returns an error so the consumer delivers the event again later. Stopping at the first failure also keeps a down server from holding the event past the consumer's claim timeout, when it would be handled twice at once. Step 3 then adds nothing, and step 4 retries only the emails that were not sent. After the consumer's last attempt the event goes to the dead-letter stream, and those emails stay `pending`; their notifications are still in the inbox.

### An approval request is decided or cancelled

On `policy.v1.approval_request.approved`, `.rejected`, or `.cancelled`:

1. The handler marks the request's `approval_requested` notifications resolved.
2. For an approval or a rejection, it notifies the person the requesting Session acted for, or the user who asked directly, with an `approval_decided` notification naming the outcome, the decider, and the note. It emails them the same way. A cancellation tells no one: whoever cancelled it, or withdrew or archived what it waited on, already knows.

### Reading the inbox

- `GET /v1/notifications` lists the caller's notifications, newest first, paged by cursor. `unread=true` keeps only unread ones.
- `GET /v1/notifications/unread-count` returns the number of unread, unresolved notifications, for the web app's bell.
- `POST /v1/notifications/{id}/read` marks one read, and `POST /v1/notifications/read` marks them all.

Each operation checks `notification.read` on the caller's Organization, which every member holds by IAM's rule for read permissions, and touches only the caller's own notifications. Another person's notification reads as not found.

### Email

An email has the notification's title as its subject and its body as text, with the link as a full address. It never carries a secret: an approval summary holds secret references at most, and the email does not include the summary. The sender sits behind an interface, `notifications.Mailer`, so tests use a fake and another provider can follow.

Configuration, under `notifications.email`:

| Key | Meaning |
|---|---|
| `smtp_address` | `host:port` of the SMTP server. Email is off while it is empty. |
| `username` | Account to authenticate as, if the server needs one. |
| `password_file` | File holding the password, like the other secrets in the configuration. |
| `from` | The sender address, such as `Ivaldi <ivaldi@example.com>`. |
| `tls` | `starttls` (the default), `tls`, or `none`, which is refused unless the address is a loopback one. |

`notifications.consumer_group` (default `notifications`) and `notifications.concurrency` set how the consumer reads the outbox stream. The mailer is `notifications/smtpmail`, which uses `net/smtp`: with `starttls` it refuses a server that does not offer STARTTLS rather than sending the password in plain text, and it folds line breaks out of the subject so a title cannot add a header.

## Changes to other capabilities

- **policy**: approval requests gain `title` (a new migration, which gives existing rows one made of their subject type and ID), which `OpenApprovalInput` carries and the API returns. A lookup, unauthorized like `policy.Approvals`, says which of a list of principals could decide a request.
- **delivery**: `RequestDeployments` writes the title.
- **team**: `team.Directory` lists an active Team's members.
- **iam**: a lookup lists the Organization's owners and admins, and one returns active users' email addresses and names.
- **web app**: a bell with the unread count, an inbox, and an approval page at `/approvals/{id}` for the links to open.

## Invariants

- **A person is told about a waiting request only if they could decide it** when the event was handled.
- **One notification per event and recipient.** Its email is sent once, and again only if sending was interrupted after the SMTP server accepted it but before that was recorded.
- **A notification for a closed request is resolved.** Handlers run concurrently, so a request can close while its created event is being handled, and the closed event's handler can resolve before the new notifications exist. After inserting them, the created handler reads the request again and, if it is no longer pending, resolves them itself and sends no email. Either that read sees the close, or the close committed after the insert and its handler resolves them, so no lock across capabilities is needed.
- **Only its recipient reads a notification.**

## Building it

Under [#82](https://github.com/ivaldibuild/ivaldi/issues/82), in this order:

1. This design. Done.
2. `title` on approval requests, and delivery writing it. Done.
3. The capability: its migrations, the consumer and its handlers, the recipients, the inbox API, and `make openapi`. Done.
4. Email: the `email_status` and `emailed_at` columns, the mailer, its configuration, and [a page in `docs/self-hosting/`](../self-hosting/notifications.md). Done.
5. The web app's bell, inbox, and approval page. Done: the bell sits in the top bar and asks for the unread count every minute and on each navigation; `/inbox` lists notifications and marks one read as it opens; `/approvals/{id}` shows what waits and who asked, and lets someone who could decide it approve or reject it, with a note that rejecting requires. A waiting Deployment's page links to its request.

## Deferred

- **Chat and webhooks.** A Team may want its requests in a channel. Those URLs are credentials and belong to the credentials capability.
- **Preferences.** People may want to turn email off, or have a digest. Everyone who could act gets both channels until then.
- **Reminders, expiry, and escalation.** A request nobody acts on could be raised again, or to someone else. These need scheduled jobs, which wait for the worker, as approval expiry does ([approvals](approvals.md#deferred)).
- **Other events.** ADR 0007 also routes degraded Deployments, findings on observed Deployments, and incident Work Items to the owning Team. Each adds a kind and a handler here when its capability needs it.
- **Pruning** old notifications, a scheduled job too.

## Open questions

- **Autonomous Sessions.** A Session with no delegator ([ADR 0007](../adr/0007-teams-and-agent-identity.md)) has no person to hear its request's outcome. Its Agent's Team leads are the likely audience, once Sessions name their Agent.

## Appendix: planned schema

The migrations will live in `apps/control-plane/internal/notifications/migrations/`, with versions kept in `public.notifications_schema_migrations`.

| Capability | Migration | Creates | Scope |
|---|---|---|---|
| notifications | 000001 | the `notifications` schema | — |
| notifications | 000002 | `notifications`, unique on `(organization_id, event_id, recipient_principal_id)`, with an `(organization_id, recipient_principal_id, created_at, id)` index for the inbox | tenant |
| notifications | 000003 | `email_status` and `emailed_at` on `notifications`, with a partial index on `(organization_id, event_id)` for the emails still pending | tenant |
| policy | next | `title` on `approval_requests`, `VARCHAR(255) NOT NULL` | tenant |
