package config

import (
	"fmt"
	"time"
)

// GetDatabaseURL returns the database URL from the configuration.
func (c *Config) DatabaseURL() (string, error) {
	if c.Persistence == nil {
		return "", fmt.Errorf("persistence configuration is not set")
	}

	if c.Persistence.DatabaseURL != nil {
		return *c.Persistence.DatabaseURL, nil
	}

	if c.Persistence.DatabaseHost != nil && c.Persistence.DatabasePort != nil && c.Persistence.DatabaseUser != nil && c.Persistence.DatabasePassword != nil && c.Persistence.DatabaseName != nil {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			*c.Persistence.DatabaseUser,
			*c.Persistence.DatabasePassword,
			*c.Persistence.DatabaseHost,
			*c.Persistence.DatabasePort,
			*c.Persistence.DatabaseName,
			*c.Persistence.SSLMode,
		), nil
	}

	return "", fmt.Errorf("no database configuration is set. Please provide either a full database URL or all individual database components (host, port, user, password, name, sslmode).")
}

func (c *Config) DatabasePoolOptions() (maxPoolSize, minPoolSize int, connectionIdleTimeout, connectionTimeout time.Duration, err error) {
	if c.Persistence == nil {
		err = fmt.Errorf("persistence configuration is not set")
		return
	}

	maxPoolSize = c.Persistence.MaxPoolSize
	minPoolSize = c.Persistence.MinPoolSize
	connectionIdleTimeout = c.Persistence.ConnectionIdleTimeout
	connectionTimeout = c.Persistence.ConnectionTimeout

	return
}
