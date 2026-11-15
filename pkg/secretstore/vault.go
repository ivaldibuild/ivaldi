package secretstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// vaultConcurrency bounds the reads one GetMany runs at once.
const vaultConcurrency = 8

// VaultConfig addresses a Vault KV v2 mount read with Kubernetes auth.
type VaultConfig struct {
	Address string
	// Mount is the KV v2 mount.
	Mount string
	// AuthRole is the Kubernetes auth role to log in as.
	AuthRole string
	// AuthMount is the Kubernetes auth mount; empty means "kubernetes".
	AuthMount string
	// Namespace is a Vault Enterprise namespace.
	Namespace string
	// JWT returns the service-account token to log in with. Address
	// receives it, so both must be the operator's choice: a token made for
	// Vault's audience, and an address they trust with it.
	JWT    func() (string, error)
	Client *http.Client
}

// NewVault returns the vault store. It logs in once, on first use, and
// reuses the token for every read. A Ref's Path is the secret's path in the
// mount, its Field a key of the secret, and its Version a KV v2 version.
func NewVault(cfg VaultConfig) Store {
	if cfg.AuthMount == "" {
		cfg.AuthMount = "kubernetes"
	}
	if cfg.Client == nil {
		// A redirect is not followed: a login would carry the service-account
		// token to wherever the reply pointed.
		cfg.Client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}
	}
	cfg.Address = strings.TrimRight(cfg.Address, "/")
	return &vaultStore{cfg: cfg}
}

type vaultStore struct {
	cfg   VaultConfig
	mu    sync.Mutex
	token string
}

func (v *vaultStore) Kind() Kind     { return KindVault }
func (v *vaultStore) Versions() bool { return true }

func (v *vaultStore) GetMany(ctx context.Context, refs []Ref) ([]Value, error) {
	out := make([]Value, len(refs))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(vaultConcurrency)
	for i, ref := range refs {
		group.Go(func() error {
			value, err := v.Get(ctx, ref)
			out[i] = value
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func (v *vaultStore) Get(ctx context.Context, ref Ref) (Value, error) {
	token, err := v.login(ctx)
	if err != nil {
		return Value{}, fail(KindVault, ref, err)
	}
	path := strings.TrimPrefix(strings.TrimPrefix(ref.Path, v.cfg.Mount+"/"), "/")
	query := url.Values{}
	if ref.Version != "" {
		query.Set("version", ref.Version)
	}
	endpoint := fmt.Sprintf("%s/v1/%s/data/%s", v.cfg.Address, v.cfg.Mount, path)
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var body struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata struct {
				Version   int    `json:"version"`
				Destroyed bool   `json:"destroyed"`
				Deletion  string `json:"deletion_time"`
			} `json:"metadata"`
		} `json:"data"`
	}
	status, err := v.do(ctx, http.MethodGet, endpoint, token, nil, &body)
	if err != nil {
		return Value{}, fail(KindVault, ref, err)
	}
	if status == http.StatusNotFound || body.Data.Data == nil {
		if ref.Version != "" {
			return Value{}, fail(KindVault, ref, ErrVersionGone)
		}
		return Value{}, fail(KindVault, ref, ErrNotFound)
	}
	version := strconv.Itoa(body.Data.Metadata.Version)
	if body.Data.Metadata.Destroyed || body.Data.Metadata.Deletion != "" {
		return Value{}, fail(KindVault, ref, ErrVersionGone)
	}
	raw := body.Data.Data
	if ref.Field == "" {
		if len(raw) != 1 {
			return Value{}, fail(KindVault, ref, fmt.Errorf("%w: the secret has %d keys, so the reference must name one", ErrFieldMissing, len(raw)))
		}
		for name := range raw {
			ref.Field = name
		}
	}
	value, ok := raw[ref.Field]
	if !ok {
		return Value{}, fail(KindVault, ref, ErrFieldMissing)
	}
	s, ok := value.(string)
	if !ok {
		encoded, _ := json.Marshal(value)
		s = string(encoded)
	}
	return Value{Value: s, Version: version, Source: ref}, nil
}

// login logs in with Kubernetes auth once and caches the token.
func (v *vaultStore) login(ctx context.Context) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.token != "" {
		return v.token, nil
	}
	jwt, err := v.cfg.JWT()
	if err != nil {
		return "", fmt.Errorf("%w: read the service-account token: %v", ErrDenied, err)
	}
	var body struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	payload, _ := json.Marshal(map[string]string{"role": v.cfg.AuthRole, "jwt": jwt})
	endpoint := fmt.Sprintf("%s/v1/auth/%s/login", v.cfg.Address, v.cfg.AuthMount)
	status, err := v.do(ctx, http.MethodPost, endpoint, "", payload, &body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || body.Auth.ClientToken == "" {
		return "", fmt.Errorf("%w: kubernetes auth as role %s failed (%d)", ErrDenied, v.cfg.AuthRole, status)
	}
	v.token = body.Auth.ClientToken
	return v.token, nil
}

// do sends a request and decodes a 200's body into out. It turns transport
// failures, 429, and 5xx into ErrUnavailable and 401/403 into ErrDenied, and
// returns other statuses for the caller.
func (v *vaultStore) do(ctx context.Context, method, endpoint, token string, payload []byte, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if v.cfg.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", v.cfg.Namespace)
	}
	resp, err := v.cfg.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return resp.StatusCode, fmt.Errorf("%w: vault answered %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp.StatusCode, fmt.Errorf("%w: vault answered %d", ErrDenied, resp.StatusCode)
	case resp.StatusCode == http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("%w: unreadable response: %v", ErrUnavailable, err)
		}
	}
	return resp.StatusCode, nil
}
