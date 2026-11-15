package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func snapshotVariable(name string, source VariableSource, values ...SnapshotValue) SnapshotVariable {
	return SnapshotVariable{Name: name, Source: source, Kind: VariableKindText, Values: values}
}

// The most specific matching value wins: a Target before an Environment
// before a Channel, and on equal scope the Application's before its Project's.
func TestResolveVariables(t *testing.T) {
	deployment := DeploymentScope{TargetID: "tgt", EnvironmentID: "prod", ChannelID: "hotfix"}
	value := func(v string, envs, targets, channels []string) SnapshotValue {
		return SnapshotValue{Value: v, Scope: scoped(envs, targets, channels)}
	}
	unscoped := value("unscoped", nil, nil, nil)
	channel := value("channel", nil, nil, []string{"hotfix"})
	environment := value("environment", []string{"staging", "prod"}, nil, nil)
	environmentAndChannel := value("environment+channel", []string{"prod"}, nil, []string{"hotfix"})
	target := value("target", nil, []string{"tgt"}, nil)
	elsewhere := value("elsewhere", []string{"staging"}, nil, nil)
	otherChannel := value("other channel", []string{"prod"}, nil, []string{"mainline"})

	tests := []struct {
		name   string
		values []SnapshotValue
		winner string
		beaten []string
	}{
		{"only an unscoped value", []SnapshotValue{unscoped}, "unscoped", nil},
		{"a channel beats unscoped", []SnapshotValue{unscoped, channel}, "channel", []string{"unscoped"}},
		{"an environment beats a channel", []SnapshotValue{channel, environment}, "environment", []string{"channel"}},
		{"scoped twice beats scoped once", []SnapshotValue{environment, environmentAndChannel, channel}, "environment+channel", []string{"environment", "channel"}},
		{"a target beats an environment and a channel together", []SnapshotValue{environmentAndChannel, target}, "target", []string{"environment+channel"}},
		{"a value for another environment does not match", []SnapshotValue{elsewhere, unscoped}, "unscoped", nil},
		{"every dimension must match", []SnapshotValue{otherChannel, unscoped}, "unscoped", nil},
		{"nothing matches", []SnapshotValue{elsewhere}, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveVariables([]SnapshotVariable{snapshotVariable("X", VariableSourceApplication, tt.values...)}, deployment)
			require.Len(t, got, 1)
			if tt.winner == "" {
				assert.Nil(t, got[0].Winner)
				return
			}
			require.NotNil(t, got[0].Winner)
			assert.Equal(t, tt.winner, got[0].Winner.Value)
			var beaten []string
			for _, b := range got[0].Beaten {
				beaten = append(beaten, b.Value)
			}
			assert.Equal(t, tt.beaten, beaten)
		})
	}

	t.Run("an application's variable and the project's it shadows are one", func(t *testing.T) {
		got := ResolveVariables([]SnapshotVariable{
			snapshotVariable("database.host", VariableSourceApplication, value("app", nil, nil, nil)),
			snapshotVariable("Database.Host", VariableSourceProject, value("project", nil, nil, nil), value("project prod", []string{"prod"}, nil, nil)),
			snapshotVariable("Api.Url", VariableSourceProject, value("api", nil, nil, nil)),
		}, deployment)
		require.Len(t, got, 2, "by name")
		assert.Equal(t, "Api.Url", got[0].Name)
		assert.Equal(t, "database.host", got[1].Name, "the application's spelling")
		assert.Equal(t, ResolvedValue{Value: "project prod", Scope: scoped([]string{"prod"}, nil, nil), Source: VariableSourceProject}, *got[1].Winner,
			"the project's more specific value beats the application's unscoped one")
		assert.Equal(t, []string{"app", "project"}, []string{got[1].Beaten[0].Value, got[1].Beaten[1].Value},
			"on equal scope the application's value beats the project's")
	})
}

func TestCheckBindings(t *testing.T) {
	resolve := func(variables ...SnapshotVariable) []ResolvedVariable {
		return ResolveVariables(variables, DeploymentScope{EnvironmentID: "prod"})
	}
	text := func(name, value string) SnapshotVariable {
		return snapshotVariable(name, VariableSourceApplication, SnapshotValue{Value: value})
	}

	assert.NoError(t, CheckBindings(resolve(
		text("Database.Url", "postgres://#{Database.Host}/#{Ivaldi.Environment.Slug}"),
		text("Database.Host", "db.internal"),
	)))

	tests := []struct {
		name      string
		variables []SnapshotVariable
		reason    string
	}{
		{"an undefined name", []SnapshotVariable{text("A", "#{Missing}")}, "A binds #{Missing}"},
		{"another spelling of a name", []SnapshotVariable{text("A", "#{database.host}"), text("Database.Host", "db")}, "#{database.host}"},
		{"a variable with no value for the deployment", []SnapshotVariable{
			text("A", "#{B}"),
			snapshotVariable("B", VariableSourceApplication, SnapshotValue{Value: "b", Scope: scoped([]string{"staging"}, nil, nil)}),
		}, "A binds #{B}"},
		{"a cycle", []SnapshotVariable{text("A", "#{B}"), text("B", "#{C}"), text("C", "#{A}")}, "A → B → C → A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckBindings(resolve(tt.variables...))
			require.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), tt.reason)
		})
	}
}

func TestSecretsForADeployment(t *testing.T) {
	const kubernetes, vault = "sst_01BX5ZZKBKACTAV9WEVGEMMVRZ", "sst_01BX5ZZKBKACTAV9WEVGEMMVR0"
	stores := map[string]SecretStoreRef{
		kubernetes: {ID: kubernetes, DisplayName: "In-cluster", Settings: StoreSettings{ID: kubernetes, Kind: StoreKubernetes, Kubernetes: &KubernetesStore{}}},
		vault: {ID: vault, DisplayName: "Vault", EnvironmentIDs: []string{"staging"},
			Settings: StoreSettings{ID: vault, Kind: StoreVault, Vault: &VaultStore{Address: "https://vault.example.com", Mount: "kv", AuthRole: "ivaldi"}}},
	}
	secret := func(name, store, path string, deliverAs VariableDeliverAs, valuesPath *string) SnapshotVariable {
		return SnapshotVariable{
			Name: name, Source: VariableSourceApplication, Kind: VariableKindSecret, DeliverAs: &deliverAs, ValuesPath: valuesPath,
			Values: []SnapshotValue{{Secret: &SecretReference{StoreID: store, Path: path, Field: "password", Version: "7"}}},
		}
	}
	resolve := func(variables ...SnapshotVariable) []ResolvedVariable {
		return ResolveVariables(variables, DeploymentScope{EnvironmentID: "prod"})
	}
	password := secret("Database.Password", kubernetes, "db-#{Region}", VariableDeliverAsSecret, nil)
	region := snapshotVariable("Region", VariableSourceProject, SnapshotValue{Value: "eu"})

	t.Run("the agent is sent the reference and how to reach its store", func(t *testing.T) {
		resolved := resolve(password, region, secret("Api.Key", kubernetes, "api", VariableDeliverAsValue, new("api.key")))
		require.NoError(t, CheckBindings(resolved))
		require.NoError(t, CheckSecretStores(resolved, stores, "prod", []string{"kubernetes"}))
		assert.Equal(t, []string{kubernetes}, SecretStoreIDs(resolved))
		assert.Equal(t, []AssignmentSecret{
			{Name: "Api.Key", Source: VariableSourceApplication, ValuesPath: "api.key", DeliverAs: VariableDeliverAsValue,
				Store: stores[kubernetes].Settings, Ref: SecretRef{Path: "api", Field: "password", Version: "7"}},
			{Name: "Database.Password", Source: VariableSourceApplication, DeliverAs: VariableDeliverAsSecret,
				Store: stores[kubernetes].Settings, Ref: SecretRef{Path: "db-#{Region}", Field: "password", Version: "7"}},
		}, AssignmentSecrets(resolved, stores))
		assert.Equal(t, []AssignmentVariable{{Name: "Region", Source: VariableSourceProject, Kind: VariableKindText, Value: "eu"}},
			AssignmentVariables(resolved), "a secret is never sent as a variable")
	})

	t.Run("a path may bind only what has a value", func(t *testing.T) {
		err := CheckBindings(resolve(password))
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "the path of secret Database.Password binds #{Region}")
	})

	refusals := []struct {
		name   string
		stores map[string]SecretStoreRef
		store  string
		kinds  []string
		reason string
	}{
		{"a store not allowed for the environment", stores, vault, []string{"vault"}, "not allowed for this environment"},
		{"a kind the agent cannot read", stores, kubernetes, []string{"vault"}, "cannot read kubernetes secret stores"},
		{"an archived store", map[string]SecretStoreRef{kubernetes: {DisplayName: "In-cluster", Archived: true}}, kubernetes, []string{"kubernetes"}, "is archived"},
		{"a store that is gone", map[string]SecretStoreRef{}, kubernetes, []string{"kubernetes"}, "no longer exists"},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckSecretStores(resolve(secret("Api.Key", tt.store, "api", VariableDeliverAsSecret, nil)), tt.stores, "prod", tt.kinds)
			require.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), tt.reason)
		})
	}
}
