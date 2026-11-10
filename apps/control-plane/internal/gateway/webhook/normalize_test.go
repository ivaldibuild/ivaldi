package webhook

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

func testNormalizer(t *testing.T) *Normalizer {
	t.Helper()
	normalizer, err := NewNormalizer(vcs.ProviderGitHub, "https://github.com")
	require.NoError(t, err)
	return normalizer
}

func repositoryPayload(action string) githubWebhook {
	return githubWebhook{
		Action: action,
		Installation: &githubInstallation{
			ID: 42, UpdatedAt: time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC),
		},
		Repository: &githubRepository{
			ID: 99, FullName: "acme/checkout", DefaultBranch: "main", Visibility: "private",
			CloneURL: "https://github.com/acme/checkout.git", HTMLURL: "https://github.com/acme/checkout",
			Archived:  action == "archived",
			UpdatedAt: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC),
		},
	}
}

func TestNormalizerMapsRepositoryActions(t *testing.T) {
	normalizer := testNormalizer(t)
	tests := map[string]vcs.WebhookAction{
		"renamed":     vcs.WebhookRepositoryChanged,
		"transferred": vcs.WebhookRepositoryChanged,
		"edited":      vcs.WebhookRepositoryChanged,
		"publicized":  vcs.WebhookRepositoryChanged,
		"privatized":  vcs.WebhookRepositoryChanged,
		"archived":    vcs.WebhookRepositoryArchived,
		"unarchived":  vcs.WebhookRepositoryUnarchived,
	}
	for action, want := range tests {
		t.Run(action, func(t *testing.T) {
			event, handled, err := normalizer.Normalize(
				eventRepository, repositoryPayload(action), "github:github.com:42", "delivery-1")
			require.NoError(t, err)
			require.True(t, handled)
			assert.Equal(t, want, event.Action)
			assert.Equal(t, "github:github.com:42", event.RouteKey)
			assert.Equal(t, "42", event.InstallationID)
			require.NotNil(t, event.Repository)
			assert.Equal(t, "99", event.Repository.ExternalID)
			assert.Equal(t, "acme/checkout", event.Repository.FullName)
			assert.Equal(t, "main", event.Repository.DefaultBranch)
			assert.Equal(t, "private", event.Repository.Visibility)
			assert.Equal(t, "https://github.com/acme/checkout", event.Repository.WebURL)
			assert.Equal(t, time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), event.OccurredAt)
		})
	}
}

func TestNormalizerIgnoresUnhandledRepositoryActions(t *testing.T) {
	normalizer := testNormalizer(t)
	_, handled, err := normalizer.Normalize(
		eventRepository, repositoryPayload("created"), "github:github.com:42", "delivery-1")
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestNormalizerMapsInstallationActions(t *testing.T) {
	normalizer := testNormalizer(t)
	tests := map[string]vcs.WebhookAction{
		"suspend":   vcs.WebhookInstallationSuspended,
		"unsuspend": vcs.WebhookInstallationUnsuspended,
		"deleted":   vcs.WebhookInstallationDeleted,
	}
	for action, want := range tests {
		t.Run(action, func(t *testing.T) {
			event, handled, err := normalizer.Normalize(eventInstallation, githubWebhook{
				Action: action,
				Installation: &githubInstallation{
					ID: 42, UpdatedAt: time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC),
				},
			}, "github:github.com:42", "delivery-1")
			require.NoError(t, err)
			require.True(t, handled)
			assert.Equal(t, want, event.Action)
			assert.Equal(t, "42", event.InstallationID)
			assert.Nil(t, event.Repository)
			assert.Equal(t, time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC), event.OccurredAt)
		})
	}
}

func TestNormalizerIgnoresUnhandledInstallationActions(t *testing.T) {
	normalizer := testNormalizer(t)
	_, handled, err := normalizer.Normalize(eventInstallation, githubWebhook{
		Action:       "created",
		Installation: &githubInstallation{ID: 42},
	}, "github:github.com:42", "delivery-1")
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestNormalizerFallsBackToTheBooleanVisibility(t *testing.T) {
	normalizer := testNormalizer(t)
	payload := repositoryPayload("renamed")
	payload.Repository.Visibility = ""
	payload.Repository.Private = true
	event, _, err := normalizer.Normalize(eventRepository, payload, "github:github.com:42", "delivery-1")
	require.NoError(t, err)
	assert.Equal(t, "private", event.Repository.Visibility)
}

func TestNormalizerDefaultsTheOccurredAt(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	normalizer := testNormalizer(t)
	payload := repositoryPayload("renamed")
	payload.Repository.UpdatedAt = time.Time{}

	event, _, err := normalizer.Normalize(eventRepository, payload, "github:github.com:42", "delivery-1")
	require.NoError(t, err)
	assert.Equal(t, now, event.OccurredAt)
}

func TestNormalizerRejectsMalformedPayloads(t *testing.T) {
	normalizer := testNormalizer(t)
	_, _, err := normalizer.Normalize(eventRepository, githubWebhook{
		Action: "renamed", Installation: &githubInstallation{ID: 42},
	}, "github:github.com:42", "delivery-1")
	assert.Error(t, err)

	_, _, err = normalizer.Normalize(eventInstallation, githubWebhook{
		Action: "suspend",
	}, "github:github.com:42", "delivery-1")
	assert.Error(t, err)
}

func TestNewNormalizerRequiresAProviderAndHost(t *testing.T) {
	_, err := NewNormalizer("", "https://github.com")
	assert.Error(t, err)
	_, err = NewNormalizer(vcs.ProviderGitHub, "")
	assert.Error(t, err)
}
