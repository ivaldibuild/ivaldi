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
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/id"
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
	maxHostURLLength         = 2048
	maxExternalIDLength      = 255
	maxExternalNameLength    = 255
	maxFullNameLength        = 512
	maxDefaultBranchLength   = 255
	maxRepositoryKeyLength   = 768
	maxCloneURLLength        = 2048
	maxWebURLLength          = 2048
	maxLookupPoolLength      = 255
	maxWebhookRouteKeyLength = 512
)

// InstallationTTL bounds an install flow, from starting it to coming back
// from the provider.
const InstallationTTL = 30 * time.Minute

// Permissions this capability asks the authorizer about. Connections belong
// to the Organization; Sources are resources under their Project.
const (
	PermissionConnectionRead   = "connection.read"
	PermissionConnectionManage = "connection.manage"
	PermissionSourceRead       = "source.read"
	PermissionSourceManage     = "source.manage"
	// PermissionWebhookRouteResolve lets a platform workload, the gateway,
	// resolve a webhook route key across every Organization (ADR 0015). IAM
	// lists it under the gateway component; no role holds it.
	PermissionWebhookRouteResolve = "version_control.webhook_route.resolve"
)

// Actions name what an actor asks the authorizer to allow.
const (
	ActionConnectionArchive          = "connection.archive"
	ActionWebhookRouteResolve        = "webhook_route.resolve"
	ActionConnectionCreate           = "connection.create"
	ActionConnectionInstall          = "connection.install"
	ActionConnectionList             = "connection.list"
	ActionConnectionListRepositories = "connection.list_repositories"
	ActionConnectionReactivate       = "connection.reactivate"
	ActionConnectionRead             = "connection.read"
	ActionConnectionRevoke           = "connection.revoke"
	ActionConnectionSuspend          = "connection.suspend"
	ActionSourceArchive              = "source.archive"
	ActionSourceCreate               = "source.create"
	ActionSourceList                 = "source.list"
	ActionSourceRead                 = "source.read"
	ActionSourceRefresh              = "source.refresh"
	ActionSourceRestore              = "source.restore"
)

// Audit event types this capability emits.
const (
	EventConnectionCreated     = "connection.created"
	EventConnectionArchived    = "connection.archived"
	EventConnectionSuspended   = "connection.suspended"
	EventConnectionRevoked     = "connection.revoked"
	EventConnectionReactivated = "connection.reactivated"

	EventSourceCreated  = "source.created"
	EventSourceUpdated  = "source.updated"
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
