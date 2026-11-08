# ADR 0010: One binary runs the control plane, gateway, worker, and indexer

- Status: Proposed; the runtime agent is now one agent per runtime, starting with `apps/k8s-agent` ([ADR 0013](0013-one-agent-per-runtime.md))
- Date: 2026-10-21

## Context

[ADR 0001](0001-deployment-topology.md) splits Ivaldi into a control plane and a data plane. Until now, each component was its own Go module and its own binary: `apps/control-plane`, `apps/gateway`, `apps/worker`, `apps/indexer`, and `apps/helm-agent`, now `apps/runtime-agent` ([ADR 0006](0006-release-and-delivery-tracking.md)). Separate modules also enforced the planes' boundary at compile time. A worker could not import the control plane's repositories, because Go does not allow one module to import another's `internal` packages.

Shipping five binaries means five images to build, version, scan, and document, while most installations, self-hosted ones especially, run all of them together. Operators would rather pull one image and choose a role.

## Decision

**One binary, `ivaldi`, runs the control plane, the gateway, the worker, and the indexer. Each is a subcommand**: `ivaldi serve`, `ivaldi gateway`, `ivaldi worker`, and `ivaldi indexer`. A deployment runs the same image with a different command. Its code lives in the `apps/control-plane` module: each component's command in `cmd/<component>`, and its code in `internal/<component>`.

**The CLI stays a separate binary, `ivaldictl`** (`apps/cli`), because it runs on people's machines and needs none of the server code.

**The runtime agent stays a separate binary** (`apps/runtime-agent`). It runs inside customers' Kubernetes clusters, with an in-cluster identity that can read, and when execution is enabled change, their workloads ([ADR 0006](0006-release-and-delivery-tracking.md)). A small binary without control-plane code keeps what runs there minimal, and lets the agent be upgraded on its own schedule.

**The planes' boundary is now enforced by a test, not by the compiler.** `apps/control-plane/internal/boundaries` checks every import in the module:

- a component imports nothing from the module except its own `cmd/<component>` and `internal/<component>` packages;
- the control plane, which is `cmd/server` and every other `internal` package, imports no component;
- only the root command imports every component, to register its subcommand.

Code that components share with each other or with the control plane goes in `pkg/`, as [ADR 0006](0006-release-and-delivery-tracking.md) already requires. ADR 0001's rule still holds: a data-plane component reaches the control plane through its API only, never its database.

**Each component reads its own configuration.** The control plane's is `internal/config`; a component that needs settings defines them in its own tree, and gets no access to the control plane's database settings, signing keys, or encryption keys.

## Consequences

- One image to build, scan, and release for the server components. `make build` produces `ivaldi` and `ivaldictl`, both stamped with the version from `git describe`.
- A worker, indexer, or gateway running in a customer's data plane carries the control plane's code, even though it never runs it. Its attack surface grows, and so does the image. The runtime agent, which has the widest access inside customer clusters, is kept out for that reason.
- The components are released together. Under bring-your-own-compute, a customer's data plane can still run an older release than the managed control plane, because their contracts in `schemas/` are versioned ([ADR 0001](0001-deployment-topology.md)). What changes is that they can no longer be released separately.
- The boundary holds only while the test runs in CI. A violation is now a failing test, not a build error.
- A component can later move back to its own module, if its image size or release cadence needs to change, by moving its two directories. The import rules keep that move mechanical.

## Alternatives considered

- **A binary for each component.** Rejected: the operational cost described above, for no benefit most installations need.
- **Separate modules with a launcher module that imports each component's command.** Considered: it keeps the compile-time boundary and still ships one binary. Rejected for now because every app module needs hand-maintained `go.mod` files (`go mod tidy` cannot resolve the unpublished `pkg` module in workspace mode), and the boundary test gives the same protection with one module.
- **Also folding in the runtime agent and the CLI.** Rejected for the reasons above: both run outside the operator's servers, on customer clusters and people's machines.
