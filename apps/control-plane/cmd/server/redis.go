package server

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/redis/go-redis/v9"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
)

// openRedis returns a client for the control plane's Redis connection. It does
// not connect: the relay reports an unreachable server and retries, so the API
// stays available while Redis is down.
func openRedis(cfg config.Redis) (*redis.Client, error) {
	opts, err := redisOptions(cfg)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}

// redisOptions builds the client settings from the configuration, reading the
// password file and the CA that TLS verifies the server against.
func redisOptions(cfg config.Redis) (*redis.Options, error) {
	password, err := cfg.Password()
	if err != nil {
		return nil, err
	}
	opts := &redis.Options{
		Addr:        cfg.Address,
		Username:    cfg.Username,
		Password:    password.Reveal(),
		DB:          cfg.Database,
		DialTimeout: cfg.DialTimeout,
	}
	opts.TLSConfig, err = redisTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	return opts, nil
}

// redisTLSConfig returns the TLS settings for the connection, or nil when TLS
// is off.
func redisTLSConfig(cfg config.Redis) (*tls.Config, error) {
	if !cfg.TLS {
		return nil, nil
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("redis: load system certificates: %w", err)
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("redis: read CA file: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("redis: CA file %s has no PEM certificates", cfg.CAFile)
		}
	}
	return &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
}
