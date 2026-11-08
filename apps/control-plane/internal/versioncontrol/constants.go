// Package versioncontrol is the control plane's version-control capability:
// the Connections an Organization uses to reach a provider, and the Sources
// (repositories) connected through them. Delivery's Applications and Builds
// refer to Sources by ID.
//
// The domain types live here; the PostgreSQL repository is in postgres, and
// the services and HTTP APIs are in connections and sources. Lookups
// implements vcs.History over stored Sources, so a capability that needs
// commit history asks this package rather than reading its tables.
package versioncontrol

import (
	"fmt"
	"strings"

	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

func init() {
	id.MustRegister(Connection{}.Prefix(), "versioncontrol.Connection")
	id.MustRegister(Source{}.Prefix(), "versioncontrol.Source")
}

// Aggregate types name the domain objects that raise events.
const (
	AggregateConnection = "versioncontrol.Connection"
	AggregateSource     = "versioncontrol.Source"
)

// Limits on what one record may hold. Text lengths match the columns.
const (
	maxHostURLLength       = 2048
	maxExternalIDLength    = 255
	maxExternalNameLength  = 255
	maxFullNameLength      = 512
	maxDefaultBranchLength = 255
	maxRepositoryKeyLength = 768
	maxCloneURLLength      = 2048
	maxWebURLLength        = 2048
	maxLookupPoolLength    = 255
)

// Permissions this capability asks the authorizer about. Connections belong
// to the Organization; Sources are resources under their Project.
const (
	PermissionConnectionRead   = "connection.read"
	PermissionConnectionManage = "connection.manage"
	PermissionSourceRead       = "source.read"
	PermissionSourceManage     = "source.manage"
)

// Actions name what an actor asks the authorizer to allow.
const (
	ActionConnectionArchive = "connection.archive"
	ActionConnectionCreate  = "connection.create"
	ActionConnectionList    = "connection.list"
	ActionConnectionRead    = "connection.read"
	ActionSourceArchive     = "source.archive"
	ActionSourceCreate      = "source.create"
	ActionSourceList        = "source.list"
	ActionSourceRead        = "source.read"
	ActionSourceRestore     = "source.restore"
)

// Audit event types this capability emits.
const (
	EventConnectionCreated  = "connection.created"
	EventConnectionArchived = "connection.archived"

	EventSourceCreated  = "source.created"
	EventSourceArchived = "source.archived"
	EventSourceRestored = "source.restored"
)

// Visibility is a repository's visibility in its provider.
type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityPrivate  Visibility = "private"
	VisibilityInternal Visibility = "internal"
)

func (v Visibility) valid() error {
	switch v {
	case VisibilityPublic, VisibilityPrivate, VisibilityInternal:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("visibility %q must be public, private, or internal", v))
}

// repositoryKey identifies a repository across the Organization. The
// provider's repository ID is stable across renames and transfers.
func repositoryKey(provider vcs.ProviderKind, hostURL, externalID string) string {
	host := hostURL
	if u, err := parseURL(hostURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return strings.Join([]string{string(provider), strings.TrimSuffix(host, "/"), externalID}, ":")
}
