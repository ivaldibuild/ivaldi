# Serving the web app

The Ivaldi web app is a set of static files that runs in the browser and calls the control plane's API. It ships as its own image, which serves the files with nginx on port 8080 as an unprivileged user:

```sh
docker build -t ivaldi-webui apps/webui
```

You can run it in one of two ways:

- **On one origin with the control plane (recommended).** A reverse proxy serves both under one address, such as `https://ivaldi.example.com`. The web app needs no configuration, and the control plane needs no `allowed_origins`.
- **On its own origin**, such as `https://app.example.com`, with the control plane at `https://ivaldi.example.com`. You tell the web app where the control plane is, and tell the control plane to accept requests from the web app.

## One origin

Your reverse proxy sends the control plane's paths to the control plane and everything else to the web app:

| Path | Goes to |
| --- | --- |
| `/v1/`, the API | control plane |
| `/oauth/`, the authorization server | control plane |
| `/.well-known/`, the server metadata and signing keys | control plane |
| `/auth/oauth/`, sign-in through Google, GitHub, or OIDC | control plane |
| `/auth/logout` | control plane |
| `/healthz` | control plane |
| everything else, including `/login` and `/auth/callback` | web app |

`/auth/` is shared: `/auth/oauth/` and `/auth/logout` belong to the control plane, and `/auth/callback` to the web app. Route the two control plane paths, not the whole `/auth/` prefix.

Configure the control plane with the one address:

```sh
IVALDI_EXTERNAL_URL=https://ivaldi.example.com
IVALDI_AUTH_LOGIN_URL=https://ivaldi.example.com/login
IVALDI_AUTH_WEB_CLIENT_REDIRECT_URIS=https://ivaldi.example.com/auth/callback
```

Leave `IVALDI_ALLOWED_ORIGINS` unset: the browser sends no cross-origin requests. Leave the web app's `IVALDI_API_URL` unset too.

### Caddy

```caddy
ivaldi.example.com {
	@control_plane path /v1/* /oauth/* /.well-known/* /auth/oauth/* /auth/logout /healthz
	reverse_proxy @control_plane ivaldi:8660
	reverse_proxy ivaldi-webui:8080
}
```

### nginx

```nginx
server {
    listen 443 ssl;
    server_name ivaldi.example.com;
    # ssl_certificate and ssl_certificate_key go here.

    location ~ ^/(v1|oauth|\.well-known|auth/oauth)/|^/(auth/logout|healthz)$ {
        proxy_pass http://ivaldi:8660;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        proxy_pass http://ivaldi-webui:8080;
        proxy_set_header Host $host;
    }
}
```

`ivaldi` and `ivaldi-webui` are the containers' host names, and 8660 is the control plane's default port (`IVALDI_PORT`).

## Separate origins

Give the web app the control plane's address in `IVALDI_API_URL`:

```sh
docker run -p 8080:8080 -e IVALDI_API_URL=https://ivaldi.example.com ivaldi-webui
```

The value is an origin: a scheme, a host, and an optional port, with no path. When the container starts, it writes the value into the web app's `index.html`, and it refuses to start if the value is not an origin. Changing the value takes a restart, not a new image.

Then let the web app's origin call the control plane, and point the control plane at the web app's pages:

```sh
IVALDI_EXTERNAL_URL=https://ivaldi.example.com
IVALDI_ALLOWED_ORIGINS=https://app.example.com
IVALDI_AUTH_LOGIN_URL=https://app.example.com/login
IVALDI_AUTH_WEB_CLIENT_REDIRECT_URIS=https://app.example.com/auth/callback
```

## Without the image

If you serve the built files another way, run `pnpm build` in `apps/webui` and serve `dist/webui/browser`. Serve `index.html` for any path that is not a file, because the web app handles its own routes. For separate origins, set the control plane's address in `index.html` yourself:

```html
<meta name="ivaldi-api-url" content="https://ivaldi.example.com">
```

Leave it empty on one origin.
