# Creating Releases from CI

CI records each Release of an Application with `ivaldictl release create`, carrying the Builds it just produced. On a version tag, it can publish the Release in the same call.

## Authentication

`ivaldictl` authenticates in one of two ways.

- **People sign in through their browser.** `ivaldictl auth login` opens the control plane's sign-in page and listens on a loopback port for the redirect. That is the authorization code flow with PKCE, as the `ivaldictl` OAuth client every control plane registers. The session is kept in `credentials.json` in your configuration directory, readable only by you, and refreshed when it expires. Use `--org` to pick an Organization, and `--no-browser` to print the address instead of opening it. `ivaldictl auth logout` revokes the session.
- **CI uses a service account's API token.** Pass it with `--api-token`, or set `IVALDI_API_TOKEN`. In CI (`CI` set, or GitHub Actions or GitLab CI detected), `ivaldictl` never opens a browser. Without a token, it fails at once.

`ivaldictl auth status` shows who the CLI acts as. The control plane's address comes from `--server-url`, then `IVALDI_SERVER_URL`, then `http://localhost:8660`.

## Setting up a service account

An owner or admin, signed in with `ivaldictl auth login`, creates the service account, gives it a role, and issues it a token:

```sh
# Create the service account; note its ID (prn_...)
ivaldictl service-account create --name "GitHub Actions" --description "Releases from CI" -o text

# Give it a role. Creating and publishing Releases needs release.manage and
# release.publish, which admins hold; admin can be granted on a Workspace.
# ivaldictl has no grant command yet, so call the API:
#   POST /v1/grants  {"principal_id": "<service account ID>", "role": "admin", "resource_id": "<workspace ID>"}

# Issue a token. Its secret is shown once.
ivaldictl api-token create --service-account <service account ID> --name github-actions --expires-in 180d -o text
```

Store the secret (it starts with `ivt_`) as a CI secret. Ivaldi keeps only its SHA-256 hash and the first 12 characters, to tell tokens apart. `--expires-at` takes an RFC 3339 time instead of `--expires-in`; with neither, the token never expires.

- **Listing tokens:** `ivaldictl api-token list --service-account <id>` shows each token's prefix and expiry, never its secret.
- **Revoking a token:** `ivaldictl api-token revoke <token ID>` ends it for good.
- **Suspending the account:** `ivaldictl service-account suspend <id>` stops all of its tokens until `ivaldictl service-account reactivate <id>`.
- **The API underneath:** `POST /v1/service-accounts`, `POST /v1/service-accounts/{id}/tokens`, `POST /v1/api-tokens/{id}/revoke`, and `POST /v1/service-accounts/{id}/suspend` or `/reactivate`, with `service_account.manage` on the Organization.

A token acts with its service account's grants. The authorizer checks it like any other caller.

## The command

```sh
ivaldictl release create --app <application ID or slug> --artifact api=sha256:<digest> [--artifact chart=sha256:<digest>] [--publish]
```

- **The Application** is named by its ID or its slug. A slug is unique only within a Project, so if two Projects have an Application with the same slug, the command fails and you must give the ID.
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
          APP: payments-api
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

## Recording which work produced a commit

The deploy sheet lists the commits a Release adds to a Target, and those it takes away, with the Work Item and Session behind each when Ivaldi knows them. CI tells it with a **Change**:

```sh
ivaldictl change record --source <source ID> --title "Add checkout" \
  --work-item <work item ID> --external-ref https://github.com/acme/api/pull/42
```

The commit defaults to the one the pipeline runs on; give `--commit` (repeatable) for others. Run it where that commit is the one that lands, such as a push to `main`, not on a pull request, whose `GITHUB_SHA` is a merge preview that never lands. A commit already recorded in the Source is refused. The service account needs `change.record` on the Source, which admin holds. See [Recording Changes](version-control.md#recording-changes).
