# Webui

This project was generated using [Angular CLI](https://github.com/angular/angular-cli) version 22.2.5.

## Sign-in

`/login` is the page the control plane sends browsers to when someone needs to sign in. It reads the `login_challenge` and calls the control plane's `/v1/auth` operations (see [docs/design/sign-in-flow.md](../../docs/design/sign-in-flow.md)). To connect the two:

- Set the control plane's `auth.login_url` to this page, such as `https://app.example.com/login`.
- Serve the web app and the control plane on one origin, and the web app needs no configuration. If the web app has its own origin, list it in the control plane's `allowed_origins` and give the web app the control plane's address: the image reads `IVALDI_API_URL` when it starts and writes it into the `<meta name="ivaldi-api-url">` tag in `index.html`.

See [Serving the web app](../../docs/self-hosting/web-app.md) for the image, the reverse proxy routes, and both layouts.

## Development server

To start a local development server, run:

```bash
ng serve
```

Once the server is running, open your browser and navigate to `http://localhost:3000/`. The application will automatically reload whenever you modify any of the source files.

The dev server forwards the control plane's paths (`/v1`, `/oauth`, `/.well-known`, `/auth/oauth`, `/auth/logout`) to `http://localhost:8660`, as a reverse proxy would on one origin (see `proxy.conf.json`). Start the control plane first, with `make local` and `make local_serve` from the repository root, or with `make local_stack` followed by `docker compose stop webui`, because the stack's web app also uses port 3000. Port 3000 matches `login_url`, `allowed_origins`, and the web client's redirect URIs in `config.local.yml`.

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

For end-to-end (e2e) testing, run:

```bash
ng e2e
```

Angular CLI does not come with an end-to-end testing framework by default. You can choose one that suits your needs.

## Additional Resources

For more information on using the Angular CLI, including detailed command references, visit the [Angular CLI Overview and Command Reference](https://angular.dev/tools/cli) page.
