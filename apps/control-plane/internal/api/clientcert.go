package api

import (
	"context"
	"crypto/x509"
	"net/http"
)

type clientCertificateKey struct{}

// withClientCertificate puts the TLS peer certificate in the request context
// before Huma sees it, so operation handlers and the authenticator can use
// it.
func withClientCertificate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			r = r.WithContext(context.WithValue(r.Context(), clientCertificateKey{}, r.TLS.PeerCertificates[0]))
		}
		next.ServeHTTP(w, r)
	})
}

// ClientCertificate returns the verified client certificate a request
// presented, when the listener requested one.
func ClientCertificate(ctx context.Context) *x509.Certificate {
	c, _ := ctx.Value(clientCertificateKey{}).(*x509.Certificate)
	return c
}
