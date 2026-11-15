# Releasing Ivaldi

Each component is released on its own schedule, from its own tag, by one workflow: **Release** (`.github/workflows/release.yaml`). A CLI fix never republishes the control plane, and an agent chart is never published for a commit that changed nothing it serves.

| Tag | Ships | Paths it covers |
| --- | --- | --- |
| `ivaldi/vX.Y.Z` | `ghcr.io/ivaldibuild/ivaldi:X.Y.Z` — the API **and** the web app, one image (`serve`, `gateway`, `worker`, `indexer`) | `apps/control-plane`, `apps/webui`, `pkg`, `api/openapi.json` |
| `ivaldi-k8s-agent/vX.Y.Z` | `ghcr.io/ivaldibuild/ivaldi-k8s-agent:X.Y.Z` and `oci://ghcr.io/ivaldibuild/charts/ivaldi-agent:X.Y.Z` | `apps/k8s-agent`, `deploy/helm/ivaldi-agent`, `pkg` |
| `ivaldictl/vX.Y.Z` | `ivaldictl` archives for linux, darwin, and windows on amd64 and arm64, with `checksums.txt` | `apps/cli`, `pkg`, `api/openapi.json` |

The web app has no tag of its own: it ships inside the control plane's image ([ADR 0016](../adr/0016-control-plane-serves-the-web-app.md)). The agent chart has no tag of its own either: its version and `appVersion` are the agent image's.

## Versions

`scripts/version.sh <component>` prints a component's version from its tags: `1.4.0` on the tag, `1.4.0-3-gabc1234` three commits after it, `0.0.0-gabc1234` while the component has no tag yet, and a `-dirty` suffix for a build with uncommitted changes. `make build` stamps each binary with its own; CI stamps each image with its own and passes the commit and the commit's time beside it, so a rebuild of one commit is identical.

**A released image reports the version it was built with, not the release's.** CI builds `:sha-<full>` on `main` before the release tag exists, and a release promotes that digest unchanged (see [Publishing a release](#publishing-a-release)). So the image tagged `1.4.0` says something like `1.3.2-7-gabc1234`, or `0.0.0-gabc1234` before the component's first tag, in `ivaldi --version`, the API description's version, the `org.opencontainers.image.version` label, and the `agent_version` an agent reports. That string still names the exact commit. The release's identity is its tag, its image tag, and `release.json`, which maps the version to the commit and digest. This is the price of shipping the artifact that passed CI rather than a second build of it. The CLI is the exception: it is built at the tag, so `ivaldictl version` prints the release version.

Components are released with [SemVer](https://semver.org/). Before 1.0, a minor version may break compatibility; after 1.0, only a major version may.

## Publishing a release

Run **Release** from `main` with the component, `tag-and-publish`, and either an explicit version (`1.4.0`, `1.4.0-rc.1`) or nothing, which takes the next patch after the component's last tag.

1. **Prepare** refuses a release that is not what it claims: the tag must not exist, the commit must be on `main`, and the `CI` check on exactly that commit must have passed. A release is never built from an untested commit.
2. **Tag and draft** creates the tag, pushes it, and opens a **draft** GitHub Release with notes generated from the conventional commits that touched the component's paths since its previous tag. Read and edit them before the release is published; nothing is announced yet.
3. **Publish** promotes rather than rebuilds. Images are copied from `:sha-<full>`, the digest CI built and tested on `main`, to the release's tags, so the release is the artifact that passed, not a second build of the same commit. The chart is packaged with the release's version and pushed. The CLI is cross-compiled at the tagged commit and its archives are attached to the Release.
4. **Finish** attaches `release.json` — the version, the tag, the commit, the CI run, the image digest, and the chart reference — and publishes the draft.

`publish-existing-tag` repeats steps 2 to 4 for a tag that exists, which is how a release whose publishing step failed is completed without moving the tag.

### What a release tag promises

- **The tag never moves.** Re-releasing a version is not possible; publish a new one.
- **An image tag is never overwritten.** The workflow refuses to push a version that already exists.
- **A chart version is never overwritten**, like every other OCI artifact.
- **`X.Y` and `latest`** follow stable releases only, and only move forward: `X.Y` names the newest `X.Y.Z`, and `latest` the newest stable release, so a patch to an older line moves neither. A pre-release gets its own version tag and nothing else.
- **The commit is on `main` and passed CI there.** Both are checked, not assumed.

## Rollback

Images and archives are immutable, so rolling back is redeploying the previous version's tag or digest. Nothing needs to be rebuilt or retagged.

For the control plane, a rollback must also satisfy the database. Migrations follow expand/contract: a migration that a release applies must be reversible by the *previous* release's binary, which means adding columns and tables, and dropping them only after the release that stopped using them. The `migrations` job in CI applies every migration, rolls every capability back, and applies it again on an empty database, so a migration without a working down would fail before release rather than during a rollback.

## Upgrading a deployment

A new control plane does not migrate on start. Run the migrations the image carries, then start it:

```sh
docker run --rm -e IVALDI_DATABASE_URL=… ghcr.io/ivaldibuild/ivaldi:1.4.0 migrate up
docker run --rm -e IVALDI_DATABASE_URL=… ghcr.io/ivaldibuild/ivaldi:1.4.0 migrate version
```

`migrate down --capability <c> --steps <n>` rolls one capability back, and requires naming it: rolling every capability back at once is never what an upgrade wants.

## Compatibility

| Between | Contract |
| --- | --- |
| control plane ↔ agent | The control plane's onboarding guide offers the agent chart version pinned in `deploy/helm/ivaldi-agent/version.txt`, which records the agent release the control plane was tested against. `runners.agent_chart_version` overrides it. |
| control plane ↔ CLI, control plane ↔ web app | The API is `/v1`; clients are generated from `api/openapi.json` and released independently. A breaking change within `/v1` is not permitted; it moves to `/v2`. |
| agent ⟷ its chart | One version, always. |

A release that changes `api/openapi.json` in a way clients must follow should say so in its notes, and the CLI and web app releases that follow it are ordinary releases of their own.

## For a fork

The workflows publish to `ghcr.io/${{ github.repository_owner }}`, so a fork publishes to its own namespace. Two settings are manual, once, in the fork's package settings: make the images and the chart public, and link them to the repository. The agent image links itself through its `org.opencontainers.image.source` label.
