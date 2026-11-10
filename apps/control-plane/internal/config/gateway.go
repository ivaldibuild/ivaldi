package config

import (
	"fmt"
	"os"
	"strings"
)

// Gateway configures the control plane's side of the integration gateway.
type Gateway struct {
	// WebhookRouteLookupTokenFile holds the deployment token the gateway
	// presents when it resolves a webhook route. Leaving it empty disables
	// the route lookup endpoint.
	WebhookRouteLookupTokenFile string `yaml:"webhook_route_lookup_token_file" env:"IVALDI_GATEWAY_WEBHOOK_ROUTE_LOOKUP_TOKEN_FILE"`
}

// WebhookRouteLookupToken reads the deployment lookup token, or returns ""
// when no file is configured.
func (g Gateway) WebhookRouteLookupToken() (Secret, error) {
	if g.WebhookRouteLookupTokenFile == "" {
		return "", nil
	}
	b, err := os.ReadFile(g.WebhookRouteLookupTokenFile)
	if err != nil {
		return "", fmt.Errorf("gateway: read the webhook route lookup token: %w", err)
	}
	token := strings.TrimRight(string(b), "\r\n")
	if token == "" {
		return "", fmt.Errorf("gateway: the webhook route lookup token file is empty")
	}
	return Secret(token), nil
}
