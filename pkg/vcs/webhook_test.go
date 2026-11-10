package vcs

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	webhookSchemaPath = "../../schemas/version-control-webhook.v1.json"
	routeSchemaPath   = "../../schemas/version-control-webhook-route.v1.json"
)

type schemaObject struct {
	Properties map[string]struct {
		Enum []string `json:"enum"`
	} `json:"properties"`
}

// TestWebhookEventMatchesItsSchema keeps the published contract and the Go
// types in step: the same JSON keys, and the same values for enums.
func TestWebhookEventMatchesItsSchema(t *testing.T) {
	raw, err := os.ReadFile(webhookSchemaPath)
	require.NoError(t, err)
	var schema struct {
		schemaObject
		Defs struct {
			Repository schemaObject `json:"repository"`
		} `json:"$defs"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))

	assert.ElementsMatch(t, jsonKeys(WebhookEvent{}), keys(schema.Properties))
	assert.ElementsMatch(t, jsonKeys(WebhookRepository{}), keys(schema.Defs.Repository.Properties))

	assert.ElementsMatch(t, []string{
		string(ProviderGitHub), string(ProviderGitLab), string(ProviderBitbucket),
		string(ProviderGitea), string(ProviderAzureDevOps),
	}, schema.Properties["provider"].Enum)
	assert.ElementsMatch(t, []string{
		string(WebhookRepositoryChanged), string(WebhookRepositoryArchived),
		string(WebhookRepositoryUnarchived), string(WebhookInstallationSuspended),
		string(WebhookInstallationUnsuspended), string(WebhookInstallationDeleted),
	}, schema.Properties["action"].Enum)
}

// TestWebhookRouteMatchesItsSchema keeps the route lookup's contract and its
// Go type in step.
func TestWebhookRouteMatchesItsSchema(t *testing.T) {
	raw, err := os.ReadFile(routeSchemaPath)
	require.NoError(t, err)
	var schema schemaObject
	require.NoError(t, json.Unmarshal(raw, &schema))

	assert.ElementsMatch(t, jsonKeys(WebhookRoute{}), keys(schema.Properties))
	assert.ElementsMatch(t, []string{
		string(ProviderGitHub), string(ProviderGitLab), string(ProviderBitbucket),
		string(ProviderGitea), string(ProviderAzureDevOps),
	}, schema.Properties["provider"].Enum)
}

// TestProviderEventWrappersKeepTheWebhookShape keeps the two event wrappers
// flat: embedding WebhookEvent must not nest its fields under a key.
func TestProviderEventWrappersKeepTheWebhookShape(t *testing.T) {
	event := WebhookEvent{
		DeliveryID: "delivery-1",
		RouteKey:   "github:github.com:42",
		Provider:   ProviderGitHub,
		HostURL:    "https://github.com",
		Action:     WebhookRepositoryChanged,
		Repository: &WebhookRepository{ExternalID: "99", FullName: "acme/api"},
	}
	want := objectKeys(t, event)

	for name, wrapper := range map[string]any{
		"repository changed":   ProviderRepositoryChanged{event},
		"installation changed": ProviderInstallationChanged{event},
	} {
		t.Run(name, func(t *testing.T) {
			assert.ElementsMatch(t, want, objectKeys(t, wrapper))
		})
	}

	assert.Equal(t, EventProviderRepositoryChanged, ProviderRepositoryChanged{}.EventName())
	assert.Equal(t, EventProviderInstallationChanged, ProviderInstallationChanged{}.EventName())
}

// objectKeys returns the top-level JSON keys of v, after marshaling it.
func objectKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var object map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &object))
	return keys(object)
}

func jsonKeys(v any) []string {
	t := reflect.TypeOf(v)
	out := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
