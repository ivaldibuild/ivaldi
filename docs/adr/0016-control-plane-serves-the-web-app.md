# ADR 0016: The control plane serves the web app

- Status: Proposed
- Date: 2026-11-15

## Context

The web app has shipped as its own image: Angular build output served by nginx on port 8080, with an entrypoint script that wrote the control plane's address into an `<meta>` tag in `index.html`, and a control-plane setting, `allowed_origins`, so the browser could call the API from another origin. A self-hosted installation therefore ran two containers and a reverse proxy that forwarded `/v1`, `/oauth`, `/.well-known`, `/auth/oauth`, and `/auth/logout` to the control plane and everything else, except `/auth/callback`, to the web app. `docs/self-hosting/web-app.md` spends most of its length explaining that split.

The split exists for deployments that want a CDN or a separate origin for the browser code. Neither Ivaldi mode needs one: a self-hosted installation runs both halves together, and the managed cloud can put a load balancer in front of a single image just as easily as in front of two.

Meanwhile the split costs something on every release: a second image to build, tag, promote, document, and keep API-compatible with the control plane.

## Decision

**The control plane serves the web app from the same origin, in the same image.** `ivaldi serve` answers `/v1`, `/oauth`, `/.well-known/`, `/auth/oauth/`, `/auth/logout`, `/openapi.json`, and `/healthz` from the API, and every other `GET` from the web app: a matching file when one exists, and `index.html` for the routes of the single-page app.

**The web app is embedded in the binary at image build time**, with `//go:embed` behind the `webui` build tag. `apps/control-plane/Dockerfile` builds the web app first, copies the output into `apps/control-plane/internal/webui/dist`, and compiles with `-tags webui`. A build without the tag (`go build`, `go test`, `make build`) carries no web app and needs no pnpm. `ivaldi serve --web-dir` serves a build from a directory instead, which is how `make local_serve` shows a local build; `ng serve` with its proxy remains the live-reload development loop.

**The web app's image, its nginx configuration, and `allowed_origins` are deleted.** With one origin the browser sends no `Origin` header, so the CORS middleware has nothing to allow. `IVALDI_API_URL` disappears too: the address is the page's own origin.

**The web app has no version of its own.** It is released as part of `ivaldi/vX.Y.Z`, and the image's label carries that version, the commit, and the commit's time.

## Consequences

- One image, one digest, one version, and one release entry for a self-hosted installation. A release promotes one image instead of two.
- No CORS configuration, no cross-origin sign-in, no reverse-proxy rules to keep in step with the API's paths, and no second Dockerfile or nginx configuration to maintain.
- A web-app change requires a control plane rollout, and a control plane that is down no longer serves a login screen. Both are acceptable for a product whose two halves are always deployed together, and both are visible in the release notes.
- The control plane image grows by the web app's build output, and its build gains a node stage of a few minutes. The binary that carries it also carries it in every role: `ivaldi gateway`, `worker`, and `indexer` embed the web app and never serve it.
- Separate-origin deployment is no longer supported. Anyone who needs it can serve the build output from their own static host, but Ivaldi does not test that path and does not ship an image for it.
- A path under `/v1` that no operation claims answers the API's 404, not the web app's `index.html`, so a mistyped API call never reads HTML as a response. This is a server behaviour with a test, not a rule for reviewers.

## Alternatives considered

- **Keep two images and release them together.** Rejected: it keeps every cost of the split and removes only the version skew that the split was never actually about.
- **Serve the web app from disk rather than embedding it.** Simpler to build, but the image then depends on a directory that can be mounted over or left out, and the binary is no longer the whole product. The `--web-dir` flag covers the local case without that risk.
- **Automatic gzip and cache headers in a proxy in front of both halves.** Rejected: the static-file handler already sets the cache policy the nginx configuration did, and the deployment stops needing a proxy to be correct.
