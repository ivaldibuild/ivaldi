package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var system = map[string]string{
	"Ivaldi.Environment.Slug": "production",
	"Ivaldi.Target.Namespace": "payments-prod",
}

func doc(name, content string) Document { return Document{Name: name, Content: []byte(content)} }

func TestBuild(t *testing.T) {
	tests := []struct {
		name    string
		in      Input
		want    map[string]any // compared without global
		wantErr string
	}{
		{
			name: "overlays deep-merge maps and replace lists, in order",
			in: Input{Overlays: []Document{
				doc("values.yaml", "replicas: 1\nresources: {cpu: 100m, memory: 128Mi}\nhosts: [a, b]\n"),
				doc("values.production.yaml", "replicas: 3\nresources: {cpu: 500m}\nhosts: [c]\n"),
			}},
			want: map[string]any{
				"replicas":  float64(3),
				"resources": map[string]any{"cpu": "500m", "memory": "128Mi"},
				"hosts":     []any{"c"},
			},
		},
		{
			name: "null replaces an earlier value and is passed on",
			in: Input{Overlays: []Document{
				doc("a.yaml", "probe: {path: /health}\n"),
				doc("b.yaml", "probe: null\n"),
			}},
			want: map[string]any{"probe": nil},
		},
		{
			name: "bindings are substituted in values, never keys",
			in: Input{Overlays: []Document{
				doc("a.yaml", "\"#{Ivaldi.Environment.Slug}\": kept\nhost: api.#{Ivaldi.Environment.Slug}.example.com\nliteral: \"##{not.bound}\"\nlist: [\"#{Ivaldi.Target.Namespace}\"]\n"),
			}},
			want: map[string]any{
				"#{Ivaldi.Environment.Slug}": "kept",
				"host":                       "api.production.example.com",
				"literal":                    "#{not.bound}",
				"list":                       []any{"payments-prod"},
			},
		},
		{
			name: "variables bind each other and beat an overlay at their values path",
			in: Input{
				Overlays: []Document{doc("a.yaml", "db: {host: overlay, port: 5432}\n")},
				Variables: []Variable{
					{Name: "DbHost", ValuesPath: "db.host", Value: "#{Prefix}-db"},
					{Name: "Prefix", Value: "#{Ivaldi.Environment.Slug}"},
				},
			},
			want: map[string]any{"db": map[string]any{"host": "production-db", "port": float64(5432)}},
		},
		{
			name:    "an undefined binding fails by name",
			in:      Input{Overlays: []Document{doc("a.yaml", "host: \"#{Missing}\"\n")}},
			wantErr: "overlay a.yaml: host: #{Missing} is not defined",
		},
		{
			name: "a cycle fails by name",
			in: Input{Variables: []Variable{
				{Name: "A", Value: "#{B}"}, {Name: "B", Value: "#{A}"},
			}},
			wantErr: "cycle",
		},
		{
			name: "overlapping values paths fail naming both",
			in: Input{Variables: []Variable{
				{Name: "Db", ValuesPath: "db", Value: "x"}, {Name: "DbHost", ValuesPath: "db.host", Value: "y"},
			}},
			wantErr: "variables Db and DbHost write overlapping values paths",
		},
		{
			name: "paths sharing a prefix that is not a key do not overlap",
			in: Input{Variables: []Variable{
				{Name: "Db", ValuesPath: "db", Value: "x"}, {Name: "Dbx", ValuesPath: "dbx", Value: "y"},
			}},
			want: map[string]any{"db": "x", "dbx": "y"},
		},
		{
			name:    "a values path through a scalar fails",
			in:      Input{Overlays: []Document{doc("a.yaml", "db: postgres\n")}, Variables: []Variable{{Name: "H", ValuesPath: "db.host", Value: "x"}}},
			wantErr: "db is not a map",
		},
		{
			name:    "an overlay must be a mapping",
			in:      Input{Overlays: []Document{doc("a.yaml", "- one\n")}},
			wantErr: "overlay a.yaml must be a mapping",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.System = system
			got, err := Build(t.Context(), tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, got.Values)
				return
			}
			require.NoError(t, err)
			delete(got.Values, "global")
			assert.Equal(t, tt.want, got.Values)
		})
	}
}

func TestBuild_AddsIvaldisGeneratedValues(t *testing.T) {
	got, err := Build(t.Context(), Input{
		Overlays: []Document{doc("a.yaml", "global: {imageRegistry: mirror.example.com}\n")},
		Generated: Generated{
			Application: "app_1", Release: "rel_1", Deployment: "dep_1", Version: "1.4.0",
			Artifacts: map[string]Image{"api": {Repository: "ghcr.io/acme/api", Digest: "sha256:abc"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"imageRegistry": "mirror.example.com",
		"ivaldi": map[string]any{
			"application": "app_1", "release": "rel_1", "deployment": "dep_1", "version": "1.4.0",
			"artifacts": map[string]any{"api": map[string]any{
				"repository": "ghcr.io/acme/api", "digest": "sha256:abc", "image": "ghcr.io/acme/api@sha256:abc",
			}},
		},
	}, got.Values["global"])
}

func TestBuild_GeneratedValuesReplaceWhatOverlaysPutThere(t *testing.T) {
	got, err := Build(t.Context(), Input{
		Overlays:  []Document{doc("a.yaml", "global:\n  imageRegistry: mirror.example.com\n  ivaldi:\n    artifacts: {old: {image: stale@sha256:old}}\n    extra: chart-controlled\n")},
		Generated: Generated{Application: "app_1", Artifacts: map[string]Image{"api": {Repository: "ghcr.io/acme/api", Digest: "sha256:abc"}}},
	})
	require.NoError(t, err)
	global := got.Values["global"].(map[string]any)
	assert.Equal(t, "mirror.example.com", global["imageRegistry"], "other global keys are kept")
	ivaldi := global["ivaldi"].(map[string]any)
	assert.NotContains(t, ivaldi, "extra")
	assert.Equal(t, []string{"api"}, keys(ivaldi["artifacts"].(map[string]any)), "only this Release's artifacts")

	_, err = Build(t.Context(), Input{Variables: []Variable{{Name: "Hack", ValuesPath: "global.ivaldi.release", Value: "x"}}})
	require.ErrorContains(t, err, "reserved for Ivaldi's generated values")
	_, err = Build(t.Context(), Input{Variables: []Variable{{Name: "Ok", ValuesPath: "global.ivaldiExtra", Value: "x"}}})
	require.NoError(t, err, "a sibling key that only shares the prefix is not reserved")
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBuild_Secrets(t *testing.T) {
	got, err := Build(t.Context(), Input{
		Overlays: []Document{doc("a.yaml", "smtp: {password: \"#{SMTP_PASSWORD}\"}\n")},
		Secrets: []Secret{
			{Name: "DB_PASSWORD", Value: "hunter22"},
			{Name: "SMTP_PASSWORD", Value: "mailpass"},
			{Name: "API_KEY", Value: "key-123", ValuesPath: "api.key", AsValue: true},
		},
		SecretName: "ivaldi-app-1-dep-1",
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"password": "mailpass"}, got.Values["smtp"], "an overlay may bind a secret")
	assert.Equal(t, map[string]any{"key": "key-123"}, got.Values["api"], "a secret delivered as a value is written at its path")
	assert.Equal(t, map[string]any{"name": "ivaldi-app-1-dep-1"}, got.Values["global"].(map[string]any)["ivaldi"].(map[string]any)["secrets"])
	assert.Equal(t, []string{"API_KEY", "SMTP_PASSWORD"}, got.SecretsInValues, "the secrets now in the values, for the warning")

	_, err = Build(t.Context(), Input{
		Variables: []Variable{{Name: "Url", Value: "postgres://#{DB_PASSWORD}@db"}},
		Secrets:   []Secret{{Name: "DB_PASSWORD", Value: "hunter22"}},
	})
	require.ErrorContains(t, err, "#{DB_PASSWORD} is not defined", "a text variable may not bind a secret")

	_, err = Build(t.Context(), Input{
		Variables: []Variable{{Name: "Db", ValuesPath: "db", Value: "x"}},
		Secrets:   []Secret{{Name: "DB_PASSWORD", Value: "y", ValuesPath: "db.password", AsValue: true}},
	})
	require.ErrorContains(t, err, "overlapping values paths")
}
