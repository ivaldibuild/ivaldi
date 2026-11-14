# The Kubernetes agent

Each Kubernetes cluster Ivaldi deploys to runs one agent, `ivaldi-k8s-agent`, installed with the `ivaldi-agent` Helm chart. The web app's cluster onboarding guide writes the chart's values and install commands for you; this page covers where the chart and image come from and what a self-hosted installation can change.

## Where they are published

CI publishes both on every push to `main` and on every `v*` tag, with one version:

| What | Where |
| --- | --- |
| Agent image | `ghcr.io/ivaldibuild/ivaldi-k8s-agent:<version>` |
| Agent chart | `oci://ghcr.io/ivaldibuild/charts/ivaldi-agent --version <version>` |

The image tag is the version with any `+` (SemVer build metadata) written as `_`, since an image tag cannot contain `+`; the chart renders the tag the same way. `<version>` is what `scripts/version.sh` prints for the commit: `0.4.0` on a tag, `0.4.0-3-gabc1234` after one, and `0.0.0-gabc1234` while the repository has no tags. The control plane carries the same version, and the guide installs the chart version that matches it, unless `runners.agent_chart_version` says otherwise. A chart version is never overwritten, and the chart is published only after its image.

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
