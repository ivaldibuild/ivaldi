package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
)

func writeSecret(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

func TestRedisOptionsReadsTheConnectionSettings(t *testing.T) {
	cfg := config.Redis{
		Address:      "redis.internal:6380",
		Username:     "control-plane",
		PasswordFile: writeSecret(t, "hunter2\n"),
		Database:     2,
		DialTimeout:  5 * time.Second,
	}

	opts, err := redisOptions(cfg)

	require.NoError(t, err)
	assert.Equal(t, "redis.internal:6380", opts.Addr)
	assert.Equal(t, "control-plane", opts.Username)
	assert.Equal(t, "hunter2", opts.Password)
	assert.Equal(t, 2, opts.DB)
	assert.Equal(t, 5*time.Second, opts.DialTimeout)
	assert.Nil(t, opts.TLSConfig, "TLS is off")
}

func TestRedisOptionsLoadsTheCA(t *testing.T) {
	cfg := config.Redis{Address: "redis:6379", TLS: true, CAFile: writeCACert(t)}

	opts, err := redisOptions(cfg)

	require.NoError(t, err)
	require.NotNil(t, opts.TLSConfig)
	assert.Equal(t, uint16(tls.VersionTLS12), opts.TLSConfig.MinVersion)
	assert.NotNil(t, opts.TLSConfig.RootCAs)
}

func TestRedisOptionsRejectsACAWithoutCertificates(t *testing.T) {
	cfg := config.Redis{Address: "redis:6379", TLS: true, CAFile: writeSecret(t, "not a certificate")}

	_, err := redisOptions(cfg)

	assert.ErrorContains(t, err, "no PEM certificates")
}

func TestRedisOptionsReportsAMissingPasswordFile(t *testing.T) {
	cfg := config.Redis{Address: "redis:6379", PasswordFile: filepath.Join(t.TempDir(), "absent")}

	_, err := redisOptions(cfg)

	assert.ErrorContains(t, err, "redis.password")
}

// writeCACert writes a self-signed CA certificate, so the TLS configuration
// has a PEM file it can actually load.
func writeCACert(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "redis"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return path
}
