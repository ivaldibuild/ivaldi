# Sign-in for self-hosted installations

A self-hosted Ivaldi signs people in with Google, GitHub, or any OpenID Connect provider, and optionally with its own email and password accounts. You can turn on any of them at once. For each provider, you register an OAuth app with the provider and give Ivaldi its client ID and secret.

Ivaldi applies these settings every time the control plane starts. To rotate a secret or change who may sign in, change the configuration and restart. Providers set this way cannot be changed through the API, and removing one turns it off without removing anyone's account.

## Before you start

Set `IVALDI_EXTERNAL_URL` to the address people use to reach Ivaldi, such as `https://ivaldi.example.com`. Each provider redirects back to:

```
<external URL>/auth/oauth/<provider>/callback
```

where `<provider>` is `google`, `github`, or `oidc`. Register exactly that URL with the provider. The external URL must use https, unless it is a loopback address for local development, and must not have a path.

## Signing keys

Ivaldi signs the access tokens it issues with keys you provide, and refuses to start without one. Generate a P-256 key and point Ivaldi at it:

```sh
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out signing.pem
IVALDI_AUTH_SIGNING_KEY_FILES=/run/secrets/signing.pem
```

Ed25519 and RSA keys of at least 2048 bits work too. The first file signs and every file verifies, so to rotate a key, list the new one first, restart, and remove the old one once its tokens have expired (`IVALDI_AUTH_ACCESS_TOKEN_TTL`, 15 minutes by default). Every replica must have the same files. For local development only, `IVALDI_AUTH_EPHEMERAL_SIGNING_KEY=true` generates a key at start instead; its tokens stop working when the process restarts. See [ADR 0012](../adr/0012-token-issuer-and-signing-keys.md).

Client secrets are never read from the YAML file. Set them in an environment variable, or point the `_FILE` variable at a file that holds the secret, such as a mounted Kubernetes secret. Setting both is an error.

## Choose who may sign in

Anyone with a Google or GitHub account can authenticate with those providers, so Ivaldi refuses to enable either one until you limit who may sign in. A person may sign in if they match any of the limits you set.

| Provider | Limits |
|---|---|
| Google | `ALLOWED_DOMAINS`: Google Workspace domains, checked against the account's Workspace domain, so personal Gmail accounts never match. `ALLOWED_EMAILS`: specific verified addresses, including Gmail ones. |
| GitHub | `ALLOWED_ORGANIZATIONS`: members of these GitHub organizations. `ALLOWED_EMAILS`: specific addresses GitHub has verified. |
| OIDC | Optional `ALLOWED_DOMAINS` and `ALLOWED_EMAILS`, because your own provider usually decides who has an account. |

Lists are comma-separated in environment variables.

By default, anyone a provider admits becomes a member on their first sign-in. Set `IVALDI_AUTH_<PROVIDER>_JIT_PROVISIONING=false` (for example `IVALDI_AUTH_GITHUB_JIT_PROVISIONING=false`) to stop that; then only the administrator can join through that provider. A first sign-in needs an email the provider has verified. If an account with that email already exists, for example a built-in account, the sign-in is linked to it rather than creating a second one.

## Google

1. In the Google Cloud console, open **APIs & Services → Credentials** and create an **OAuth client ID** of type **Web application**.
2. Add the redirect URI `https://ivaldi.example.com/auth/oauth/google/callback`.
3. Configure Ivaldi:

```sh
IVALDI_AUTH_GOOGLE_CLIENT_ID=1234567890-abc.apps.googleusercontent.com
IVALDI_AUTH_GOOGLE_CLIENT_SECRET_FILE=/run/secrets/google-client-secret
IVALDI_AUTH_GOOGLE_ALLOWED_DOMAINS=example.com
```

## GitHub

1. In GitHub, open **Settings → Developer settings → OAuth Apps** (for an organization, the organization's settings) and create a new app. If you already use a GitHub App to connect repositories, you can use its client ID and secret instead.
2. Set the **Authorization callback URL** to `https://ivaldi.example.com/auth/oauth/github/callback`, and generate a client secret.
3. Configure Ivaldi:

```sh
IVALDI_AUTH_GITHUB_CLIENT_ID=Iv1.0123456789abcdef
IVALDI_AUTH_GITHUB_CLIENT_SECRET_FILE=/run/secrets/github-client-secret
IVALDI_AUTH_GITHUB_ALLOWED_ORGANIZATIONS=example-org
```

To check organization membership, Ivaldi asks for the `read:org` scope. If the organization restricts third-party access, an owner must approve the app before its members can sign in. For GitHub Enterprise Server, also set `IVALDI_AUTH_GITHUB_URL=https://github.example.com`.

## Any OpenID Connect provider

Keycloak, Dex, Entra ID, Okta, and other OIDC providers work the same way. Register Ivaldi as a confidential client with the redirect URI `https://ivaldi.example.com/auth/oauth/oidc/callback`, then:

```sh
IVALDI_AUTH_OIDC_ISSUER_URL=https://login.example.com/realms/acme
IVALDI_AUTH_OIDC_CLIENT_ID=ivaldi
IVALDI_AUTH_OIDC_CLIENT_SECRET_FILE=/run/secrets/oidc-client-secret
IVALDI_AUTH_OIDC_DISPLAY_NAME=Acme SSO
```

`IVALDI_AUTH_OIDC_SCOPES` adds scopes to `openid`; it defaults to `email,profile`.

## Email and password accounts

For a small installation without an identity provider, turn on Ivaldi's own accounts:

```sh
IVALDI_AUTH_BUILTIN_ACCOUNTS=true
```

People with an account can then sign in with their email and password. There is no self-service sign-up, password reset, or email verification yet, so accounts come from the bootstrap below or from a provider sign-in.

## The first administrator

When the database is empty, Ivaldi creates the Organization named by `IVALDI_BOOTSTRAP_ORGANIZATION_NAME`, with the address `IVALDI_BOOTSTRAP_ORGANIZATION_SLUG` (derived from the name when unset). A self-hosted installation has exactly one Organization; Ivaldi refuses to start with an empty database and no bootstrap settings. The bootstrap settings are used only then: owners and admins can later rename the Organization or change its address (`PATCH /v1/organization`), and Ivaldi does not change it back to match the settings when it restarts.

The first person to sign in as `IVALDI_BOOTSTRAP_ADMIN_EMAIL` through any enabled provider becomes the Organization's owner. The provider must have verified that email, and the address must pass the provider's limits, but just-in-time provisioning need not be on. This happens once: as soon as the Organization has an owner, the admin email gives no special rights.

With built-in accounts on, you can instead create the administrator's account when Ivaldi first starts:

```sh
IVALDI_BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/admin-password
```

The file is used once. Ivaldi never replaces a password with it: it does nothing once the admin's account exists or the Organization has an owner, so you can remove it after the first start.

## The sign-in page

The control plane serves no sign-in pages; the Ivaldi web app does. Tell the control plane where its sign-in page is. When a reverse proxy serves the web app and the control plane on one origin, as [Serving the web app](web-app.md) recommends, that is the external URL's `/login`:

```sh
IVALDI_AUTH_LOGIN_URL=https://ivaldi.example.com/login
```

When the web app has its own origin, point at it there and let it call the control plane's API from the browser:

```sh
IVALDI_AUTH_LOGIN_URL=https://app.example.com/login
IVALDI_ALLOWED_ORIGINS=https://app.example.com
```

The web app then also needs the control plane's address; see [Serving the web app](web-app.md#separate-origins). See [the sign-in flow](../design/sign-in-flow.md) for how the page and the control plane work together.

## Applications that sign in

Ivaldi registers its own OAuth clients on every start. `ivaldictl` signs in through a browser and a loopback redirect, and needs no configuration. To let the web app sign people in, register its callback addresses:

```sh
IVALDI_AUTH_WEB_CLIENT_REDIRECT_URIS=https://ivaldi.example.com/auth/callback
```

Use the web app's own origin instead, such as `https://app.example.com/auth/callback`, when it has one.

Clients follow the OAuth 2.1 authorization code flow with PKCE, starting at `<external URL>/oauth/authorize`. The server's metadata is at `<external URL>/.well-known/oauth-authorization-server`.

## In a YAML file

The same settings, except the secrets, can go in the configuration file:

```yaml
external_url: https://ivaldi.example.com
auth:
  login_url: https://ivaldi.example.com/login
  signing_key_files: [/run/secrets/signing.pem]
  builtin_accounts: true
  web_client:
    redirect_uris: [https://ivaldi.example.com/auth/callback]
  providers:
    google:
      client_id: 1234567890-abc.apps.googleusercontent.com
      client_secret_file: /run/secrets/google-client-secret
      allowed_domains: [example.com]
    github:
      client_id: Iv1.0123456789abcdef
      client_secret_file: /run/secrets/github-client-secret
      allowed_organizations: [example-org]
      jit_provisioning: false
bootstrap:
  organization_slug: example
  organization_name: Example
  admin_email: admin@example.com
  admin_password_file: /run/secrets/admin-password
```

With the web app on its own origin, add `allowed_origins: [https://app.example.com]` and use that origin in `login_url` and `redirect_uris`. Environment variables override the file.
