# The Kubernetes agent

Each Kubernetes cluster Ivaldi deploys to runs one agent, `ivaldi-k8s-agent`, installed with the `ivaldi-agent` Helm chart. The web app's cluster onboarding guide writes the chart's values and install commands for you; this page covers where the chart and image come from and what a self-hosted installation can change.

## Where they are published

The **Release** workflow publishes both from one tag, `ivaldi-k8s-agent/vX.Y.Z` (see [Releasing Ivaldi](../releases/README.md)):

| What | Where |
| --- | --- |
| Agent image | `ghcr.io/ivaldibuild/ivaldi-k8s-agent:X.Y.Z` |
| Agent chart | `oci://ghcr.io/ivaldibuild/charts/ivaldi-agent --version X.Y.Z` |

The chart's `version` and `appVersion` are the image's tag, and an image tag is the version with any `+` (SemVer build metadata) written as `_`, since an image tag cannot contain `+`. Commits on `main` also publish `:sha-<full>` images, which are what a release promotes; they are not installation targets.

The agent is released on its own schedule, so its version is not the control plane's. The control plane's onboarding guide offers the chart version pinned in `deploy/helm/ivaldi-agent/version.txt`, the agent release the control plane was tested against, unless `runners.agent_chart_version` says otherwise. A chart version is never overwritten, and the chart is published only after its image.

## Mirrors and air gaps

A cluster that cannot pull from ghcr.io needs both copied to a registry it can reach:

```sh
helm pull oci://ghcr.io/ivaldibuild/charts/ivaldi-agent --version <version>
helm push ivaldi-agent-<version>.tgz oci://registry.example.com/charts
crane copy ghcr.io/ivaldibuild/ivaldi-k8s-agent:<version> registry.example.com/ivaldi-k8s-agent:<version>
```

Then point the control plane at the chart, so the guide's commands use it:

```yaml
runners:
  agent_chart: oci://registry.example.com/charts/ivaldi-agent
```

and install with `--set image.repository=registry.example.com/ivaldi-k8s-agent`.

The agent pulls charts over HTTPS. For local development against a registry without TLS, such as `registry:2` beside a kind cluster, name its host in `registry.plainHTTPHosts` (for example `--set 'registry.plainHTTPHosts={registry:5000}'`). Nothing else uses plain HTTP, and the nodes pull images through their own container runtime's registry configuration.

## For a fork: package settings

A new ghcr.io package is private and unlinked. After the first publish, once, by hand, in the organization's package settings:

- make `ivaldi-k8s-agent` and `charts/ivaldi-agent` public, so clusters can pull them without credentials;
- link `charts/ivaldi-agent` to the repository. The image links itself through its `org.opencontainers.image.source` label.
