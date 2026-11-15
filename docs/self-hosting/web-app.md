# The web app

The control plane serves the web app itself, from the same image and the same origin (`ivaldi/vX.Y.Z`, see [ADR 0016](../adr/0016-control-plane-serves-the-web-app.md)). A self-hosted installation runs one container, and the browser needs no configuration: it calls the API on the origin the page came from.

```sh
docker run -p 8660:8660 -v "$PWD/config.yml:/ivaldi/config.yml:ro" \
  ghcr.io/ivaldibuild/ivaldi:1.2.3 serve --config /ivaldi/config.yml
```

Open `http://localhost:8660`. A deployment behind a reverse proxy or load balancer terminates TLS there and forwards everything to port 8660, unchanged:

```caddy
ivaldi.example.com {
	reverse_proxy ivaldi:8660
}
```

Nothing needs splitting by path: `/v1`, `/oauth`, `/.well-known/`, `/auth/oauth/`, `/auth/logout`, `/openapi.json`, and `/healthz` are the API's, and every other path is a route of the single-page app. Set `external_url` to the address browsers use, because the sign-in flow and the published metadata are built from it:

```sh
IVALDI_EXTERNAL_URL=https://ivaldi.example.com
IVALDI_AUTH_LOGIN_URL=https://ivaldi.example.com/login
IVALDI_AUTH_WEB_CLIENT_REDIRECT_URIS=https://ivaldi.example.com/auth/callback
```

There is no CORS to configure and no `IVALDI_API_URL`: one origin needs neither.

## Which paths the API answers

| Path | Served by |
| --- | --- |
| `/v1/` | the API, including a JSON 404 for an operation that does not exist |
| `/oauth/`, `/.well-known/` | the API: authorization server and metadata |
| `/auth/oauth/`, `/auth/logout` | the API |
| `/login`, `/auth/callback` | the web app |
| `/openapi.json`, `/healthz` | the API |
| everything else | the web app: a file when one matches, `index.html` for its routes |

A request for a missing file, such as an asset named by an old build, is a 404 rather than `index.html`, so a stale page fails visibly instead of loading the app again.

## Caching

`index.html` is revalidated on every load, so a new release is picked up on the next navigation. Files whose names carry a content hash, and everything under `/media/`, are immutable and cached for a year.

## During development

`ng serve` in `apps/webui` remains the live-reload server, on port 8670, proxying the API's paths to the control plane on 8660 (`apps/webui/proxy.conf.json`). `make local_serve` serves a built copy instead when one exists: run `make webui` first. Both local origins are listed in the web client's `redirect_uris` in `config.local.yml`, so signing in works either way.

## Without the image

The build output is plain files. Run `pnpm build` in `apps/webui` and serve `dist/webui/browser` from any static host, with `index.html` as the fallback for paths that are not files. That is not a supported deployment: releases ship the image, and the web app and the API are tested together.
