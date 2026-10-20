// Package release is the shared core of the delivery capability (ADR 0006):
// what the control plane and the runtime agent must agree on, without
// storage. The records live in the control plane's internal/delivery.
//
// So far it holds version parsing and comparison, version ranges, and the
// configuration of Channel rules. Deployment status values and transitions,
// and matching Releases against rules, follow with Releases and Deployments.
package release
