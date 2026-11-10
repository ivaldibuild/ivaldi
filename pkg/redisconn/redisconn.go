// Package redisconn builds Redis clients from connection settings. The
// control plane and the gateway share it, so both read the password file and
// verify the server with the same TLS rules.
package redisconn

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config is one Redis connection's settings.
type Config struct {
	// Address is host:port.
	Address string
	// Username is the Redis ACL user.
	Username string
	// Password is the user's secret, already read from its file.
	Password string
	Database int
	// TLS turns on TLS, verified against CAFile, or the system roots when
	// CAFile is empty.
	TLS         bool
	CAFile      string
	DialTimeout time.Duration
}

// New returns a client. It does not connect: the caller reports an
// unreachable server when it first uses the client.
func New(c Config) (*redis.Client, error) {
	opts, err := Options(c)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opts), nil
}

// Options returns the client settings from c.
func Options(c Config) (*redis.Options, error) {
	opts := &redis.Options{
		Addr:        c.Address,
		Username:    c.Username,
		Password:    c.Password,
		DB:          c.Database,
		DialTimeout: c.DialTimeout,
	}
	tlsConfig, err := tlsConfig(c)
	if err != nil {
		return nil, err
	}
	opts.TLSConfig = tlsConfig
	return opts, nil
}

// tlsConfig returns the TLS settings for the connection, or nil when TLS is
// off.
func tlsConfig(c Config) (*tls.Config, error) {
	if !c.TLS {
		return nil, nil
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("redis: load system certificates: %w", err)
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("redis: read CA file: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("redis: CA file %s has no PEM certificates", c.CAFile)
		}
	}
	return &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, nil
}
