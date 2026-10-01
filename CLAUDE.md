# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Product direction, architecture principles, domain vocabulary, style, and commit conventions live in `AGENTS.md` and apply here in full:

@AGENTS.md

## Current state of the repository

The repo is an early scaffold. Most of the layout is reserved but empty, so don't assume code exists because a directory does:

- Only `apps/control-plane` (Cobra root command `serve`, no subcommands) and `apps/cli` (Cobra root `ivaldi`) have code. `gateway`, `indexer`, `worker`, and `helm-agent` contain only `go.mod` plus empty `cmd/` and `internal/` dirs.
- Implemented shared code in `pkg/` is `env`, `logging`, and `homedir`. `pkg/events`, `graph`, `orchestration`, `policy`, `release`, and `vcs` are empty placeholders for the capability boundaries in `docs/vision.md`.
- `Makefile` and `README.md` are empty; `deploy/`, `migrations/`, `schemas/`, `scripts/`, and `.github/actions/` have no tracked files yet.

## Commands

Toolchain is pinned by `mise.toml` (`mise install`). Beyond the commands in `AGENTS.md`:

```sh
# Single test (pkg is its own module, so run from the workspace root by package path)
go test ./pkg/env -run TestParseTag_Required -v

# Single CLI test (CLI is outside go.work)
(cd apps/cli && GOWORK=off go test ./... -run TestName)

gofmt -l .        # list unformatted files
```

There is no lint config or Makefile target yet; `go vet` and `gofmt` are the checks.

## Module layout gotchas

- Every app and `pkg/` is a separate Go module. Root `go.work` stitches together `pkg` and every app except `apps/cli`, so apps import shared code as `github.com/ivaldibuild/ivaldi/pkg/...` without `replace` directives. `apps/cli` would need it added to `go.work` (or a `replace`) to use `pkg/`.
- Module paths: `pkg` is `github.com/ivaldibuild/ivaldi/pkg`, while each app (including the CLI) is its own `github.com/ivaldibuild/ivaldi-<app>` module.
- `pkg/env` and `pkg/logging` were ported from another project; leftover fixture names (e.g. `dispatch` in `env_test.go`) are test data only.

## Shared packages

- `pkg/env` — struct-tag config loader. Layering is `envDefault` < YAML < environment variables; errors are aggregated (`*env.AggregateError`) rather than returned on first failure. Use `Options.Source` to inject env lookups in tests instead of `os.Setenv`. `LoadDotEnv` loads `.env` files.
- `pkg/logging` — backend-agnostic `Logger` interface with a zap implementation, lumberjack file rotation, and a testify `MockLogger`. Depend on the `Logger` interface, not zap.
