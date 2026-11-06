# Creating Releases from CI

CI records each Release of an Application with `ivaldictl release create`, carrying the Builds it just produced. On a version tag, it can publish the Release in the same call.

## Authentication

`ivaldictl` authenticates in one of two ways.

- **People sign in through their browser.** `ivaldictl auth login` opens the control plane's sign-in page and listens on a loopback port for the redirect. That is the authorization code flow with PKCE, as the `ivaldictl` OAuth client every control plane registers. The session is kept in `credentials.json` in your configuration directory, readable only by you, and refreshed when it expires. Use `--org` to pick an Organization, and `--no-browser` to print the address instead of opening it. `ivaldictl auth logout` revokes the session.
- **CI uses a service account's API token.** Pass it with `--api-token`, or set `IVALDI_API_TOKEN`. In CI (`CI` set, or GitHub Actions or GitLab CI detected), `ivaldictl` never opens a browser. Without a token, it fails at once.

`ivaldictl auth status` shows who the CLI acts as. The control plane's address comes from `--server-url`, then `IVALDI_SERVER_URL`, then `http://localhost:8660`.

## Setting up a service account

An owner or admin creates the service account, gives it a role, and issues it a token:

```sh
# Create the service account
POST /v1/service-accounts            {"display_name": "GitHub Actions"}

# Give it a role. Creating and publishing Releases needs release.manage and
# release.publish, which admins hold; admin can be granted on a Workspace.
POST /v1/grants                      {"principal_id": "<service account ID>", "role": "admin", "resource_id": "<workspace ID>"}

# Issue a token. Its secret is shown once.
POST /v1/service-accounts/<id>/tokens {"name": "github-actions", "expires_at": "2027-06-01T00:00:00Z"}
```

Store the returned `secret` (it starts with `ivt_`) as a CI secret. Ivaldi keeps only its SHA-256 hash and the first 12 characters, to tell tokens apart.

- **Revoking a token:** `POST /v1/api-tokens/<id>/revoke` ends it for good.
- **Suspending the account:** `POST /v1/service-accounts/<id>/suspend` stops all of its tokens until it is reactivated.

A token acts with its service account's grants. The authorizer checks it like any other caller.

## The command

```sh
ivaldictl release create --app <application ID> --artifact api=sha256:<digest> [--artifact chart=sha256:<digest>] [--publish]
```

- **Builds** are named by digest: `--artifact <artifact name>=<digest>`, once per artifact. Tags cannot be resolved to digests yet.
- **What CI knows.** In GitHub Actions and GitLab CI, `ivaldictl` reads the commit, the full Git ref, and the CI run's URL from the environment:

  | Pipeline | Ref |
  |---|---|
  | GitHub Actions | `GITHUB_REF`, such as `refs/tags/v1.4.0` or `refs/pull/42/merge` |
  | GitLab CI, tag | `refs/tags/<tag>` |
  | GitLab CI, merge request | `refs/merge-requests/<iid>/head` |
  | GitLab CI, branch | `refs/heads/<branch>` |

  `--commit`, `--ref`, `--ci-provider`, and `--ci-run` override them.
- **The version.** On a version tag such as `v1.4.0`, it defaults to `1.4.0`. Elsewhere, give `--version`.
- **The Channel.** Without `--channel`, the Release joins the one Channel whose rules it meets, such as a Channel for versions without a pre-release tag, or one for `refs/pull/**`. If no Channel's rules match, or more than one does, the command fails and says why.

## GitHub Actions

```yaml
on:
  push:
    tags: ["v*"]

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - id: build
        uses: docker/build-push-action@v6
        with:
          push: true
          tags: ghcr.io/acme/api:${{ github.ref_name }}
      - run: ivaldictl release create --app "$APP" --artifact "api=${{ steps.build.outputs.digest }}" --publish
        env:
          IVALDI_SERVER_URL: https://ivaldi.example.com
          IVALDI_API_TOKEN: ${{ secrets.IVALDI_API_TOKEN }}
          APP: app_01H...
```

On a pull request, give a version and leave out `--publish`, so the Release stays a draft:

```sh
ivaldictl release create --app "$APP" --version "1.5.0-pr.${PR_NUMBER}.${GITHUB_RUN_NUMBER}" --artifact "api=$DIGEST"
```

## GitLab CI

```yaml
release:
  rules:
    - if: $CI_COMMIT_TAG =~ /^v/
  script:
    - ivaldictl release create --app "$APP" --artifact "api=$IMAGE_DIGEST" --publish
  variables:
    IVALDI_SERVER_URL: https://ivaldi.example.com
```

Set `IVALDI_API_TOKEN` as a masked CI/CD variable.
