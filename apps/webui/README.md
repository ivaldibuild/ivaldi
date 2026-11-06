# Webui

This project was generated using [Angular CLI](https://github.com/angular/angular-cli) version 22.2.5.

## Sign-in

`/login` is the page the control plane sends browsers to when someone needs to sign in. It reads the `login_challenge` and calls the control plane's `/v1/auth` operations (see [docs/design/sign-in-flow.md](../../docs/design/sign-in-flow.md)). To connect the two:

- Set the control plane's `auth.login_url` to this page, such as `https://app.example.com/login`.
- If the web app is served from a different origin than the control plane, list the web app's origin in the control plane's `allowed_origins`, and put the control plane's address in `src/index.html`:

  ```html
  <meta name="ivaldi-api-url" content="https://ivaldi.example.com" />
  ```

  Leave it empty when both are served from the same origin.

## Development server

To start a local development server, run:

```bash
ng serve
```

Once the server is running, open your browser and navigate to `http://localhost:4200/`. The application will automatically reload whenever you modify any of the source files.

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
