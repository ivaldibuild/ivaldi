package secretstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DefaultDopplerAPI is Doppler's API.
const DefaultDopplerAPI = "https://api.doppler.com"

// DopplerConfig reads one Doppler config.
type DopplerConfig struct {
	// Project and Config name the config. Both are empty for a service
	// token, which implies its config; another token needs them.
	Project string
	Config  string
	// Token returns a token that can read the config. The reader supplies
	// it, from wherever its operator keeps it.
	Token func(ctx context.Context) (string, error)
	// API is Doppler's API address; empty means DefaultDopplerAPI.
	API    string
	Client *http.Client
}

// NewDoppler returns the doppler store. A Ref's Path is the secret's name and
// its Field a key of a JSON value. Doppler has no per-secret versions, so a
// pinned value is checked by fingerprint instead.
func NewDoppler(cfg DopplerConfig) Store {
	if cfg.API == "" {
		cfg.API = DefaultDopplerAPI
	}
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	cfg.API = strings.TrimRight(cfg.API, "/")
	return &dopplerStore{cfg: cfg}
}

type dopplerStore struct{ cfg DopplerConfig }

func (d *dopplerStore) Kind() Kind     { return KindDoppler }
func (d *dopplerStore) Versions() bool { return false }

func (d *dopplerStore) Get(ctx context.Context, ref Ref) (Value, error) {
	values, err := d.GetMany(ctx, []Ref{ref})
	if err != nil {
		return Value{}, err
	}
	return values[0], nil
}

// GetMany downloads the whole config once and picks each value from it.
func (d *dopplerStore) GetMany(ctx context.Context, refs []Ref) ([]Value, error) {
	for _, ref := range refs {
		if ref.Version != "" {
			return nil, fail(KindDoppler, ref, fmt.Errorf("%w: Doppler has no per-secret versions", ErrVersionGone))
		}
	}
	secrets, err := d.download(ctx)
	if err != nil {
		ref := Ref{}
		if len(refs) > 0 {
			ref = refs[0]
		}
		return nil, fail(KindDoppler, ref, err)
	}
	out := make([]Value, 0, len(refs))
	for _, ref := range refs {
		raw, ok := secrets[ref.Path]
		if !ok {
			return nil, fail(KindDoppler, ref, ErrNotFound)
		}
		value, err := field(raw, ref.Field)
		if err != nil {
			return nil, fail(KindDoppler, ref, err)
		}
		out = append(out, Value{Value: value, Source: ref})
	}
	return out, nil
}

func (d *dopplerStore) download(ctx context.Context) (map[string]string, error) {
	token, err := d.cfg.Token(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: read the Doppler token: %v", ErrDenied, err)
	}
	query := url.Values{"format": {"json"}}
	if d.cfg.Project != "" {
		query.Set("project", d.cfg.Project)
		query.Set("config", d.cfg.Config)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.cfg.API+"/v3/configs/config/secrets/download?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := d.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: doppler answered %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: doppler answered %d", ErrDenied, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: doppler answered %d", ErrUnavailable, resp.StatusCode)
	}
	var secrets map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&secrets); err != nil {
		return nil, fmt.Errorf("%w: unreadable response: %v", ErrUnavailable, err)
	}
	return secrets, nil
}
