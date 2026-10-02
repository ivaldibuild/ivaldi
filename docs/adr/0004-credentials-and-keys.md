# ADR 0004: Credentials and customer-managed keys

- Status: Proposed
- Date: 2026-10-01

## Context

The managed cloud must support bring-your-own-keys (BYOK), and self-hosted installations must manage their own secrets. "Keys" covers two separate needs:

- **Provider credentials:** model API keys (e.g. Anthropic, OpenAI), VCS tokens and app keys, CI credentials, and webhook secrets that Ivaldi or its agent Sessions use to call other systems.
- **Encryption keys:** customer-managed keys that protect tenant data stored by Ivaldi.

Under bring-your-own-compute ([ADR 0001](0001-deployment-topology.md)), customers expect raw secrets to stay in their environment.

## Decision

Credentials are their own capability, separate from IAM and policy:

- IAM decides **who** the actor is.
- Policy decides **what** the actor may do.
- The credentials capability supplies **the secret** an authorized actor may use.

([ADR 0003](0003-identity-access-and-policy.md))

### Provider credentials

- **A credential broker** in the control plane gives a Session only the credentials its IAM grants and policy allow, for a limited time.
- **The control plane stores references, not secrets, when the customer runs the data plane.** It records which secret to use and where it lives. The data plane resolves the reference from the customer's own secret store (e.g. HashiCorp Vault, AWS Secrets Manager, or Kubernetes secrets), so the raw secret never reaches Ivaldi's cloud.
- **Secret stores sit behind an Ivaldi-owned interface.** Implementations for specific stores are providers, not domain code.
- **Credentials are injected into a Session's execution environment** with the narrowest practical scope and lifetime. They are not written into agent conversations or logs.
- **Every credential issuance is recorded as an audit event** linked to the Session.

### Customer-managed encryption keys

- **Tenant data is encrypted with envelope encryption.** A data key encrypts the data, and the data key is protected by a key held in the customer's key-management service, such as AWS KMS or Vault Transit.
- **The key-management service sits behind an Ivaldi-owned interface**, with a local implementation for self-hosted installations that do not use an external KMS.

## Consequences

- BYOC customers can keep provider secrets in their own environment. The managed control plane holds only references and audit records.
- Every capability that needs a secret goes through the broker instead of reading configuration directly. This adds an indirection, but it gives one place for scoping, rotation, and audit.
- A customer revoking or rotating their KMS key affects Ivaldi's ability to read that tenant's data. This is intended, and operational tooling must report it clearly.
- The broker and key-management interfaces are needed by both planes, so their interfaces may live in `pkg/`; storage and issuance stay in the control plane.

## Alternatives considered

- **Store all provider secrets in the control-plane database, encrypted with an Ivaldi-held key.** Rejected for BYOC and BYOK: raw secrets would reach Ivaldi's cloud, and customers would not control the key.
- **Fold credentials into IAM.** Rejected: identity and secret delivery have different storage, rotation, and audit needs, and keeping them apart limits the impact if either is compromised.

## Open questions

- Which secret stores and KMS providers to support first.
- Whether managed-cloud tenants without BYOK keep secrets in an Ivaldi-operated store, and how that store is isolated per tenant.
