package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/state"
)

const namespace = "ivaldi-system"

// controlPlane is a fake control plane: it answers redeem and renew, and
// records the Authorization header of every other request.
type controlPlane struct {
	mu            sync.Mutex
	redeemStatus  int
	redeemBodies  []map[string]any
	expiresIn     time.Duration
	renewals      atomic.Int32
	refuseOthers  atomic.Bool
	authorization []string
	credentialHdr []string
}

func (c *controlPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	expires := time.Now().Add(c.expiresIn)
	switch r.URL.Path {
	case "/v1/agent/enrolments/redeem":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		c.redeemBodies = append(c.redeemBodies, body)
		if c.redeemStatus != 0 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(c.redeemStatus)
			_, _ = w.Write([]byte(`{"status":409,"detail":"the enrollment token was already used"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"organization_id": "org_1", "cluster_id": "cls_1", "workload_principal_id": "prn_1",
			"credential_id": "wcr_1", "credential_kind": "bearer_token", "workload_token": "iwt_first",
			"expires_at": expires,
		}})
	case "/v1/agent/credentials/renew":
		c.authorization = append(c.authorization, r.Header.Get("Authorization"))
		n := c.renewals.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"credential_id": "wcr_2", "credential_kind": "bearer_token", "workload_token": "iwt_renewed",
			"expires_at": expires.Add(time.Duration(n) * time.Hour), "previous_expires_at": expires,
		}})
	default:
		c.authorization = append(c.authorization, r.Header.Get("Authorization"))
		c.credentialHdr = append(c.credentialHdr, r.Header.Get(credentialHeader))
		if c.refuseOthers.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func (c *controlPlane) lastAuthorization() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authorization[len(c.authorization)-1]
}

type fixture struct {
	cp      *controlPlane
	server  *httptest.Server
	kube    *fake.Clientset
	store   state.Store
	creds   *manager
	request func() *http.Response
}

func newFixture(t *testing.T, method Method, token string) *fixture {
	t.Helper()
	cp := &controlPlane{expiresIn: time.Hour}
	server := httptest.NewServer(cp)
	t.Cleanup(server.Close)

	kube := fake.NewClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "state", Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "enrolment", Namespace: namespace},
			Data: map[string][]byte{state.EnrolmentTokenKey: []byte(token)}},
	)
	store := state.NewSecretStore(kube, namespace, "state")
	cfg := Config{
		ServerURL: server.URL,
		Method:    method,
		State:     store,
		Enrolment: state.NewEnrolmentSecret(kube, namespace, "enrolment"),
	}
	if method == MethodKubernetes {
		cfg.KubernetesTokenFile = filepath.Join(t.TempDir(), "token")
		require.NoError(t, os.WriteFile(cfg.KubernetesTokenFile, []byte("projected-1"), 0o600))
	}
	creds, err := New(cfg)
	require.NoError(t, err)
	m := creds.(*manager)
	m.retryBase = time.Millisecond
	return &fixture{cp: cp, server: server, kube: kube, store: store, creds: m, request: func() *http.Response {
		resp, err := m.Client().Get(server.URL + "/v1/agent/targets")
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp
	}}
}

func TestEnrol_RedeemsTheTokenAndAuthenticatesWithTheCredential(t *testing.T) {
	f := newFixture(t, MethodBearer, "iwe_one")

	st, err := f.creds.Enrol(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "cls_1", st.ClusterID)
	assert.Equal(t, "org_1", st.OrganizationID)
	assert.Equal(t, "iwt_first", st.WorkloadToken)

	stored, err := f.store.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, st.CredentialID, stored.CredentialID)
	assert.Equal(t, st.WorkloadToken, stored.WorkloadToken)
	assert.True(t, st.ExpiresAt.Equal(*stored.ExpiresAt))
	assert.True(t, st.IssuedAt.Equal(stored.IssuedAt))
	assert.Equal(t, state.HashEnrolmentToken("iwe_one"), stored.EnrolmentTokenSHA256)

	f.request()
	assert.Equal(t, "Bearer iwt_first", f.cp.lastAuthorization())

	// A restart uses the stored credential and does not redeem again.
	_, err = f.creds.Enrol(t.Context())
	require.NoError(t, err)
	assert.Len(t, f.cp.redeemBodies, 1)
}

func TestEnrol_NeverRetriesARefusedToken(t *testing.T) {
	f := newFixture(t, MethodBearer, "iwe_used")
	f.cp.redeemStatus = http.StatusConflict

	_, err := f.creds.Enrol(t.Context())
	require.ErrorIs(t, err, ErrNotEnrolled)
	_, err = f.creds.Enrol(t.Context())
	require.ErrorIs(t, err, ErrNotEnrolled)
	assert.Len(t, f.cp.redeemBodies, 1)
}

func TestKubernetesMethod_SendsTheProjectedTokenReadAtEachRequest(t *testing.T) {
	f := newFixture(t, MethodKubernetes, "iwe_one")

	_, err := f.creds.Enrol(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "kubernetes", f.cp.redeemBodies[0]["method"])
	assert.Equal(t, "projected-1", f.cp.redeemBodies[0]["kubernetes_token"])

	f.request()
	require.NoError(t, os.WriteFile(f.creds.cfg.KubernetesTokenFile, []byte("projected-2"), 0o600))
	f.request()
	assert.Equal(t, []string{"Bearer projected-1", "Bearer projected-2"}, f.cp.authorization)
	assert.Equal(t, []string{"kubernetes", "kubernetes"}, f.cp.credentialHdr)
}

func TestMaintain_ForgetsACredentialTheControlPlaneRefuses(t *testing.T) {
	f := newFixture(t, MethodBearer, "iwe_one")
	_, err := f.creds.Enrol(t.Context())
	require.NoError(t, err)

	done := make(chan error)
	go func() { done <- f.creds.Maintain(t.Context()) }()
	f.cp.refuseOthers.Store(true)
	assert.Equal(t, http.StatusUnauthorized, f.request().StatusCode)

	select {
	case err := <-done:
		require.ErrorIs(t, err, ErrCredentialLost)
	case <-time.After(5 * time.Second):
		t.Fatal("Maintain did not notice the refused credential")
	}
	stored, err := f.store.Load(t.Context())
	require.NoError(t, err)
	assert.False(t, stored.HasCredential())
	assert.Equal(t, state.HashEnrolmentToken("iwe_one"), stored.EnrolmentTokenSHA256)

	// The spent token is not redeemed again.
	_, err = f.creds.Enrol(t.Context())
	require.ErrorIs(t, err, ErrNotEnrolled)
}

func TestMaintain_RenewsAtHalfLifeAndSavesTheNewTokenBeforeUsingIt(t *testing.T) {
	f := newFixture(t, MethodBearer, "iwe_one")
	f.cp.expiresIn = 200 * time.Millisecond
	f.creds.jitter = func() float64 { return 0.5 }
	_, err := f.creds.Enrol(t.Context())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- f.creds.Maintain(ctx) }()
	require.Eventually(t, func() bool { return f.cp.renewals.Load() >= 1 }, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, "Bearer iwt_first", f.cp.lastAuthorization())

	require.Eventually(t, func() bool {
		stored, err := f.store.Load(t.Context())
		return err == nil && stored.WorkloadToken == "iwt_renewed"
	}, 5*time.Second, 10*time.Millisecond)
	f.request()
	assert.Equal(t, "Bearer iwt_renewed", f.cp.lastAuthorization())

	cancel()
	require.NoError(t, <-done)
}

func TestRenewAt_IsHalfwayThroughTheLifeWithinFivePercent(t *testing.T) {
	issued := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	expires := issued.Add(24 * time.Hour)
	st := state.State{IssuedAt: issued, ExpiresAt: &expires}
	for _, jitter := range []float64{0, 0.5, 0.999} {
		m := &manager{jitter: func() float64 { return jitter }}
		at := m.renewAt(st)
		assert.False(t, at.Before(issued.Add(time.Duration(0.45*float64(24*time.Hour)))), jitter)
		assert.False(t, at.After(issued.Add(time.Duration(0.55*float64(24*time.Hour)))), jitter)
	}
}
