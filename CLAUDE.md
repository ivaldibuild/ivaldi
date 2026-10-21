# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Product direction, architecture principles, domain vocabulary, style, and commit conventions live in `AGENTS.md` and apply here in full:

@AGENTS.md

Architecture decisions are recorded in `docs/adr/`. Read them before changing deployment topology, the gateway, identity and access, policy, credential handling, or where data models live.

## Current state of the repository

The repo is an early scaffold. Most of the layout is reserved but empty, so don't assume code exists because a directory does:

- Three binaries ([ADR 0010](docs/adr/0010-one-binary-for-the-components.md)): `apps/control-plane` builds `ivaldi`, whose subcommands `serve`, `gateway`, `worker`, and `indexer` start each component (none does anything yet); `apps/cli` builds `ivaldictl`; `apps/runtime-agent` has only a `go.mod`. In `ivaldi`, a component's command is `cmd/<component>` and its code `internal/<component>`. `internal/boundaries` tests the import rules: a component imports only its own packages and `pkg/`, and the control plane (`cmd/server` and every other `internal` package) imports no component. Add a new component to `boundaries.Components`. The root `--config` flag has no default, so the binary never needs a home directory.
- Implemented shared code in `pkg/` is `env`, `logging`, `homedir`, `database`, `transaction`, `pagination`, `id`, `identity`, `authorization`, `clock`, `slug`, and `release` (so far only versions, version ranges, and Channel rule configs). `pkg/events`, `graph`, `orchestration`, `policy`, and `vcs` are empty placeholders for the capability boundaries in `docs/vision.md`.
- The control plane has migrations for four capabilities: `iam`, `workspace`, `versioncontrol`, and `delivery` (each in `apps/control-plane/internal/<capability>/migrations`, with designs in `docs/design/`). Nothing applies them at startup yet.
- `workspace` and `delivery` have Go code; `delivery` covers only its configuration side so far (Environments, Targets, Lifecycles, Registries, Applications with artifacts, Channels, and placements), not Builds, Releases, or Deployments. Both follow ADR 0005, and `delivery` refines the conventions below in ways described after them: domain types, the service, and the interfaces it needs (`interface.go`) in `internal/workspace`; the GORM repository in `internal/workspace/postgres`. A capability's `interface.go` holds only interfaces specific to it (its `Repository`, and hooks such as workspace's `Dependent` and `LockSource`). Contracts shared by several capabilities live with their owner, with no implementations yet: `identity.Actor` (`pkg/identity`) and the single authorizer (`pkg/authorization`: `Request`, `Decision`, `Require`, and its errors) in `pkg/` because the gateway and workers use them too; `iam.ResourceRegistrar` and the IAM resource types, `audit.Recorder`, and `team.Directory` in `internal/iam`, `internal/audit`, and `internal/team` because only control-plane capabilities use them. Each capability keeps its own defaults, limits, permission names, authorizer action names (`Action…`), and audit event types in its `constants.go`; enum types such as delivery's `Tier` live next to the domain type that uses them. Nothing in `cmd/` wires any of it up yet. Domain objects (`Workspace`, `Project`, `Setting`) are the only surface that mutates domain state: their fields are unexported, they are created with `New…` constructors that apply creation rules and assign IDs, and they change only through their methods (`Update`, `Archive`, `MakeDefault`, `TransferTo`, `Change`, …). No method takes a time: domain objects and audit records read the package clock (`now()` in `clock.go`), which returns UTC truncated to microseconds so objects compare equal after a database round trip. Tests fix it with `fixClock(t, …)` and so must not run in parallel. The service orchestrates (authorize, load, call methods, persist, audit) and never sets state itself. Repositories only persist and retrieve: they read state with `Data()` and rebuild stored objects with `Reconstitute…`, which skips creation rules and is for repositories only. Audit records carry `Data()` snapshots. Value objects such as `Scope` and `Resolved` keep exported fields. IDs are ULIDs from `pkg/id`, assigned by the service with `id.Assign(&w.ID, w)` when it creates an object. A domain type that declares `Prefix()` (such as `Workspace` → `wsp`) gets prefixed IDs and registers the prefix with `id.MustRegister` in its package's `init` (a prefix claimed twice panics at startup); a type without `Prefix()` gets a plain ULID. GORM entities have no ID hooks. Database access goes through `pkg/database` (see Shared packages).
- `delivery` differs from `workspace` in these conventions, and new capabilities should follow `delivery`:
  - **State and rebuilding.** Domain types expose `Snapshot()` (not `Data()`). There is no `Reconstitute…`: repositories rebuild stored objects with the same `NewX(props, &id)` constructor, passing the stored ID and timestamps in `NewXProps` (a new object leaves them nil). Creation rules therefore also run on load, so a rule added later must still accept existing rows. Child records owned by an aggregate (Lifecycle phases, Channel rules) are passed in the props and change only through the parent's methods; readers get them as snapshots.
  - **Repositories save whole objects.** `UpdateX(ctx, x)` writes what `x.Snapshot()` holds; archive and restore are domain methods the service calls before saving. Every read takes the Organization ID; slug lookups also take their scope and match only active rows. Mappers (`*_mapper.go`, one struct per aggregate) convert between entities and domain types.
  - **Shared helpers.** The clock is `pkg/clock` (`clock.Now()`, `clock.Fix(t, …)` in tests, `clock.CopyTime`), and slugs come from `pkg/slug` (`Create`, `Validate`). Validation errors are `NewErrInvalidInput(msg)`, which matches the `ErrInvalid` sentinel.
  - **Lists page by cursor.** List queries have a `Page pagination.Query` field, and repositories return `*pagination.Result[*T]` (`pkg/pagination`), implemented with `database.Paginate` (see Shared packages). A capability's `postgres` package declares a `database.Keyset` per list and translates `pagination.ErrInvalidQuery` into its own invalid-input error (`delivery/postgres/keysets.go`).
  - **Services.** Each capability declares service interfaces (`EnvironmentService`, …) in `interfaces.go`, implemented by one `Service`. The generic `run(change[T])` helper in `delivery/service.go` loads, authorizes outside the transaction, reloads (optionally locked), applies domain methods, saves, and audits with before/after snapshots and the decision ID.
- `Makefile` wraps golang-migrate's `migrate` CLI (`make help`): `<capability>_migration_generate name=<name>` creates a numbered pair, and `migrate_up`, `migrate_down capability=<c>`, and `migrate_version` take `DATABASE_URL`. Every capability numbers from 000001, so each must use its own version table (`<capability>_schema_migrations`, with `version_control` for `versioncontrol`); the Makefile sets it, so don't call `migrate` against the database without `x-migrations-table`. `deploy/`, `migrations/`, `schemas/`, `scripts/`, and `.github/actions/` have no tracked files yet.

## Commands

Toolchain is pinned by `mise.toml` (`mise install`). Beyond the commands in `AGENTS.md`:

```sh
# Single test (pkg is its own module, so run from the workspace root by package path)
go test ./pkg/env -run TestParseTag_Required -v

# Single CLI test
go test ./apps/cli/... -run TestName

# Tests against PostgreSQL (pkg/database and capability repositories); they skip
# when the variable is unset. The user must be able to CREATE DATABASE: each test
# makes and drops its own (pkg/database/databasetest).
IVALDI_TEST_DATABASE_URL='postgres://user:pass@localhost:5432/postgres?sslmode=disable' \
  go test ./pkg/database/... ./apps/control-plane/internal/workspace/postgres/ ./apps/control-plane/internal/delivery/postgres/

gofmt -l .        # list unformatted files
```

There is no lint config or lint target yet; `go vet` and `gofmt` are the checks.

## Module layout gotchas

- Every app and `pkg/` is a separate Go module. Root `go.work` stitches together `pkg` and every app, including `apps/cli`, so apps import shared code as `github.com/ivaldibuild/ivaldi/pkg/...` without `replace` directives. Builds that run with `GOWORK=off` (for example, a per-module Docker build) lose that and would need a `replace` or a published `pkg` version.
- Module paths: `pkg` is `github.com/ivaldibuild/ivaldi/pkg`, while each app (including the CLI) is its own `github.com/ivaldibuild/ivaldi-<app>` module. `apps/control-plane`'s module, `ivaldi-control-plane`, also holds the gateway, worker, and indexer.
- `VERSION` in each binary's `cmd` package is a `var` so `-ldflags "-X main.VERSION=…"` can set it; the Makefile does.
- `pkg/env` and `pkg/logging` were ported from another project; leftover fixture names (e.g. `dispatch` in `env_test.go`) are test data only.

## Shared packages

- `pkg/env` — struct-tag config loader. Layering is `envDefault` < YAML < environment variables; errors are aggregated (`*env.AggregateError`) rather than returned on first failure. Use `Options.Source` to inject env lookups in tests instead of `os.Setenv`. `LoadDotEnv` loads `.env` files.
- `pkg/logging` — backend-agnostic `Logger` interface with a zap implementation, lumberjack file rotation, and a testify `MockLogger`. Depend on the `Logger` interface, not zap.
- `pkg/database` — opens PostgreSQL with GORM and carries one transaction in the context: `Transactor.WithinTx` starts it (or joins one already in the context), and repositories call `database.Conn(ctx, db)` so their writes join it. A transaction only spans repositories sharing one `*gorm.DB`, so open it once per process. `UniqueViolation`/`ForeignKeyViolation`/`CheckViolation` return the constraint name for translating into domain errors. `databasetest.New(t, migrations.FS)` gives tests a throwaway database. Never use GORM `AutoMigrate`; migrations own the schema.
- `pkg/pagination` — `Query` and `Result[T]` for paged lists, with no database code so domain packages and service interfaces can use them (like `pkg/transaction`). `pkg/database`'s `Paginate` implements keyset paging with them: a capability gives it a fresh base query per call and a `Keyset` (the allowlist of sort columns, a default, and the tie-breaking ID column), and gets back a `Page` of rows plus opaque cursors; `ToResult` maps the rows to domain objects. Invalid sorts and cursors fail with `pagination.ErrInvalidQuery` before any query runs.
- `pkg/transaction` — the `Transactor` interface (`WithinTx`) that capabilities depend on to make writes commit together, with no database code, so domain packages can import it. `database.Transactor` implements it. Capabilities use this one rather than declaring their own. The guarantee only holds if every write inside `WithinTx`, including other capabilities' implementations called there, goes through the context it passes.
- `pkg/id` — ULID primary keys, assigned by domain code (`id.Assign`), prefixed for types that declare `Prefix()`. Prefix registration (`id.MustRegister`) catches duplicates only within one program, not across deployables.
- `go mod tidy` fails in an app module that imports `pkg/` (it ignores `go.work` and tries to download the unpublished module), so adjust that app's `go.mod` by hand.
