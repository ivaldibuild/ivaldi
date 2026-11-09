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

const webhookSchemaPath = "../../schemas/version-control-webhook.v1.json"

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
