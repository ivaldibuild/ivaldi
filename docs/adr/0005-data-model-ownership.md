# ADR 0005: Data model ownership and placement

- Status: Proposed
- Date: 2026-10-01

## Context

Ivaldi stores its orchestration state in PostgreSQL. Several capabilities (IAM, Work Items, Sessions, policy, credentials, provenance) each need tables. Without a placement rule, data models tend to collect in a shared package that every app imports. That couples capabilities through each other's tables and blurs the boundary between the control plane and the data plane.

[ADR 0001](0001-deployment-topology.md) establishes that only the control plane accesses its database, and that data-plane runners talk to it through its API. The repository already has top-level `migrations/postgres/` and `schemas/` directories, both empty.

## Decision

### Each capability owns its tables

The domain types for a capability live in that capability's package inside the control plane. Examples:

- IAM types live in `apps/control-plane/internal/iam`.
- Work Item types live in `apps/control-plane/internal/workitem`.

Each capability owns its tables. Other capabilities reach its data only through its Go interface, never by querying or joining its tables directly.

### Domain types are separate from database code

Within a capability:

- The **domain types and the repository interface** live in the capability package.
- The **PostgreSQL implementation** lives in a subpackage, e.g. `internal/iam/postgres`.

Domain logic never imports a database driver.

### Database models are not shared through `pkg/`

`pkg/` holds code that more than one deployable needs. Control-plane database models do not qualify, because only the control plane touches its database.

What crosses the plane boundary is versioned contracts, such as events, work assignments, and API messages. These are defined in the top-level `schemas/` directory (Protobuf or JSON Schema), and code is generated from them. Database rows are never sent between the planes directly.

### Migrations belong to the app that owns the database

Migrations are the source of truth for the schema. They move from `migrations/postgres/` to `apps/control-plane/migrations/`, so ownership is obvious and the migrations are versioned and released with the control plane.

### Tenant scoping is enforced in two layers

Every tenant-scoped table carries a tenant identifier ([ADR 0001](0001-deployment-topology.md)). The repository layer requires a tenant in every query. PostgreSQL row-level security is a second line of defense against a missing filter.

### Code-intelligence data is out of scope

The indexer's graph and vector data lives in the data plane, behind Ivaldi's graph interface. It does not use control-plane tables. Indexed records identify their repository and commit or index generation, as required for provenance.

## Consequences

- Capability boundaries stay enforceable. A capability can change its schema without breaking others, and a capability such as IAM could later be extracted into its own service without untangling cross-table joins.
- Queries that span capabilities must be composed through interfaces or dedicated read models instead of SQL joins. This costs some convenience and performance; it is accepted to keep the boundaries clean.
- Data-plane components never depend on control-plane database models, which keeps bring-your-own-compute deployments decoupled from the cloud database schema.
- Contract changes in `schemas/` need explicit versioning, because the control plane and a customer's data plane may run different versions.

## Alternatives considered

- **A shared `pkg/models` package used by every app.** Rejected: it couples capabilities and both planes to one schema and defeats the capability boundaries.
- **One database per capability now.** Rejected for now: it adds operational cost before any capability needs independent storage. Owning tables through interfaces keeps that option open.
- **Keep migrations at the repository root.** Rejected: only the control plane owns this database, so its migrations should ship with it.

## Open questions

- Which migration tool to use (e.g. goose, golang-migrate, or Atlas).
- Whether to generate query code with sqlc or write repositories by hand.
