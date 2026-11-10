package webhook

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// GitHub event names.
const (
	eventRepository   = "repository"
	eventInstallation = "installation"
)

// Normalizer maps GitHub webhooks to the shared normalized contract.
type Normalizer struct {
	provider vcs.ProviderKind
	hostURL  string
}

// NewNormalizer returns a normalizer for one provider host.
func NewNormalizer(provider vcs.ProviderKind, hostURL string) (*Normalizer, error) {
	switch {
	case !provider.Valid():
		return nil, fmt.Errorf("webhook: provider %q is not supported", provider)
	case hostURL == "":
		return nil, fmt.Errorf("webhook: a provider host is required")
	}
	return &Normalizer{provider: provider, hostURL: hostURL}, nil
}

// Provider returns the normalized provider kind.
func (n *Normalizer) Provider() vcs.ProviderKind { return n.provider }

// HostURL returns the provider host the normalizer uses.
func (n *Normalizer) HostURL() string { return n.hostURL }

// RouteKey returns the platform-wide route key for an installation.
func (n *Normalizer) RouteKey(installationID string) string {
	return vcs.ProviderKey(n.provider, n.hostURL, installationID)
}

// Normalize maps one decoded GitHub webhook. handled is false for an event or
// action the gateway does not publish. An error means the payload is
// malformed.
func (n *Normalizer) Normalize(eventName string, payload githubWebhook, routeKey, deliveryID string) (vcs.WebhookEvent, bool, error) {
	event := vcs.WebhookEvent{
		DeliveryID:     deliveryID,
		RouteKey:       routeKey,
		Provider:       n.provider,
		HostURL:        n.hostURL,
		InstallationID: payload.installationID(),
	}
	switch eventName {
	case eventRepository:
		if payload.Repository == nil || payload.Repository.ID == 0 {
			return vcs.WebhookEvent{}, false, fmt.Errorf("webhook: a repository event needs a repository")
		}
		action, ok := repositoryAction(payload.Action)
		if !ok {
			return vcs.WebhookEvent{}, false, nil
		}
		event.Action = action
		event.OccurredAt = providerTime(payload.Repository.UpdatedAt)
		event.Repository = &vcs.WebhookRepository{
			ExternalID:    strconv.FormatInt(payload.Repository.ID, 10),
			FullName:      payload.Repository.FullName,
			DefaultBranch: payload.Repository.DefaultBranch,
			Visibility:    visibility(payload.Repository),
			CloneURL:      payload.Repository.CloneURL,
			WebURL:        payload.Repository.HTMLURL,
			Archived:      payload.Repository.Archived,
		}
		return event, true, nil
	case eventInstallation:
		if payload.Installation == nil || payload.Installation.ID == 0 {
			return vcs.WebhookEvent{}, false, fmt.Errorf("webhook: an installation event needs an installation")
		}
		action, ok := installationAction(payload.Action)
		if !ok {
			return vcs.WebhookEvent{}, false, nil
		}
		event.Action = action
		event.OccurredAt = providerTime(payload.Installation.UpdatedAt)
		return event, true, nil
	default:
		return vcs.WebhookEvent{}, false, nil
	}
}

func repositoryAction(action string) (vcs.WebhookAction, bool) {
	switch action {
	case "archived":
		return vcs.WebhookRepositoryArchived, true
	case "unarchived":
		return vcs.WebhookRepositoryUnarchived, true
	case "renamed", "transferred", "edited", "publicized", "privatized":
		return vcs.WebhookRepositoryChanged, true
	default:
		return "", false
	}
}

func installationAction(action string) (vcs.WebhookAction, bool) {
	switch action {
	case "suspend":
		return vcs.WebhookInstallationSuspended, true
	case "unsuspend":
		return vcs.WebhookInstallationUnsuspended, true
	case "deleted":
		return vcs.WebhookInstallationDeleted, true
	default:
		return "", false
	}
}

// visibility returns GitHub's visibility, falling back to the older boolean
// field when the payload omits it.
func visibility(repository *githubRepository) string {
	switch repository.Visibility {
	case "public", "private", "internal":
		return repository.Visibility
	}
	if repository.Private {
		return "private"
	}
	return "public"
}

// providerTime returns the provider's timestamp, or zero when the payload has
// none. The control plane orders a repository's or installation's events by
// this time, so the gateway never substitutes its own clock: a zero time
// tells the control plane the event cannot be ordered.
func providerTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Truncate(time.Microsecond)
}
