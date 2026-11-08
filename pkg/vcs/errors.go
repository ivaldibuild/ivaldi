package vcs

import "errors"

var (
	// ErrNotFound means the provider has no such repository, commit, or range.
	ErrNotFound = errors.New("not found in the version-control provider")
	// ErrUnauthorized means the provider refused the request.
	ErrUnauthorized = errors.New("the version-control provider refused the request")
	// ErrUnavailable means the provider could not be reached, or asked Ivaldi
	// to slow down.
	ErrUnavailable = errors.New("the version-control provider is unavailable")
	// ErrUnsupportedProvider means no provider implementation is configured
	// for the connection.
	ErrUnsupportedProvider = errors.New("the version-control provider is not supported")
	// ErrLookupInDataPlane means the connection's lookups run in the data
	// plane, so the control plane must not contact it (ADR 0008).
	ErrLookupInDataPlane = errors.New("lookups for this connection run in the data plane")
	// ErrSourceArchived means the Source was disconnected.
	ErrSourceArchived = errors.New("the source is archived")
	// ErrConnectionInactive means the Source's Connection is archived,
	// suspended, or revoked.
	ErrConnectionInactive = errors.New("the source's connection is not active")
	// ErrInvalidRange means a commit range is missing an endpoint.
	ErrInvalidRange = errors.New("a commit range needs two commits")
	// ErrRangeTooLarge means a commit range holds more commits than a
	// provider reads in one lookup.
	ErrRangeTooLarge = errors.New("the commit range holds too many commits")
	// ErrTooManyRepositories means an account holds more repositories than
	// a provider lists in one lookup.
	ErrTooManyRepositories = errors.New("the account holds too many repositories to list")
)
