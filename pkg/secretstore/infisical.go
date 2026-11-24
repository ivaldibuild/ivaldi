package secretstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// DefaultInfisicalSite is Infisical Cloud's US site.
const DefaultInfisicalSite = "https://app.infisical.com"

// InfisicalSites are Infisical Cloud's sites. A store elsewhere is
// self-hosted.
var InfisicalSites = []string{DefaultInfisicalSite, "https://us.infisical.com", "https://eu.infisical.com"}

// InfisicalConfig reads one environment of an Infisical project, under one
// folder.
type InfisicalConfig struct {
	// SiteURL is Infisical's address; empty means DefaultInfisicalSite.
	SiteURL     string
	ProjectID   string
	Environment string
	// SecretPath is the folder references are relative to; empty means
	// the root, "/".
	SecretPath string
	// Token returns an access token that can read the folder. The reader
	// supplies it, from whichever login its store uses.
	Token  func(ctx context.Context) (string, error)
	Client *http.Client
}

// NewInfisical returns the infisical store. A Ref's Path is the secret's
// name, or folder/NAME relative to SecretPath; its Field a key of a JSON
// value; and its Version a version number. Secret references in values are
// expanded, and imported secrets read, as Infisical does by default.
func NewInfisical(cfg InfisicalConfig) Store {
	if cfg.SiteURL == "" {
		cfg.SiteURL = DefaultInfisicalSite
	}
	if cfg.Client == nil {
		cfg.Client = noRedirects()
	}
	cfg.SiteURL = strings.TrimRight(cfg.SiteURL, "/")
	cfg.SecretPath = path.Clean("/" + cfg.SecretPath)
	return &infisicalStore{cfg: cfg}
}

type infisicalStore struct{ cfg InfisicalConfig }

// infisicalSecret is the part of a secret Infisical returns that the reader
// uses.
type infisicalSecret struct {
	Key     string `json:"secretKey"`
	Value   string `json:"secretValue"`
	Version int    `json:"version"`
	// Hidden is set when the identity may list the secret but not read its
	// value.
	Hidden bool `json:"secretValueHidden"`
}

func (i *infisicalStore) Kind() Kind     { return KindInfisical }
func (i *infisicalStore) Versions() bool { return true }

func (i *infisicalStore) Get(ctx context.Context, ref Ref) (Value, error) {
	folder, name, err := i.locate(ref.Path)
	if err != nil {
		return Value{}, fail(KindInfisical, ref, err)
	}
	query := i.query(folder)
	query.Set("type", "shared")
	query.Set("includeImports", "true")
	if ref.Version != "" {
		if n, err := strconv.Atoi(ref.Version); err != nil || n < 1 {
			return Value{}, fail(KindInfisical, ref, fmt.Errorf("%w: an Infisical version is a number from 1", ErrVersionGone))
		}
		query.Set("version", ref.Version)
	}
	var body struct {
		Secret infisicalSecret `json:"secret"`
	}
	status, err := i.get(ctx, "/api/v4/secrets/"+url.PathEscape(name), query, &body)
	switch {
	case err != nil:
		return Value{}, fail(KindInfisical, ref, err)
	case status == http.StatusNotFound && ref.Version != "":
		return Value{}, fail(KindInfisical, ref, ErrVersionGone)
	case status == http.StatusNotFound:
		return Value{}, fail(KindInfisical, ref, ErrNotFound)
	case status != http.StatusOK:
		return Value{}, fail(KindInfisical, ref, fmt.Errorf("%w: infisical answered %d", ErrUnavailable, status))
	}
	return i.value(ref, body.Secret)
}

// GetMany lists the folder once when every reference is to the latest
// version of a secret in it, and reads each by name otherwise. A secret the
// folder does not hold itself, such as an imported one, is read by name.
func (i *infisicalStore) GetMany(ctx context.Context, refs []Ref) ([]Value, error) {
	folder, listable := "", len(refs) > 1
	for n, ref := range refs {
		f, _, err := i.locate(ref.Path)
		if err != nil || ref.Version != "" || (n > 0 && f != folder) {
			listable = false
			break
		}
		folder = f
	}
	var listed map[string]infisicalSecret
	if listable {
		var err error
		if listed, err = i.list(ctx, folder); err != nil {
			return nil, fail(KindInfisical, refs[0], err)
		}
	}
	out := make([]Value, 0, len(refs))
	for _, ref := range refs {
		_, name, _ := i.locate(ref.Path)
		secret, ok := listed[name]
		var v Value
		var err error
		if ok {
			v, err = i.value(ref, secret)
		} else {
			v, err = i.Get(ctx, ref)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// list returns the secrets a folder holds itself, by name.
func (i *infisicalStore) list(ctx context.Context, folder string) (map[string]infisicalSecret, error) {
	query := i.query(folder)
	query.Set("includeImports", "false")
	query.Set("recursive", "false")
	var body struct {
		Secrets []infisicalSecret `json:"secrets"`
	}
	status, err := i.get(ctx, "/api/v4/secrets", query, &body)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNotFound:
		return nil, ErrNotFound
	case status != http.StatusOK:
		return nil, fmt.Errorf("%w: infisical answered %d", ErrUnavailable, status)
	}
	out := make(map[string]infisicalSecret, len(body.Secrets))
	for _, s := range body.Secrets {
		out[s.Key] = s
	}
	return out, nil
}

func (i *infisicalStore) value(ref Ref, s infisicalSecret) (Value, error) {
	if s.Hidden {
		return Value{}, fail(KindInfisical, ref, fmt.Errorf("%w: the identity may not read the secret's value", ErrDenied))
	}
	value, err := field(s.Value, ref.Field)
	if err != nil {
		return Value{}, fail(KindInfisical, ref, err)
	}
	return Value{Value: value, Version: strconv.Itoa(s.Version), Source: ref}, nil
}

// locate splits a reference's path into the folder it is in and the
// secret's name. A path cannot leave the store's folder.
func (i *infisicalStore) locate(p string) (folder, name string, err error) {
	dir, name := path.Split(p)
	if name == "" {
		return "", "", fmt.Errorf("%w: the path names no secret", ErrNotFound)
	}
	folder = path.Join(i.cfg.SecretPath, dir)
	if folder != i.cfg.SecretPath && !strings.HasPrefix(folder, strings.TrimSuffix(i.cfg.SecretPath, "/")+"/") {
		return "", "", fmt.Errorf("%w: the path leaves the store's folder %s", ErrNotFound, i.cfg.SecretPath)
	}
	return folder, name, nil
}

func (i *infisicalStore) query(folder string) url.Values {
	return url.Values{
		"projectId": {i.cfg.ProjectID}, "environment": {i.cfg.Environment}, "secretPath": {folder},
		"viewSecretValue": {"true"}, "expandSecretReferences": {"true"},
	}
}

// get sends a GET and decodes a 200's body into out. It turns transport
// failures, 429, and 5xx into ErrUnavailable and 401/403 into ErrDenied, and
// returns other statuses for the caller.
func (i *infisicalStore) get(ctx context.Context, endpoint string, query url.Values, out any) (int, error) {
	token, err := i.cfg.Token(ctx)
	if err != nil {
		if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrDenied) {
			return 0, err
		}
		return 0, fmt.Errorf("%w: log in to Infisical: %v", ErrDenied, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.cfg.SiteURL+endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := i.cfg.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return resp.StatusCode, fmt.Errorf("%w: infisical answered %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp.StatusCode, fmt.Errorf("%w: infisical answered %d", ErrDenied, resp.StatusCode)
	case resp.StatusCode == http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("%w: unreadable response: %v", ErrUnavailable, err)
		}
	}
	return resp.StatusCode, nil
}

// noRedirects is a client that follows no redirect, so a request never
// carries a token to wherever a reply points.
func noRedirects() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
