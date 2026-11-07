// Package release is the shared core of the delivery capability (ADR 0006):
// what the control plane and the runtime agent must agree on, without
// storage. The records live in the control plane's internal/delivery.
//
// It holds version parsing and comparison, version ranges, the
// configuration of Channel rules and matching Releases against them, and
// Release and Deployment status values and their transitions.
package release
