package vcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderKeyNormalizesTheHost(t *testing.T) {
	tests := []struct {
		name       string
		provider   ProviderKind
		hostURL    string
		externalID string
		want       string
	}{
		{
			name:       "absolute URL",
			provider:   ProviderGitHub,
			hostURL:    "https://github.com",
			externalID: "42",
			want:       "github:github.com:42",
		},
		{
			name:       "absolute URL with a trailing slash",
			provider:   ProviderGitHub,
			hostURL:    "https://github.com/",
			externalID: "42",
			want:       "github:github.com:42",
		},
		{
			name:       "bare host",
			provider:   ProviderGitHub,
			hostURL:    "github.com",
			externalID: "42",
			want:       "github:github.com:42",
		},
		{
			name:       "bare host with a trailing slash",
			provider:   ProviderGitLab,
			hostURL:    "gitlab.example.com/",
			externalID: "7",
			want:       "gitlab:gitlab.example.com:7",
		},
		{
			name:       "host with a port and path",
			provider:   ProviderGitea,
			hostURL:    "https://gitea.example.com:8443/acme",
			externalID: "7",
			want:       "gitea:gitea.example.com:8443:7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ProviderKey(tt.provider, tt.hostURL, tt.externalID))
		})
	}
}
