# Ivaldi

> In Norse legend, Ivaldi is a powerful dwarf or giant alchemist. He is most famous for his offspring, known collectively as the Sons of Ivaldi (Ívaldasynir). They were master blacksmiths who lived in Svartalfheim and forged some of the most legendary treasures for the gods.

Ivaldi is a collaborative workspace where agents and humans craft, test, release, and monitor software together.

It combines two things that are usually separate products:

- **An agentic software craft shop.** Ivaldi coordinates execution engines such as Claude Code, Codex, and OpenCode. It owns the context, orchestration, policy, credentials, and provenance around them. The goal is shared understanding and controlled delivery, not maximum code generation.
- **A continuous delivery platform.** Ivaldi tracks every change from commit through build, deployment, and release, and keeps watching it once it is running. Kubernetes is the first runtime; others will follow.

Ivaldi is not another coding agent. It is the workspace those agents work in, and it makes their work observable, reviewable, and traceable.

> **Status:** early development. The architecture is documented and the identity data model is in progress. Most components are still scaffolding, and nothing here is ready to run in production.

## Lifecycle

All work in Ivaldi moves through one lifecycle:

```
Intent → Context → Plan → Craft → Verify → Review → Ship → Observe → Learn
```

Each step leaves evidence: tests, builds, checks, repository state, and policy decisions. Ivaldi relies on that evidence rather than on an agent saying the work is done. A Work Item can be traced through its Sessions, Changes, commits, pull requests, CI runs, builds, Deployments, and Releases.

Humans stay in the loop. They can review, approve, steer, and step in at any stage.

## Capabilities

- **Code intelligence:** a graph of indexed repositories, served to agents over MCP. Every result names the repository and commit it came from.
- **Intake:** turns requests from people and integrations into Work Items.
- **Agent orchestration:** runs agent Sessions in sandboxes. Orchestration state lives in Ivaldi, not in an agent's conversation.
- **Policy and approvals:** least-privilege control over the tools, credentials, network destinations, and actions available to each Session.
- **VCS and CI/CD integration:** connects to source hosts and pipelines through Ivaldi-owned interfaces.
- **Deployment and release tracking:** links each Deployment to the builds and commits it contains, starting with Kubernetes.
- **Observability:** shows what happened in deployments, infrastructure, and agent Sessions.

## Architecture

Ivaldi is split into a **control plane**, which holds orchestration state, identity, policy, and provenance, and a **data plane**, which runs wherever the code and compute live. Data-plane components always connect out to the control plane, so customers never have to open inbound access to their networks.

Because of this split, one codebase supports three deployment modes:

| Mode | Control plane | Data plane |
|---|---|---|
| Self-hosted | You | You |
| Managed cloud | Ivaldi | Ivaldi |
| Managed cloud + bring your own compute | Ivaldi | You |

| Component | Plane | Role |
|---|---|---|
| [`apps/control-plane`](apps/control-plane) | Control | Work Items, Sessions, identity and access, policy, credential references, provenance |
| [`apps/gateway`](apps/gateway) | Either | Receives provider webhooks and inbound MCP tool calls |
| [`apps/worker`](apps/worker) | Data | Runs agent Sessions and other execution |
| [`apps/indexer`](apps/indexer) | Data | Builds code-intelligence indexes from source |
| [`apps/helm-agent`](apps/helm-agent) | Data | Carries out and reports Kubernetes deployments |
| [`apps/cli`](apps/cli) | — | `ivaldi` command-line client |
| [`pkg/`](pkg) | Shared | Libraries used by more than one component |

For details, see [`docs/vision.md`](docs/vision.md) and the [architecture decision records](docs/adr/README.md).

## Development

Ivaldi is a multi-module Go repository. The toolchain version is pinned in `mise.toml`.

```sh
mise install

# Test every module in the Go workspace
go test ./pkg/... ./apps/cli/... ./apps/control-plane/... ./apps/gateway/... ./apps/indexer/... ./apps/worker/... ./apps/helm-agent/...

# Run the control plane or the CLI from source
go run ./apps/control-plane/cmd
go run ./apps/cli/cmd
```

Before opening a pull request, run `gofmt` and `go vet`. Contribution conventions, including domain vocabulary, code style, and commit format, are in [`AGENTS.md`](AGENTS.md).
