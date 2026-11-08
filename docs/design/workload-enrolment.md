# Workload enrolment and credentials

- Status: Implemented (control plane)
- Date: 2026-11-07
- Related: [ADR 0001](../adr/0001-deployment-topology.md), [ADR 0003](../adr/0003-identity-access-and-policy.md), [ADR 0006](../adr/0006-release-and-delivery-tracking.md), [ADR 0008](../adr/0008-reaching-customer-systems.md), [IAM data model](iam-data-model.md)

> [ADR 0014](../adr/0014-agents-serve-clusters.md) moves runtime-agent enrolment from Targets to Clusters. This document describes what is implemented today and is updated when that change is.

## Decision

A data-plane workload, such as a runtime agent, enrols for one Target and
authenticates as a `workload` principal. Three credential mechanisms are
supported:

| Method | Enrolment evidence | Long-term credential | Renewal |
|---|---|---|---|
| `bearer` | Enrolment token | Ivaldi workload token `iwt_…` | New token; the old one keeps a short grace period |
| `mtls` | Enrolment token + client certificate | Customer-issued X.509 certificate | Credential record is extended; certificate renewal stays with the customer PKI |
| `kubernetes` | Enrolment token + projected service-account token | Kubernetes projected token | Credential record is extended after verifying a fresh projected token |

Every credential has a fixed expiry. A workload renews before expiry. If a
credential expires or is revoked, a new single-use enrolment token is required.

## Tokens

- Enrolment token: `iwe_` prefix, 32 random bytes, SHA-256 hash at rest,
  single-use, short-lived (`runners.enrollment_token_ttl`, default 1 hour).
- Bearer workload token: `iwt_` prefix, 32 random bytes, SHA-256 hash at rest,
  expires after `runners.workload_credential_ttl` (default 24 hours).

Neither raw token is stored by the control plane.

## Enrolment

`POST /v1/targets/{target_id}/enrolments` asks for an enrolment token with a
chosen method and requires `environment.manage` on the Target's Environment.

`POST /v1/agent/enrolments/redeem` is public; the enrolment token is the
credential. Redeem:

1. locks and validates the enrolment token;
2. locks the Target;
3. creates a `runtime_agent` workload principal;
4. creates the selected credential, revoking the Organization's previous
   credential for the same certificate or service-account identity;
5. redeems the token;
6. suspends the previous workload and revokes its credentials;
7. sets `target.agent_principal_id`;
8. records events and audit in one transaction.

Re-enrolment is the replacement path. A certificate or service-account
identity names one live workload: another Organization cannot enrol the same
identity, because a request is authenticated before its Organization is known.

## Renewal

`POST /v1/agent/credentials/renew` is authenticated by the current workload
credential. The workload must still be the Target's assigned agent.

- **Bearer:** issues a new token and credential. The old credential gets a
  5-minute grace period and records `replaced_by`.
- **mTLS:** extends the existing credential's `not_after`, bounded by the
  certificate expiry. Ivaldi does not issue certificates.
- **Kubernetes:** verifies a fresh projected token and extends the existing
  credential's `not_after`. Ivaldi does not rotate Kubernetes tokens.

A client should start renewal at 50% of the TTL with ±5% jitter, and retry
while the current credential remains valid.

## Verification

`internal/iam/workloadauth` wraps the API-token verifier:

- `mtls:<certificate fingerprint>|<expiry>` from a verified TLS peer
  certificate → `mtls_certificate` credential.
- `iwt_…` → bearer credential by SHA-256 hash.
- `k8s:<token>` → Kubernetes OIDC verification, then
  `kubernetes_service_account` credential.
- anything else → existing API-token/access-token verifier.

Kubernetes tokens are verified through the issuer's OIDC discovery/JWKS. The
issuer must be in `runners.kubernetes_issuers`; the audience must match
`runners.kubernetes_audience`. A workload presents its projected token as a
bearer token with `X-Ivaldi-Credential: kubernetes`, so the verifier tells it
apart from an Ivaldi access token.

mTLS uses the main API TLS listener with `tls.client_ca_file`. When TLS is
terminated by a proxy, mTLS needs the proxy to forward a verified client
certificate; otherwise use bearer or Kubernetes identity.

## ADR 0008

Every workload step is outbound: redeem, renewal, heartbeat, assignment
polling, and reports. The control plane never reaches into a customer cluster.
Kubernetes OIDC verification is the only control-plane outbound request, and
only to an explicitly allowed issuer.
