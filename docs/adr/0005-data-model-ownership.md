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

### Migrations belong to the app that owns the database, colocated per capability

Migrations are the source of truth for the schema. They move from `migrations/postgres/` into the control plane, and live inside the capability package that owns the tables they create — e.g. `apps/control-plane/internal/iam/migrations/` — rather than in one shared `apps/control-plane/migrations/` directory. Each capability embeds its own migration files (`go:embed`) through its package, and the control plane applies every capability's migrations together at startup. This keeps ownership obvious at the file-tree level, consistent with each capability owning its tables, and the migrations still version and release with the control plane binary.

### Migrations are managed with golang-migrate

golang-migrate applies versioned, hand-written `.up.sql`/`.down.sql` files directly; there is no separate desired-state schema file to diff against. This fits the decision above that migrations themselves are the source of truth, and keeps the mental model simpler than a schema-diffing tool such as Atlas.

### Repositories use GORM, and only for queries

Each capability's `postgres` subpackage implements its repository interface with GORM on the `pgx` driver. GORM is a query layer only:

- **The schema stays with golang-migrate.** `AutoMigrate` is never used, so the migration files remain the only source of truth for the schema.
- **GORM stays in the `postgres` subpackage.** Its row structs and tags live there and are mapped to the capability's domain types, which have no GORM tags. Domain code never imports GORM.
- **Database errors become domain errors.** The repository translates PostgreSQL constraint violations by constraint name, for example a unique slug index into "slug already in use", so callers never inspect driver errors.
- **One transaction can span capabilities.** `pkg/database` opens the connection and carries a transaction in the request context. Each repository uses it when present, so a capability's write, the IAM resource it registers, and its audit record commit together. This only works for repositories sharing one connection pool, so each process opens the database once and gives that handle to every capability.
- **Connection helpers are shared; models are not.** `pkg/database` holds no capability's row types or queries, so it does not conflict with keeping database models out of `pkg/`. Its `databasetest` subpackage gives every capability's repository tests a throwaway database with that capability's migrations applied.

### Tenant scoping is enforced in application code

Every tenant-scoped table carries a tenant identifier ([ADR 0001](0001-deployment-topology.md)). The repository layer requires a tenant in every query; PostgreSQL row-level security is not used. Composite foreign keys that include the tenant identifier make cross-tenant references impossible at the database level, but a missing tenant filter in a query is caught only by code review and tests, so repository tests must cover tenant isolation.

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
- **sqlc or hand-written `pgx` queries.** Considered. sqlc checks queries against the schema when code is generated, and hand-written queries need no extra layer. GORM was chosen for less boilerplate across many capabilities' repositories; repository tests against PostgreSQL catch the query errors sqlc would have caught earlier.
- **GORM's `AutoMigrate`.** Rejected: it would make Go structs a second source of truth for the schema, and it cannot express the partial and composite indexes the migrations rely on.
