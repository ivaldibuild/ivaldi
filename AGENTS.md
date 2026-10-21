# Repository Guidelines

## Product Direction

Ivaldi is an **agentic software craft shop**, not a software factory or a coding agent. It coordinates execution engines such as Codex, Claude Code, and OpenCode while owning context, orchestration, policy, infrastructure, state, observability, integrations, and provenance. Optimize for collaborative understanding and controlled delivery, not maximum code generation. Agents should make software development more observable, not less.

Preserve the lifecycle `Intent → Context → Plan → Craft → Verify → Review → Ship → Observe → Learn`. Important work should remain traceable from Work Item through Session, Change, commit, pull request, CI, build, deployment, and release. Prefer deterministic evidence—tests, builds, checks, repository state, and policy decisions—over an agent's assertion that work is complete.

## Project Structure & Module Organization

Ivaldi is a multi-module Go repository. Deployable components live under `apps/`: `control-plane`, `gateway`, `indexer`, `worker`, `helm-agent`, and `cli`. Shared libraries live in `pkg/`, currently including configuration (`env`), home-directory helpers, and structured logging. Keep package tests beside their implementation as `*_test.go`. Architecture and product notes belong in `docs/`.

The root `go.work` includes the shared package module and every app module, so run Go commands from the repository root.

## Architecture Principles

- Define capability boundaries before service boundaries; a domain concept does not automatically need a microservice.
- Keep Ivaldi's domain independent of vendors. Sandbox, VCS, CI, agent-harness, queue, and code-intelligence integrations should sit behind Ivaldi-owned interfaces when real provider variation exists.
- Treat graph engines such as Gortex and storage systems such as Turbopuffer as replaceable implementation choices, not product boundaries or required developer tooling.
- Keep orchestration state in Ivaldi rather than relying on an agent conversation as the source of truth.
- Apply least privilege by session, repository, environment, tool, credential, and network destination. Policy and approval decisions must not be buried in provider code.
- Preserve provenance: indexed knowledge identifies its repository and commit/generation; changes link to Sessions and Work Items; deployments link to builds and commits.
- Keep humans in the workflow through review, approval, steering, and intervention.

Use established terms consistently: Workspace, Project, Source, Work Item, Session, Agent, Policy, Change, Build, Deployment, and Release. PostgreSQL, Redis Streams, AWS/EKS, Next.js, and individual sandbox or intelligence providers are current directions or replaceable choices—not reasons to couple domain logic to infrastructure.

## Build, Test, and Development Commands

- `mise install` installs the repository's pinned tools: the Go toolchain (Go 1.27.1) and the golang-migrate CLI (`migrate` 4.20.1) used by the Makefile's migration targets.
- `go test ./pkg/... ./apps/cli/... ./apps/control-plane/... ./apps/gateway/... ./apps/indexer/... ./apps/worker/... ./apps/helm-agent/...` tests every module in the root workspace. List each module; `./apps/...` fails in workspace mode because `apps/` is not itself a module.
- `go run ./apps/control-plane/cmd` starts the control-plane command locally.
- `go run ./apps/cli/cmd` runs the CLI from source.
- `go vet <package-pattern>` performs static checks before review.

## Coding Style & Naming Conventions

Use standard Go formatting: tabs for indentation, `gofmt` on every changed `.go` file, and idiomatic package names that are short, lowercase, and singular. Exported identifiers use `PascalCase`; unexported identifiers use `camelCase`. Keep Cobra command setup in `cmd/`, reusable business logic out of `main` packages, and provider details away from domain logic. Return errors with useful context. Avoid catch-all packages such as `utils`, `helpers`, or `common`; app-specific code belongs in `apps/<app>/internal`.

## Testing Guidelines

Use Go's `testing` package; `testify` is available in `pkg` for assertions and mocks. Name tests `TestThing` in `thing_test.go`, prefer table-driven cases for parsers and configuration behavior, and use `t.Run` for readable scenarios. Add regression coverage with behavioral changes. No coverage threshold is enforced, but new code should exercise success and error paths.

## Commit & Pull Request Guidelines

Prefer concise Conventional Commit subjects, matching recent history: `feat(env): add validation errors` or `fix(cli): handle missing config`. Keep commits focused. Pull requests should explain the motivation and behavior, list verification commands, link relevant work, and call out configuration, security, compatibility, or architectural changes. Record consequential architecture decisions as ADRs. Include screenshots only for user-visible output changes.

## Security & Configuration

Do not commit credentials, tokens, local environment files, or generated logs. Document new configuration keys and provide safe defaults where possible; tests should inject environment lookups instead of mutating developer machines.
