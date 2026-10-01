package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := `# comment
FOO_TEST_VAR=hello
BAR_TEST_VAR="quoted value"
BAZ_TEST_VAR='single quoted'
export EXPORTED_VAR=exported

`
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Clean up env vars after test.
	for _, key := range []string{"FOO_TEST_VAR", "BAR_TEST_VAR", "BAZ_TEST_VAR", "EXPORTED_VAR"} {
		os.Unsetenv(key)
		defer os.Unsetenv(key)
	}

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	tests := []struct {
		key  string
		want string
	}{
		{"FOO_TEST_VAR", "hello"},
		{"BAR_TEST_VAR", "quoted value"},
		{"BAZ_TEST_VAR", "single quoted"},
		{"EXPORTED_VAR", "exported"},
	}

	for _, tt := range tests {
		got := os.Getenv(tt.key)
		if got != tt.want {
			t.Errorf("%s = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestLoadDotEnv_NoOverwrite(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	if err := os.WriteFile(envFile, []byte("EXISTING_VAR=from_file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("EXISTING_VAR", "from_env")
	defer os.Unsetenv("EXISTING_VAR")

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	if got := os.Getenv("EXISTING_VAR"); got != "from_env" {
		t.Errorf("EXISTING_VAR = %q, want %q (should not overwrite)", got, "from_env")
	}
}

func TestLoadDotEnv_MissingFile(t *testing.T) {
	if err := LoadDotEnv("/nonexistent/.env"); err != nil {
		t.Errorf("expected missing file to be silently skipped, got: %v", err)
	}
}

func TestParseDotEnvLine(t *testing.T) {
	tests := []struct {
		line    string
		key     string
		value   string
		wantOK  bool
	}{
		{"KEY=value", "KEY", "value", true},
		{"KEY=", "KEY", "", true},
		{`KEY="quoted"`, "KEY", "quoted", true},
		{`KEY='single'`, "KEY", "single", true},
		{"KEY = spaced", "KEY", "spaced", true},
		{"no_equals", "", "", false},
		{"=no_key", "", "", false},
	}

	for _, tt := range tests {
		key, value, ok := parseDotEnvLine(tt.line)
		if ok != tt.wantOK || key != tt.key || value != tt.value {
			t.Errorf("parseDotEnvLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.line, key, value, ok, tt.key, tt.value, tt.wantOK)
		}
	}
}

func TestParseWithDotEnvPaths(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	if err := os.WriteFile(envFile, []byte("DOTENV_PARSE_TEST=works\n"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("DOTENV_PARSE_TEST")
	defer os.Unsetenv("DOTENV_PARSE_TEST")

	type cfg struct {
		Val string `env:"DOTENV_PARSE_TEST"`
	}

	var c cfg
	err := Parse(&c, Options{DotEnvPaths: []string{envFile}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if c.Val != "works" {
		t.Errorf("Val = %q, want %q", c.Val, "works")
	}
}
