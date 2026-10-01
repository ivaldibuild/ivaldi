package env

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv reads .env files and sets variables into the process environment.
// Variables that are already set in the environment are NOT overwritten,
// so real env vars always take precedence over .env file values.
// Files that do not exist are silently skipped.
func LoadDotEnv(paths ...string) error {
	for _, path := range paths {
		if err := loadDotEnvFile(path); err != nil {
			return err
		}
	}
	return nil
}

func loadDotEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("env: opening .env file %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || line[0] == '#' {
			continue
		}

		// Skip export prefix.
		line = strings.TrimPrefix(line, "export ")

		key, value, ok := parseDotEnvLine(line)
		if !ok {
			return fmt.Errorf("env: %s:%d: invalid line %q", path, lineNum, line)
		}

		// Only set if not already present in the environment.
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}

	return scanner.Err()
}

// parseDotEnvLine parses a single KEY=VALUE line.
// It handles unquoted, single-quoted, and double-quoted values.
func parseDotEnvLine(line string) (key, value string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 1 {
		return "", "", false
	}

	key = strings.TrimSpace(line[:eq])
	raw := strings.TrimSpace(line[eq+1:])

	// Unquote single or double quotes.
	if len(raw) >= 2 {
		if (raw[0] == '"' && raw[len(raw)-1] == '"') ||
			(raw[0] == '\'' && raw[len(raw)-1] == '\'') {
			raw = raw[1 : len(raw)-1]
		}
	}

	return key, raw, true
}
