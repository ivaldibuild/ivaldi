// Package auth holds the agent's credential for the control plane: it redeems
// the enrolment token, authenticates every request with the credential,
// renews it at half its life, and notices when the control plane stops
// accepting it (see docs/design/workload-enrolment.md).
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/state"
)

// Method is how the agent authenticates to the control plane.
type Method string

// The methods the control plane accepts.
const (
	MethodKubernetes Method = "kubernetes"
	MethodBearer     Method = "bearer"
	MethodMTLS       Method = "mtls"
)

// Valid reports whether m is a known method.
func (m Method) Valid() bool {
	switch m {
	case MethodKubernetes, MethodBearer, MethodMTLS:
		return true
	}
	return false
}

var (
	// ErrNotEnrolled means the agent holds no credential and has no
	// enrolment token it has not already tried. It needs a new token.
	ErrNotEnrolled = errors.New("the agent is not enrolled; it needs a new enrolment token")
	// ErrCredentialLost means the control plane no longer accepts the
	// agent's credential. The agent has forgotten it and needs a new
	// enrolment token.
	ErrCredentialLost = errors.New("the control plane no longer accepts the agent's credential; it needs a new enrolment token")
)

// Credentials is the agent's credential for the control plane.
type Credentials interface {
	// Enrol makes sure the agent holds a credential and returns its state.
	// It redeems the enrolment token when the token has not been tried
	// before, and otherwise uses the stored credential. It returns
	// ErrNotEnrolled when neither gives a credential; a refused redeem is
	// never tried again. Transient failures are retried until ctx ends.
	Enrol(ctx context.Context) (state.State, error)
	// Client returns the HTTP client for the control plane, which
	// authenticates every request with the credential.
	Client() *http.Client
	// Maintain renews the credential at half its life, ±5%, until ctx
	// ends. It returns ErrCredentialLost, after forgetting the
	// credential, when the control plane refuses it, when it expires, or
	// when Invalidate is called.
	Maintain(ctx context.Context) error
	// Invalidate tells Maintain the credential is no longer accepted, for
	// callers that learn it from a response, such as a 403 because the
	// Cluster has another agent.
	Invalidate(reason string)
}

// Config configures Credentials.
type Config struct {
	ServerURL           string
	Method              Method
	KubernetesTokenFile string
	ClientCertFile      string
	ClientKeyFile       string
	ClientCAFile        string
	State               state.Store
	Enrolment           state.EnrolmentTokens
	Logger              *slog.Logger
}

// New returns Credentials for cfg. It checks the TLS files but makes no
// request.
func New(cfg Config) (Credentials, error) {
	if !cfg.Method.Valid() {
		return nil, fmt.Errorf("unknown authentication method %q", cfg.Method)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	base, err := baseTransport(cfg)
	if err != nil {
		return nil, err
	}
	m := &manager{cfg: cfg, now: time.Now, jitter: rand.Float64, retryBase: time.Second}
	m.client = &http.Client{Timeout: requestTimeout, Transport: &authTransport{next: base, manager: m}}
	public := &http.Client{Timeout: requestTimeout, Transport: base}
	m.public, err = apiclient.NewClientWithResponses(cfg.ServerURL, apiclient.WithHTTPClient(public))
	if err != nil {
		return nil, fmt.Errorf("control plane client: %w", err)
	}
	m.api, err = apiclient.NewClientWithResponses(cfg.ServerURL, apiclient.WithHTTPClient(m.client))
	if err != nil {
		return nil, fmt.Errorf("control plane client: %w", err)
	}
	return m, nil
}

type manager struct {
	cfg       Config
	client    *http.Client
	public    *apiclient.ClientWithResponses // redeem only: no credential yet
	api       *apiclient.ClientWithResponses
	now       func() time.Time
	jitter    func() float64 // [0, 1)
	retryBase time.Duration

	mu      sync.Mutex
	current state.State
	lost    chan struct{}
	reason  string
	// capped is set when a renewal could not extend the credential: it
	// is left to expire.
	capped bool
}

func (m *manager) Client() *http.Client { return m.client }

func (m *manager) Enrol(ctx context.Context) (state.State, error) {
	st, err := m.cfg.State.Load(ctx)
	if err != nil {
		return state.State{}, err
	}
	if st.HasCredential() && st.ExpiresAt != nil && !m.now().Before(*st.ExpiresAt) {
		m.cfg.Logger.Warn("the stored credential has expired", "credential_id", st.CredentialID, "expired_at", *st.ExpiresAt)
		st = st.WithoutCredential()
		if err := m.cfg.State.Save(ctx, st); err != nil {
			return state.State{}, err
		}
	}

	token, err := m.cfg.Enrolment.Token(ctx)
	switch {
	case errors.Is(err, state.ErrNoEnrolmentToken):
		token = ""
	case err != nil:
		return state.State{}, err
	}
	if token != "" && state.HashEnrolmentToken(token) != st.EnrolmentTokenSHA256 {
		st, err = m.redeem(ctx, st, token)
		if err != nil {
			return state.State{}, err
		}
	}
	if !st.HasCredential() {
		return state.State{}, ErrNotEnrolled
	}

	m.mu.Lock()
	m.current = st
	m.lost = make(chan struct{})
	m.reason = ""
	m.capped = false
	m.mu.Unlock()
	return st, nil
}

// redeem redeems token. A refusal is recorded so the token is never tried
// again, and leaves any credential the agent already holds in place.
func (m *manager) redeem(ctx context.Context, st state.State, token string) (state.State, error) {
	body := apiclient.RedeemRequestBody{Token: token, Method: apiclient.RedeemRequestBodyMethod(m.cfg.Method)}
	if m.cfg.Method == MethodKubernetes {
		projected, err := readTokenFile(m.cfg.KubernetesTokenFile)
		if err != nil {
			return state.State{}, err
		}
		body.KubernetesToken = &projected
	}

	var resp *apiclient.RedeemClusterEnrolmentResponse
	err := m.retry(ctx, func() (bool, error) {
		var err error
		resp, err = m.public.RedeemClusterEnrolmentWithResponse(ctx, body)
		if err != nil {
			return true, err
		}
		return transient(resp.StatusCode()), nil
	})
	if err != nil {
		return state.State{}, fmt.Errorf("redeem the enrolment token: %w", err)
	}

	hash := state.HashEnrolmentToken(token)
	if resp.JSON201 == nil {
		m.cfg.Logger.Error("the control plane refused the enrolment token; mint a new one and put it in the enrolment secret",
			"status", resp.StatusCode(), "detail", problemDetail(resp.ApplicationproblemJSONDefault))
		st.EnrolmentTokenSHA256 = hash
		if err := m.cfg.State.Save(ctx, st); err != nil {
			return state.State{}, err
		}
		return st, nil
	}

	r := resp.JSON201.Data
	next := state.State{
		ClusterID:            r.ClusterId,
		OrganizationID:       r.OrganizationId,
		WorkloadPrincipalID:  r.WorkloadPrincipalId,
		CredentialID:         r.CredentialId,
		CredentialKind:       r.CredentialKind,
		ExpiresAt:            r.ExpiresAt,
		IssuedAt:             m.now(),
		EnrolmentTokenSHA256: hash,
	}
	if r.WorkloadToken != nil {
		next.WorkloadToken = *r.WorkloadToken
	}
	// The token is spent, so this credential is the only one there will
	// be: keep trying to save it rather than give it up.
	if err := m.save(ctx, next, "the redeemed credential"); err != nil {
		return state.State{}, err
	}
	m.cfg.Logger.Info("enrolled", "cluster_id", next.ClusterID, "workload_principal_id", next.WorkloadPrincipalID,
		"credential_kind", next.CredentialKind)
	return next, nil
}

func (m *manager) Invalidate(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lost == nil {
		return
	}
	select {
	case <-m.lost:
	default:
		m.reason = reason
		close(m.lost)
	}
}

func (m *manager) Maintain(ctx context.Context) error {
	m.mu.Lock()
	lost := m.lost
	m.mu.Unlock()
	if lost == nil {
		return ErrNotEnrolled
	}

	for {
		st := m.snapshot()
		if st.ExpiresAt == nil {
			// A credential without an expiry needs no renewal.
			select {
			case <-ctx.Done():
				return nil
			case <-lost:
				return m.forget(ctx)
			}
		}
		m.mu.Lock()
		capped := m.capped
		m.mu.Unlock()
		at := m.renewAt(st)
		if capped {
			at = *st.ExpiresAt
		}
		timer := time.NewTimer(max(0, at.Sub(m.now())))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-lost:
			timer.Stop()
			return m.forget(ctx)
		case <-timer.C:
		}
		if capped {
			m.Invalidate("the credential expired and could not be extended")
			return m.forget(ctx)
		}
		if err := m.renew(ctx, st); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, ErrCredentialLost) {
				return m.forget(ctx)
			}
			return err
		}
	}
}

// renewAt is halfway through the credential's life, ±5% of it.
func (m *manager) renewAt(st state.State) time.Time {
	life := st.ExpiresAt.Sub(st.IssuedAt)
	fraction := 0.45 + 0.1*m.jitter()
	return st.IssuedAt.Add(time.Duration(float64(life) * fraction))
}

// renew renews the credential, retrying transient failures while it is
// still valid.
func (m *manager) renew(ctx context.Context, st state.State) error {
	deadline, cancel := context.WithDeadline(ctx, *st.ExpiresAt)
	defer cancel()

	var resp *apiclient.RenewWorkloadCredentialResponse
	err := m.retry(deadline, func() (bool, error) {
		var err error
		resp, err = m.api.RenewWorkloadCredentialWithResponse(deadline, apiclient.RenewRequestBody{})
		if err != nil {
			return true, err
		}
		return transient(resp.StatusCode()), nil
	})
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, context.DeadlineExceeded):
		m.Invalidate("the credential expired before it could be renewed")
		return ErrCredentialLost
	case err != nil:
		return fmt.Errorf("renew the credential: %w", err)
	}
	if resp.JSON200 == nil {
		m.Invalidate(fmt.Sprintf("renewal was refused (%d): %s", resp.StatusCode(), problemDetail(resp.ApplicationproblemJSONDefault)))
		return ErrCredentialLost
	}

	r := resp.JSON200.Data
	if st.ExpiresAt != nil && r.ExpiresAt != nil && !r.ExpiresAt.After(*st.ExpiresAt) {
		// The control plane could not extend it, as with an mTLS
		// credential that has reached its certificate's expiry. Renewing
		// again would only come sooner and sooner; it runs out instead.
		m.mu.Lock()
		m.capped = true
		m.mu.Unlock()
		m.cfg.Logger.Warn("the credential cannot be extended past its expiry; the agent will need a new enrolment token then",
			"credential_id", r.CredentialId, "expires_at", *r.ExpiresAt)
		return nil
	}
	next := st
	next.CredentialID = r.CredentialId
	next.CredentialKind = r.CredentialKind
	next.ExpiresAt = r.ExpiresAt
	next.IssuedAt = m.now()
	if r.WorkloadToken != nil {
		next.WorkloadToken = *r.WorkloadToken
	}
	// The new credential is used at once, because the old bearer token
	// stops working after a short grace period whether or not it is
	// saved, and saving is retried until it succeeds, so a restart never
	// loads a token the control plane has replaced.
	m.mu.Lock()
	m.current = next
	m.mu.Unlock()
	if err := m.save(ctx, next, "the renewed credential"); err != nil {
		return err
	}
	m.cfg.Logger.Info("renewed the credential", "credential_id", next.CredentialID, "expires_at", next.ExpiresAt)
	return nil
}

// save writes st to the state Secret, retrying until it succeeds or ctx
// ends: what it holds cannot be fetched again.
func (m *manager) save(ctx context.Context, st state.State, what string) error {
	wait := m.retryBase
	for {
		err := m.cfg.State.Save(ctx, st)
		if err == nil {
			return nil
		}
		m.cfg.Logger.Error("could not save "+what+"; retrying", "error", err, "in", wait)
		select {
		case <-ctx.Done():
			return fmt.Errorf("save %s: %w", what, err)
		case <-time.After(wait):
		}
		wait = min(2*wait, 30*time.Second)
	}
}

// forget removes the lost credential from the state, so a restart waits for
// a new enrolment token instead of using it, and returns ErrCredentialLost.
func (m *manager) forget(ctx context.Context) error {
	m.mu.Lock()
	st, reason := m.current, m.reason
	m.mu.Unlock()
	m.cfg.Logger.Error("the credential is no longer valid; mint a new enrolment token and put it in the enrolment secret",
		"reason", reason, "credential_id", st.CredentialID)
	// The process may be stopping; forgetting still matters.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()
	if err := m.cfg.State.Save(saveCtx, st.WithoutCredential()); err != nil {
		m.cfg.Logger.Error("could not forget the lost credential", "error", err)
	}
	return ErrCredentialLost
}

func (m *manager) snapshot() state.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// authenticate adds the credential to req.
func (m *manager) authenticate(req *http.Request) error {
	switch m.cfg.Method {
	case MethodKubernetes:
		token, err := readTokenFile(m.cfg.KubernetesTokenFile)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(credentialHeader, credentialHeaderKubernetes)
	case MethodBearer:
		token := m.snapshot().WorkloadToken
		if token == "" {
			return errors.New("the agent holds no bearer token")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	case MethodMTLS:
		// The client certificate is presented during the TLS handshake.
	}
	return nil
}

// retry calls attempt until it succeeds or reports a permanent outcome,
// backing off from retryBase up to 30s, until ctx ends.
func (m *manager) retry(ctx context.Context, attempt func() (again bool, err error)) error {
	wait := m.retryBase
	for {
		again, err := attempt()
		if !again {
			return err
		}
		if err != nil {
			m.cfg.Logger.Warn("control plane request failed; retrying", "error", err, "in", wait)
		} else {
			m.cfg.Logger.Warn("control plane is unavailable; retrying", "in", wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait = min(2*wait, 30*time.Second)
	}
}

// transient reports whether a response status is worth retrying.
func transient(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func problemDetail(p *apiclient.ErrorModel) string {
	if p == nil || p.Detail == nil {
		return ""
	}
	return *p.Detail
}
