package server

import (
	"github.com/redis/go-redis/v9"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi/pkg/redisconn"
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
	return redisconn.Options(redisconn.Config{
		Address:     cfg.Address,
		Username:    cfg.Username,
		Password:    password.Reveal(),
		Database:    cfg.Database,
		TLS:         cfg.TLS,
		CAFile:      cfg.CAFile,
		DialTimeout: cfg.DialTimeout,
	})
}
