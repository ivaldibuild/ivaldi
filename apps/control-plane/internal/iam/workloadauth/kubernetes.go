package workloadauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

var kubernetesAlgorithms = []jose.SignatureAlgorithm{jose.RS256, jose.ES256, jose.EdDSA}

// KubernetesIdentity is what a valid projected service-account token says.
type KubernetesIdentity struct {
	Issuer    string
	Subject   string
	Audience  string
	ExpiresAt time.Time
}

// KubernetesVerifier verifies a projected service-account token.
type KubernetesVerifier interface {
	Verify(ctx context.Context, token string) (KubernetesIdentity, error)
}

// NewKubernetesVerifier returns a verifier that accepts tokens from the
// configured issuers and audience. An empty issuer list disables this kind.
func NewKubernetesVerifier(issuers []string, audience string, client *http.Client) (*KubernetesVerifierImpl, error) {
	if audience == "" {
		return nil, errors.New("kubernetes verifier: audience is required")
	}
	for _, issuer := range issuers {
		u, err := url.Parse(issuer)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("kubernetes verifier: issuer %q is not a URL", issuer)
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &KubernetesVerifierImpl{
		issuers: slices.Clone(issuers), audience: audience, client: client,
		keys: map[string]cachedKeys{},
	}, nil
}

// KubernetesVerifierImpl verifies Kubernetes tokens through OIDC discovery.
type KubernetesVerifierImpl struct {
	issuers  []string
	audience string
	client   *http.Client
	mu       sync.Mutex
	keys     map[string]cachedKeys
}

type cachedKeys struct {
	set     *jose.JSONWebKeySet
	fetched time.Time
}

const keyCacheTTL = time.Hour

func (v *KubernetesVerifierImpl) Verify(ctx context.Context, token string) (KubernetesIdentity, error) {
	jws, err := jose.ParseSigned(token, kubernetesAlgorithms)
	if err != nil {
		return KubernetesIdentity{}, fmt.Errorf("%w: parse kubernetes token: %v", identity.ErrInvalidToken, err)
	}
	payload := jws.UnsafePayloadWithoutVerification()
	var claims kubernetesClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return KubernetesIdentity{}, fmt.Errorf("%w: kubernetes claims: %v", identity.ErrInvalidToken, err)
	}
	if !slices.Contains(v.issuers, claims.Issuer) {
		return KubernetesIdentity{}, fmt.Errorf("%w: issuer %q is not allowed", identity.ErrInvalidToken, claims.Issuer)
	}
	audience, err := claims.audience()
	if err != nil {
		return KubernetesIdentity{}, err
	}
	if !slices.Contains(audience, v.audience) {
		return KubernetesIdentity{}, fmt.Errorf("%w: audience %q is not %q", identity.ErrInvalidToken, audience, v.audience)
	}
	now := clock.Now()
	if claims.ExpiresAt <= 0 || !now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return KubernetesIdentity{}, identity.ErrTokenExpired
	}
	if claims.NotBefore > 0 && now.Add(time.Minute).Before(time.Unix(claims.NotBefore, 0)) {
		return KubernetesIdentity{}, fmt.Errorf("%w: not valid yet", identity.ErrInvalidToken)
	}
	keys, err := v.keysFor(ctx, claims.Issuer)
	if err != nil {
		return KubernetesIdentity{}, err
	}
	if len(jws.Signatures) == 0 {
		return KubernetesIdentity{}, fmt.Errorf("%w: kubernetes token has no signature", identity.ErrInvalidToken)
	}
	signature := jws.Signatures[0]
	kid := signature.Protected.KeyID
	candidates := keys.Key(kid)
	if len(candidates) == 0 && kid == "" && len(keys.Keys) == 1 {
		candidates = keys.Keys
	}
	if len(candidates) == 0 {
		return KubernetesIdentity{}, fmt.Errorf("%w: unknown kubernetes key %q", identity.ErrInvalidToken, kid)
	}
	var verified bool
	for _, key := range candidates {
		if key.Algorithm != "" && key.Algorithm != string(signature.Protected.Algorithm) {
			continue
		}
		if _, err := jws.Verify(key.Key); err == nil {
			verified = true
			break
		}
	}
	if !verified {
		return KubernetesIdentity{}, fmt.Errorf("%w: kubernetes signature", identity.ErrInvalidToken)
	}
	if claims.Subject == "" {
		return KubernetesIdentity{}, fmt.Errorf("%w: kubernetes subject", identity.ErrInvalidToken)
	}
	return KubernetesIdentity{
		Issuer: claims.Issuer, Subject: claims.Subject, Audience: v.audience,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(),
	}, nil
}

type kubernetesClaims struct {
	Issuer    string          `json:"iss"`
	Subject   string          `json:"sub"`
	Audience  json.RawMessage `json:"aud"`
	ExpiresAt int64           `json:"exp"`
	NotBefore int64           `json:"nbf"`
}

func (c kubernetesClaims) audience() ([]string, error) {
	var one string
	if err := json.Unmarshal(c.Audience, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(c.Audience, &many); err != nil {
		return nil, fmt.Errorf("%w: kubernetes audience", identity.ErrInvalidToken)
	}
	return many, nil
}

func (v *KubernetesVerifierImpl) keysFor(ctx context.Context, issuer string) (*jose.JSONWebKeySet, error) {
	v.mu.Lock()
	cached, ok := v.keys[issuer]
	v.mu.Unlock()
	if ok && clock.Now().Sub(cached.fetched) < keyCacheTTL {
		return cached.set, nil
	}
	discoveryURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	var discovery struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := v.getJSON(ctx, discoveryURL, &discovery); err != nil {
		return nil, fmt.Errorf("kubernetes discovery for %s: %w", issuer, err)
	}
	if discovery.JWKSURI == "" {
		return nil, fmt.Errorf("kubernetes issuer %s has no jwks_uri", issuer)
	}
	var set jose.JSONWebKeySet
	if err := v.getJSON(ctx, discovery.JWKSURI, &set); err != nil {
		return nil, fmt.Errorf("kubernetes keys for %s: %w", issuer, err)
	}
	v.mu.Lock()
	v.keys[issuer] = cachedKeys{set: &set, fetched: clock.Now()}
	v.mu.Unlock()
	return &set, nil
}

func (v *KubernetesVerifierImpl) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// KubernetesIdentifier is the stable credential identifier for a verified
// Kubernetes identity.
func KubernetesIdentifier(id KubernetesIdentity) string {
	return strings.Join([]string{id.Issuer, id.Subject, id.Audience}, "\n")
}
