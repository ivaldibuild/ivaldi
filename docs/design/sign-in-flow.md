# Sign-in flow between the control plane and the frontend

The control plane is the OAuth 2.1 authorization server for every client
(the web app, `ivaldictl`, MCP clients), but it renders no pages. The Ivaldi
web app renders sign-in: the authorization server hands it a **login
challenge**, the web app calls the control plane's JSON operations with it,
and sends the browser back to finish. The challenge and the values that
follow are sealed by the control plane, short-lived (15 minutes), and tied
to one sign-in, so the control plane keeps no state between the steps.

Configure the page with `auth.login_url`, such as
`https://ivaldi.example.com/login`. The control plane serves the web app on
its own origin ([ADR 0016](../adr/0016-control-plane-serves-the-web-app.md)),
so the page calls the API on the same origin and nothing needs CORS.

## The flow

```
client       →  GET  /oauth/authorize?response_type=code&client_id=…&code_challenge=…
control plane ← 302  <login_url>?login_challenge=LC
                     (and binds LC to this browser with a cookie)

web app      →  GET  /v1/auth/login?login_challenge=LC
             ←  { client_name, password, sign_up, providers: [{kind, name, start_url}] }

  password:  →  POST /v1/auth/login/password  { login_challenge, email, password }
  provider:  browser → start_url → provider → /auth/oauth/<kind>/callback
                    → 302 to redirect_to, or back to <login_url> (below)

each step    ←  { redirect_to }                           → send the browser there
             or { login_session, organizations: [...] }   → let the person choose, then
                POST /v1/auth/login/organization { login_challenge, login_session, organization_id }

A provider callback that needs a choice sends the browser back with only
login_challenge and login_session; the web app continues it with
POST /v1/auth/login/organizations { login_challenge, login_session }, which
answers like any step against the memberships as they are now (a choice,
redirect_to when only one remains, 403 when none does, 400 when the pair
doesn't match or has expired). It is a POST so the sealed values stay out of
URLs and access logs.

browser      →  GET  /oauth/authorize/continue?login_verifier=…
             ← 302  <client redirect_uri>?code=…&state=…&iss=…
```

Only the browser that opened `/oauth/authorize` can finish at
`/oauth/authorize/continue`. The control plane then remembers the sign-in
in its own cookie, so the next authorization skips the web app when the
Organization is clear: there is one, or the client passed
`organization=<slug or ID>`. With `prompt=login` the web app is asked again;
with `prompt=none` the client gets `login_required` instead.
A slug changes when an owner or admin changes the Organization's address
(`PATCH /v1/organization`), and from then on the old slug matches nothing:
a hint naming it is treated as naming no Organization the person belongs
to, so the web app asks them to choose. Clients that keep a hint should
keep the Organization's ID, which never changes.

## Coming back from a provider

After a provider sign-in, the control plane sends the browser either on to
`/oauth/authorize/continue`, or back to `<login_url>` with:

| Query parameters | Meaning |
|---|---|
| `login_challenge`, `login_session` | Signed in; choose an Organization. Continue with `POST /v1/auth/login/organizations { login_challenge, login_session }`, which answers like any step, then choose with `POST /v1/auth/login/organization`. |
| `login_challenge`, `signup_token`, `email` | Managed cloud only: no account yet. Ask for the Organization's name, then `POST /v1/auth/signup { login_challenge, signup_token, organization_name, organization_slug? }`, which answers like any step. |
| `login_challenge`, `error` | The sign-in failed; offer another way. |
| `error` alone | The authorization request itself was invalid (`invalid_request`, with `error_description`), or the sign-in expired; the person must start again from the application. |

`error` is one of `login_expired`, `cancelled` (at the provider),
`provider_failed`, `not_admitted` (the provider's limits refuse the account),
`no_membership`, `account_exists` (sign in another way first, to link it),
`account_disabled`, or `server_error`. `GET /auth/logout` ends the browser's
sign-in at the control plane and returns to `<login_url>?logged_out=true`.

## JSON errors

The `/v1/auth` operations answer problems as `application/problem+json`:
400 for an expired or mismatched challenge, session, or sign-up token; 401
for a wrong email or password (never saying which); 403 when the account,
provider, or organization does not allow the sign-in; 409 when an
organization's name or address is taken, or the person already has an
account; 422 for invalid input.

## After sign-in: who is signed in

The access token carries only IDs: the principal (`sub`), the Organization
(`org`), the actor kind, and, for an agent Session, `sid`. To show who is
signed in, the frontend calls `GET /v1/me` with the token. It needs no
permission beyond a valid token, and returns the caller's principal (its
`display_name` in this Organization, and the Account's `email` for a user,
`null` otherwise), the Organization (`id`, `slug`, `display_name`,
`logo_url`), and the roles the caller holds on the Organization, directly or
through a group, as `{ role, role_id, resource_id }`, where `role` is the
built-in role's key (`owner`, `admin`, `member`, `viewer`) and empty for a
custom role. It answers 403 once the caller's principal, Account, or
Organization is no longer active, even while the token has not expired.
