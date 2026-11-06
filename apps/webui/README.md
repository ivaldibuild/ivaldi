# Webui

This project was generated using [Angular CLI](https://github.com/angular/angular-cli) version 22.2.5.

## Sign-in

`/login` is the page the control plane sends browsers to when someone needs to sign in. It reads the `login_challenge` and calls the control plane's `/v1/auth` operations (see [docs/design/sign-in-flow.md](../../docs/design/sign-in-flow.md)). To connect the two:

- Set the control plane's `auth.login_url` to this page, such as `https://app.example.com/login`.
- Serve the web app and the control plane on one origin, and the web app needs no configuration. If the web app has its own origin, list it in the control plane's `allowed_origins` and give the web app the control plane's address: the image reads `IVALDI_API_URL` when it starts and writes it into the `<meta name="ivaldi-api-url">` tag in `index.html`.

See [Serving the web app](../../docs/self-hosting/web-app.md) for the image, the reverse proxy routes, and both layouts.

## Signing in to the web app

The web app signs people in as the control plane's first-party OAuth client, `ivaldi-web`, with the authorization code flow and PKCE. Register its callback, such as `https://app.example.com/auth/callback`, in the control plane's `auth.web_client.redirect_uris`. Pages that need a signed-in person start that flow on their own (`requireSignIn`), and `authInterceptor` adds the access token to `/v1` calls. Tokens stay in memory: after a reload the web app authorizes again, and the control plane, which remembers the sign-in, sends it straight back.

## First-run setup

`/setup` walks an owner through a new installation: the default Workspace, how sign-in is configured, a first Team with its agents, Environments with Standard and Hotfix Lifecycles, and a first Kubernetes Target. Each step calls the control plane's `/v1` operations directly, so it also works as a review of what already exists.

## Delivery

Signed in, the web app opens on the delivery screens, for the Workspace chosen in the sidebar:

- **Applications** (`/applications`): each Application by Environment, listing the Targets it's placed on, and a form to create one, with its Project if needed.
- **An Application** (`/applications/:id`): its Channels and the Lifecycles they follow, its artifacts, and its placements, with forms to declare an artifact and place it on a Target.
- **Environments** and **Lifecycles**: what the Workspace has, read-only. The first-run setup creates them.

The control plane doesn't serve Builds, Releases or Deployments yet, so these screens show configuration, not which version runs where.

## Development server

To start a local development server, run:

```bash
ng serve
```

Once the server is running, open your browser and navigate to `http://localhost:8670/`. The application will automatically reload whenever you modify any of the source files.

The dev server forwards the control plane's paths (`/v1`, `/oauth`, `/.well-known`, `/auth/oauth`, `/auth/logout`) to `http://localhost:8660`, as a reverse proxy would on one origin (see `proxy.conf.json`). Start the control plane first, with `make local` and `make local_serve` from the repository root, or with `make local_stack` followed by `docker compose stop webui`, because the stack's web app also uses port 8670. Port 8670 matches `login_url`, `allowed_origins`, and the web client's redirect URIs in `config.local.yml`.

## Code scaffolding

Angular CLI includes powerful code scaffolding tools. To generate a new component, run:

```bash
ng generate component component-name
```

For a complete list of available schematics (such as `components`, `directives`, or `pipes`), run:

```bash
ng generate --help
```

## Building

To build the project run:

```bash
ng build
```

This will compile your project and store the build artifacts in the `dist/` directory. By default, the production build optimizes your application for performance and speed.

## Running unit tests

To execute unit tests with the [Vitest](https://vitest.dev/) test runner, use the following command:

```bash
ng test
```

## Running end-to-end tests

The Playwright tests in `e2e/` drive the real sign-in flow against the local stack, in Chromium, Firefox, and WebKit. Start the stack from the repository root, then run them:

```bash
make local_stack          # from the repository root
pnpm exec playwright install   # once, to download the browsers
pnpm e2e
```

They use `http://localhost:8670` for the web app and `http://localhost:8660` for the control plane; set `IVALDI_E2E_WEB_URL` and `IVALDI_E2E_API_URL` to test elsewhere, such as `ng serve` with `make local_serve`. The password sign-in test signs in as the bootstrap administrator: `bootstrap.admin_email` from `config.local.yml` and the password in `.local/admin-password`, or `IVALDI_E2E_ADMIN_EMAIL` and `IVALDI_E2E_ADMIN_PASSWORD`. Without either, it is skipped.

`make local_stack` builds the web app's image from the source at that moment, so rebuild it (run it again) to test changes, or test `ng serve` instead.

## Additional Resources

For more information on using the Angular CLI, including detailed command references, visit the [Angular CLI Overview and Command Reference](https://angular.dev/tools/cli) page.
