# Repository Guidelines

## Project Structure & Module Organization

Ivaldi is a multi-module Go repository. Deployable components live under `apps/`: `control-plane`, `gateway`, `indexer`, `worker`, `helm-agent`, and the standalone `cli`. Shared libraries live in `pkg/`, currently including configuration (`env`), home-directory helpers, and structured logging. Keep package tests beside their implementation as `*_test.go`. Architecture and product notes belong in `docs/`.

The root `go.work` includes the shared package module and all app modules except `apps/cli`; run CLI commands with `GOWORK=off` unless you intentionally add it to the workspace.

## Build, Test, and Development Commands

- `mise install` installs the repository's pinned Go toolchain (Go 1.27.1).
- `go test ./pkg/... ./apps/control-plane/... ./apps/gateway/... ./apps/indexer/... ./apps/worker/... ./apps/helm-agent/...` tests every module in the root workspace.
- `(cd apps/cli && GOWORK=off go test ./...)` tests the standalone CLI module.
- `go run ./apps/control-plane/cmd` starts the control-plane command locally.
- `(cd apps/cli && GOWORK=off go run ./cmd)` runs the CLI from source.
- `go vet <package-pattern>` performs static checks before review.

## Coding Style & Naming Conventions

Use standard Go formatting: tabs for indentation, `gofmt` on every changed `.go` file, and idiomatic package names that are short, lowercase, and singular. Exported identifiers use `PascalCase`; unexported identifiers use `camelCase`. Keep Cobra command setup in `cmd/`, and keep reusable business logic out of `main` packages. Return errors with useful context rather than logging deep inside library code.

## Testing Guidelines

Use Go's `testing` package; `testify` is available in `pkg` for assertions and mocks. Name tests `TestThing` in `thing_test.go`, prefer table-driven cases for parsers and configuration behavior, and use `t.Run` for readable scenarios. Add regression coverage with behavioral changes. No coverage threshold is enforced, but new code should exercise success and error paths.

## Commit & Pull Request Guidelines

Prefer concise Conventional Commit subjects, matching recent history: `feat(env): add validation errors` or `fix(cli): handle missing config`. Keep commits focused. Pull requests should explain the motivation and behavior, list verification commands, link relevant issues, and call out configuration or compatibility changes. Include screenshots only for user-visible output changes.

## Security & Configuration

Do not commit credentials, tokens, local environment files, or generated logs. Document new configuration keys and provide safe defaults where possible; tests should inject environment lookups instead of mutating developer machines.
