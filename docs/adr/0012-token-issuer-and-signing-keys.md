# ADR 0012: One token issuer with signing keys from configuration

- Status: Proposed
- Date: 2026-11-05
- Related: [ADR 0003](0003-identity-access-and-policy.md), [ADR 0004](0004-credentials-and-keys.md), [docs/design/iam-data-model.md](../design/iam-data-model.md)

## Context

[ADR 0003](0003-identity-access-and-policy.md) makes the control plane the
OAuth 2.1 authorization server for every client: the web app, `ivaldictl`,
and MCP clients. Its access tokens are short-lived JWTs that resource
servers verify offline. The IAM data model leaves open whether each
Organization has its own issuer, or the platform has one issuer, and whether
keys are per Organization. Its `signing_keys` table holds per-Organization
public keys with the private key as a credential or KMS reference
([ADR 0004](0004-credentials-and-keys.md)), but neither the credentials
capability nor a KMS integration exists yet.

The control plane needs to issue tokens now, for sign-in to work.

## Decision

### One platform issuer

The issuer is the control plane's external URL, without a trailing slash.
The same string is the API's resource identifier (RFC 8707) and its tokens'
audience. The authorization server publishes RFC 8414 metadata at
`/.well-known/oauth-authorization-server`, its keys at
`/.well-known/jwks.json`, and the API's protected resource metadata (RFC 9728)
at `/.well-known/oauth-protected-resource`. Because RFC 8414 puts the
well-known path at the root, the external URL may not have a path.

### Signing keys come from configuration

`auth.signing_key_files` lists PEM private keys. The first signs; every one
is published and verifies, so a key is rotated by putting a new one first
and removing the old one once its tokens have expired. Each key's type
decides its algorithm: a P-256 key signs ES256, an Ed25519 key EdDSA, and an
RSA key of at least 2048 bits RS256. The key ID is its RFC 7638 thumbprint.
A verifier takes the algorithm from the key, never from the token.

Without key files the control plane refuses to start, unless
`auth.ephemeral_signing_key` is set for local development: then it generates
a key at start, whose tokens die with the process and which other replicas
cannot verify.

The same keys seal the authorization server's browser cookies: HKDF derives
a separate secret per purpose from each key, so rotating a signing key
rotates the cookie keys without signing anyone out.

### Access tokens

Access tokens are RFC 9068 JWTs (`typ: at+jwt`) carrying `iss`, `sub` (the
principal's ID), `aud`, `exp`, `iat`, `nbf`, `jti`, `org`, `client_id`,
`scope`, and `kind`. They carry no email or account ID. `pkg/identity` owns
the claims' format and the offline verifier.

### `signing_keys` waits for per-Organization keys

The `signing_keys` table stays unused. It is for per-Organization keys held
by the credentials capability or a KMS, when a customer's data-plane gateway
must trust only that customer's tokens.

## Consequences

- Sign-in works without the credentials capability or a KMS; the keys are
  secrets the operator mounts as files, like the provider client secrets.
- A gateway in one customer's data plane trusts tokens for every
  Organization the issuer signs for, and must check `org` itself. Moving to
  per-Organization keys later changes key selection and JWKS publication,
  not the token format.
- Rotation is a configuration change and a rolling restart.

## Alternatives considered

- **Per-Organization issuers** (`<external URL>/orgs/<slug>`) would isolate
  tenants' tokens fully, but multiply discovery documents and complicate
  sign-in for people in several Organizations, before any deployment needs it.
- **Keys generated and stored in the database** would need encryption at rest,
  which waits on [ADR 0004](0004-credentials-and-keys.md)'s credentials
  capability.
- **HMAC-signed tokens** cannot be verified by a party that cannot also
  sign, which rules out offline verification in the data plane.
