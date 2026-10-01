package env

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// mockSource returns a Source function backed by a map.
func mockSource(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

// --- Parse: basic types ---

func TestParse_BasicTypes(t *testing.T) {
	type Config struct {
		Host string `env:"HOST"`
		Port int    `env:"PORT"`
		TLS  bool   `env:"TLS"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"HOST": "localhost",
			"PORT": "8080",
			"TLS":  "true",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "localhost" {
		t.Errorf("Host: expected localhost, got %q", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: expected 8080, got %d", cfg.Port)
	}
	if !cfg.TLS {
		t.Error("TLS: expected true")
	}
}

// --- Parse: pointer types ---

func TestParse_PointerTypes(t *testing.T) {
	type Config struct {
		Port *int           `env:"PORT"`
		Name *string        `env:"NAME"`
		TTL  *time.Duration `env:"TTL"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"PORT": "9090",
			"NAME": "test",
			"TTL":  "15m",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port == nil || *cfg.Port != 9090 {
		t.Errorf("Port: expected *9090, got %v", cfg.Port)
	}
	if cfg.Name == nil || *cfg.Name != "test" {
		t.Errorf("Name: expected *test, got %v", cfg.Name)
	}
	if cfg.TTL == nil || *cfg.TTL != 15*time.Minute {
		t.Errorf("TTL: expected *15m, got %v", cfg.TTL)
	}
}

func TestParse_PointerNilWhenMissing(t *testing.T) {
	type Config struct {
		Port *int `env:"PORT"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != nil {
		t.Errorf("Port: expected nil, got %v", cfg.Port)
	}
}

// --- Parse: nested structs ---

func TestParse_NestedStructs(t *testing.T) {
	type DB struct {
		Host string `env:"DB_HOST"`
		Port int    `env:"DB_PORT"`
	}
	type Config struct {
		DB DB `yaml:"db"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"DB_HOST": "db.example.com",
			"DB_PORT": "5432",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.Host != "db.example.com" {
		t.Errorf("DB.Host: expected db.example.com, got %q", cfg.DB.Host)
	}
	if cfg.DB.Port != 5432 {
		t.Errorf("DB.Port: expected 5432, got %d", cfg.DB.Port)
	}
}

// --- Parse: pointer-to-struct allocation ---

func TestParse_PointerToStruct(t *testing.T) {
	type DB struct {
		Host string `env:"DB_HOST"`
	}
	type Config struct {
		DB *DB `yaml:"db"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"DB_HOST": "allocated",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB == nil {
		t.Fatal("DB: expected non-nil pointer")
	}
	if cfg.DB.Host != "allocated" {
		t.Errorf("DB.Host: expected allocated, got %q", cfg.DB.Host)
	}
}

// --- Parse: slice fields ---

func TestParse_SliceField(t *testing.T) {
	type Config struct {
		Channels []string `env:"CHANNELS"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"CHANNELS": "general, random, deploy",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"general", "random", "deploy"}
	if !reflect.DeepEqual(cfg.Channels, expected) {
		t.Errorf("Channels: expected %v, got %v", expected, cfg.Channels)
	}
}

// --- Parse: named string type ---

func TestParse_NamedStringType(t *testing.T) {
	type Role string
	type Config struct {
		Role *Role `env:"ROLE"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"ROLE": "admin",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Role == nil || *cfg.Role != "admin" {
		t.Errorf("Role: expected *admin, got %v", cfg.Role)
	}
}

// --- Parse: required ---

func TestParse_Required_Missing(t *testing.T) {
	type Config struct {
		Token string `env:"TOKEN,required"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{}),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var reqErr *RequiredError
	var aggErr *AggregateError
	if errors.As(err, &aggErr) {
		if len(aggErr.Errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(aggErr.Errors))
		}
		if !errors.As(aggErr.Errors[0], &reqErr) {
			t.Errorf("expected RequiredError, got %T", aggErr.Errors[0])
		}
	} else {
		t.Errorf("expected AggregateError, got %T", err)
	}
}

func TestParse_Required_Present(t *testing.T) {
	type Config struct {
		Token string `env:"TOKEN,required"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"TOKEN": "secret",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "secret" {
		t.Errorf("Token: expected secret, got %q", cfg.Token)
	}
}

// --- Parse: notEmpty ---

func TestParse_NotEmpty(t *testing.T) {
	type Config struct {
		Token string `env:"TOKEN,notEmpty"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"TOKEN": "",
		}),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Parse: envDefault ---

func TestParse_EnvDefault(t *testing.T) {
	type Config struct {
		Port int `env:"PORT" envDefault:"8080"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: expected 8080, got %d", cfg.Port)
	}
}

func TestParse_EnvDefault_OverriddenByEnv(t *testing.T) {
	type Config struct {
		Port int `env:"PORT" envDefault:"8080"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"PORT": "9090",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port: expected 9090, got %d", cfg.Port)
	}
}

// --- Parse: expand ---

func TestParse_Expand(t *testing.T) {
	type Config struct {
		URL string `env:"URL,expand"`
	}

	os.Setenv("EXPAND_HOST", "example.com")
	defer os.Unsetenv("EXPAND_HOST")

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"URL": "https://${EXPAND_HOST}/api",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://example.com/api" {
		t.Errorf("URL: expected https://example.com/api, got %q", cfg.URL)
	}
}

// --- Parse: prefix ---

func TestParse_Prefix(t *testing.T) {
	type Config struct {
		Port int `env:"PORT"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Prefix: "APP_",
		Source: mockSource(map[string]string{
			"APP_PORT": "3000",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port: expected 3000, got %d", cfg.Port)
	}
}

// --- Parse: skip fields without env tag ---

func TestParse_NoEnvTag(t *testing.T) {
	type Config struct {
		Name string `yaml:"name"`
		Port int    `env:"PORT"`
	}

	var cfg Config
	cfg.Name = "original"
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"PORT": "8080",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "original" {
		t.Errorf("Name: expected original, got %q", cfg.Name)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: expected 8080, got %d", cfg.Port)
	}
}

// --- Parse: unexported fields ---

func TestParse_UnexportedFields(t *testing.T) {
	type Config struct {
		secret string `env:"SECRET"` //nolint:unused
		Public string `env:"PUBLIC"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"SECRET": "hidden",
			"PUBLIC": "visible",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Public != "visible" {
		t.Errorf("Public: expected visible, got %q", cfg.Public)
	}
}

// --- Parse: interface{} fields are skipped ---

func TestParse_InterfaceField(t *testing.T) {
	type Config struct {
		Data any    `env:"DATA"`
		Name string `env:"NAME"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"DATA": "something",
			"NAME": "test",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Data != nil {
		t.Errorf("Data: expected nil, got %v", cfg.Data)
	}
	if cfg.Name != "test" {
		t.Errorf("Name: expected test, got %q", cfg.Name)
	}
}

// --- Parse: invalid input ---

func TestParse_NonPointer(t *testing.T) {
	type Config struct{}
	err := Parse(Config{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParse_NilPointer(t *testing.T) {
	err := Parse((*struct{})(nil))
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Parse: env overrides existing value ---

func TestParse_EnvOverridesExistingValue(t *testing.T) {
	type Config struct {
		Host string `env:"HOST"`
	}

	cfg := Config{Host: "from-yaml"}
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{
			"HOST": "from-env",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "from-env" {
		t.Errorf("Host: expected from-env, got %q", cfg.Host)
	}
}

func TestParse_NoEnvLeavesExistingValue(t *testing.T) {
	type Config struct {
		Host string `env:"HOST"`
	}

	cfg := Config{Host: "from-yaml"}
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "from-yaml" {
		t.Errorf("Host: expected from-yaml, got %q", cfg.Host)
	}
}

// --- Parse: aggregate errors ---

func TestParse_AggregateErrors(t *testing.T) {
	type Config struct {
		A string `env:"A,required"`
		B string `env:"B,required"`
		C string `env:"C,required"`
	}

	var cfg Config
	err := Parse(&cfg, Options{
		Source: mockSource(map[string]string{}),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var aggErr *AggregateError
	if !errors.As(err, &aggErr) {
		t.Fatalf("expected AggregateError, got %T", err)
	}
	if len(aggErr.Errors) != 3 {
		t.Errorf("expected 3 errors, got %d", len(aggErr.Errors))
	}
}

// --- LoadYAML ---

func TestLoadYAML(t *testing.T) {
	type Config struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	}

	yamlData := `
host: example.com
port: 5432
`
	var cfg Config
	err := LoadYAML(strings.NewReader(yamlData), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "example.com" {
		t.Errorf("Host: expected example.com, got %q", cfg.Host)
	}
	if cfg.Port != 5432 {
		t.Errorf("Port: expected 5432, got %d", cfg.Port)
	}
}

// --- Load: YAML then env override ---

func TestLoad_YAMLThenEnv(t *testing.T) {
	type Config struct {
		Host string `yaml:"host" env:"HOST"`
		Port int    `yaml:"port" env:"PORT"`
	}

	yamlData := `
host: from-yaml
port: 3000
`
	// Write temp YAML file.
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlData), 0644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	err := Load(&cfg, Options{
		YAMLPath: yamlPath,
		Source: mockSource(map[string]string{
			"PORT": "9090",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Host comes from YAML (no env override).
	if cfg.Host != "from-yaml" {
		t.Errorf("Host: expected from-yaml, got %q", cfg.Host)
	}
	// Port overridden by env.
	if cfg.Port != 9090 {
		t.Errorf("Port: expected 9090, got %d", cfg.Port)
	}
}

func TestLoad_EnvOnly(t *testing.T) {
	type Config struct {
		Host string `yaml:"host" env:"HOST"`
	}

	var cfg Config
	err := Load(&cfg, Options{
		Source: mockSource(map[string]string{
			"HOST": "env-only",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "env-only" {
		t.Errorf("Host: expected env-only, got %q", cfg.Host)
	}
}

// --- Load: full DispatchdConfig-like struct ---

func TestLoad_FullConfig(t *testing.T) {
	type Database struct {
		Host     string `yaml:"host" env:"DB_HOST"`
		Port     int    `yaml:"port" env:"DB_PORT"`
		User     string `yaml:"user" env:"DB_USER"`
		Password string `yaml:"password" env:"DB_PASSWORD"`
		Name     string `yaml:"name" env:"DB_NAME"`
	}

	type SecretManagerType string

	type SecretManager struct {
		Type *SecretManagerType `yaml:"type" env:"SM_TYPE"`
	}

	type SlackConfig struct {
		BotToken        string   `yaml:"bot_token" env:"SLACK_BOT_TOKEN"`
		SocketMode      bool     `yaml:"socket_mode" env:"SLACK_SOCKET_MODE"`
		AllowedChannels []string `yaml:"allowed_channels" env:"SLACK_CHANNELS"`
	}

	type Config struct {
		Port          *int          `yaml:"port" env:"PORT"`
		DatabaseUrl   *string       `yaml:"database_url" env:"DATABASE_URL"`
		Database      *Database     `yaml:"database"`
		SecretManager SecretManager `yaml:"secret_manager"`
		Slack         SlackConfig   `yaml:"slack"`
	}

	yamlData := `
port: 8080
database:
  host: localhost
  port: 5432
  user: dispatch
  password: secret
  name: dispatch_db
secret_manager:
  type: database
slack:
  bot_token: xoxb-yaml-token
  socket_mode: false
  allowed_channels:
    - C001
    - C002
`
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlData), 0644); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	err := Load(&cfg, Options{
		YAMLPath: yamlPath,
		Source: mockSource(map[string]string{
			"PORT":            "9090",
			"DB_HOST":         "prod-db.example.com",
			"SLACK_BOT_TOKEN": "xoxb-env-token",
			"SLACK_CHANNELS":  "C100,C200,C300",
			"SM_TYPE":         "vault",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Port overridden by env.
	if cfg.Port == nil || *cfg.Port != 9090 {
		t.Errorf("Port: expected *9090, got %v", cfg.Port)
	}

	// DatabaseUrl not set in YAML or env.
	if cfg.DatabaseUrl != nil {
		t.Errorf("DatabaseUrl: expected nil, got %v", cfg.DatabaseUrl)
	}

	// Database.Host overridden by env, Port from YAML.
	if cfg.Database == nil {
		t.Fatal("Database: expected non-nil")
	}
	if cfg.Database.Host != "prod-db.example.com" {
		t.Errorf("Database.Host: expected prod-db.example.com, got %q", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("Database.Port: expected 5432, got %d", cfg.Database.Port)
	}
	if cfg.Database.User != "dispatch" {
		t.Errorf("Database.User: expected dispatch, got %q", cfg.Database.User)
	}

	// SecretManager.Type overridden by env.
	vault := SecretManagerType("vault")
	if cfg.SecretManager.Type == nil || *cfg.SecretManager.Type != vault {
		t.Errorf("SecretManager.Type: expected *vault, got %v", cfg.SecretManager.Type)
	}

	// Slack.BotToken overridden by env.
	if cfg.Slack.BotToken != "xoxb-env-token" {
		t.Errorf("Slack.BotToken: expected xoxb-env-token, got %q", cfg.Slack.BotToken)
	}

	// Slack.SocketMode from YAML (no env override).
	if cfg.Slack.SocketMode {
		t.Error("Slack.SocketMode: expected false")
	}

	// Slack.AllowedChannels overridden by env.
	expectedChannels := []string{"C100", "C200", "C300"}
	if !reflect.DeepEqual(cfg.Slack.AllowedChannels, expectedChannels) {
		t.Errorf("Slack.AllowedChannels: expected %v, got %v", expectedChannels, cfg.Slack.AllowedChannels)
	}
}

// --- Parse: envDefault layering (default < YAML/caller < env) ---

func TestParse_EnvDefault_DoesNotClobberExistingValue(t *testing.T) {
	type Config struct {
		Port  int    `env:"PORT" envDefault:"8080"`
		Level string `env:"LEVEL" envDefault:"info"`
	}

	// Simulates a struct already populated from YAML before Parse overlays env.
	cfg := Config{Port: 3000, Level: "debug"}
	err := Parse(&cfg, Options{Source: mockSource(map[string]string{})})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 3000 {
		t.Errorf("Port: envDefault clobbered existing value: got %d, want 3000", cfg.Port)
	}
	if cfg.Level != "debug" {
		t.Errorf("Level: envDefault clobbered existing value: got %q, want %q", cfg.Level, "debug")
	}
}

func TestParse_EnvDefault_AppliesToZeroValue(t *testing.T) {
	type Config struct {
		Port  int    `env:"PORT" envDefault:"8080"`
		Level string `env:"LEVEL" envDefault:"info"`
	}

	var cfg Config
	err := Parse(&cfg, Options{Source: mockSource(map[string]string{})})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: got %d, want default 8080", cfg.Port)
	}
	if cfg.Level != "info" {
		t.Errorf("Level: got %q, want default %q", cfg.Level, "info")
	}
}

func TestParse_EnvDefault_EnvStillWinsOverExistingValue(t *testing.T) {
	type Config struct {
		Port int `env:"PORT" envDefault:"8080"`
	}

	cfg := Config{Port: 3000}
	err := Parse(&cfg, Options{Source: mockSource(map[string]string{"PORT": "4000"})})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 4000 {
		t.Errorf("Port: env must win over existing value: got %d, want 4000", cfg.Port)
	}
}
