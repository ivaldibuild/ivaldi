# Sign-in for self-hosted installations

A self-hosted Ivaldi signs people in with Google, GitHub, or any OpenID Connect provider. You can turn on any of them at once. For each one, you register an OAuth app with the provider and give Ivaldi its client ID and secret.

Ivaldi applies these settings every time the control plane starts. To rotate a secret or change who may sign in, change the configuration and restart. Providers set this way cannot be changed through the API, and removing one turns it off without removing anyone's account.

## Before you start

Set `IVALDI_EXTERNAL_URL` to the address people use to reach Ivaldi, such as `https://ivaldi.example.com`. Each provider redirects back to:

```
<external URL>/auth/oauth/<provider>/callback
```

where `<provider>` is `google`, `github`, or `oidc`. Register exactly that URL with the provider.

Client secrets are never read from the YAML file. Set them in an environment variable, or point the `_FILE` variable at a file that holds the secret, such as a mounted Kubernetes secret. Setting both is an error.

## Choose who may sign in

Anyone with a Google or GitHub account can authenticate with those providers, so Ivaldi refuses to enable either one until you limit who may sign in. A person may sign in if they match any of the limits you set.

| Provider | Limits |
|---|---|
| Google | `ALLOWED_DOMAINS`: Google Workspace domains, checked against the account's Workspace domain, so personal Gmail accounts never match. `ALLOWED_EMAILS`: specific verified addresses, including Gmail ones. |
| GitHub | `ALLOWED_ORGANIZATIONS`: members of these GitHub organizations. `ALLOWED_EMAILS`: specific addresses GitHub has verified. |
| OIDC | Optional `ALLOWED_DOMAINS` and `ALLOWED_EMAILS`, because your own provider usually decides who has an account. |

Lists are comma-separated in environment variables.

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

## The first administrator

When the database is empty, Ivaldi creates the Organization named by `IVALDI_BOOTSTRAP_ORGANIZATION_SLUG` and `IVALDI_BOOTSTRAP_ORGANIZATION_NAME`. The first person to sign in as `IVALDI_BOOTSTRAP_ADMIN_EMAIL`, through any enabled provider, becomes its administrator. That address must also pass the provider's limits.

## In a YAML file

The same settings, except the secrets, can go in the configuration file:

```yaml
external_url: https://ivaldi.example.com
auth:
  providers:
    google:
      client_id: 1234567890-abc.apps.googleusercontent.com
      client_secret_file: /run/secrets/google-client-secret
      allowed_domains: [example.com]
    github:
      client_id: Iv1.0123456789abcdef
      client_secret_file: /run/secrets/github-client-secret
      allowed_organizations: [example-org]
bootstrap:
  organization_slug: example
  organization_name: Example
  admin_email: admin@example.com
```

Environment variables override the file.
