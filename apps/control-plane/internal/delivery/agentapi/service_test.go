package agentapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

var ctx = context.Background()

func TestTargets(t *testing.T) {
	w := newWorld(t)
	resp, err := w.svc.Targets(ctx, agentCaller())
	require.NoError(t, err)

	assert.Equal(t, clusterID, resp.Cluster.ID)
	assert.Equal(t, "prod-eu", resp.Cluster.Slug)
	assert.Equal(t, 30, resp.HeartbeatIntervalSeconds)
	assert.Equal(t, 5, resp.PollIntervalSeconds)
	assert.Equal(t, 60, resp.WatchIntervalSeconds)
	require.Len(t, resp.Targets, 1, "the other cluster's target is not listed")
	target := resp.Targets[0]
	assert.Equal(t, targetID, target.ID)
	assert.Equal(t, "payments", target.Namespace)
	require.Len(t, target.Applications, 1)
	app := target.Applications[0]
	assert.Equal(t, appID, app.ID)
	require.Len(t, app.Artifacts, 1)
	assert.Equal(t, "ghcr.io/acme/api", app.Artifacts[0].ArtifactName)
}

func TestTargetsRefusesANonAgent(t *testing.T) {
	w := newWorld(t)
	_, err := w.svc.Targets(ctx, userCaller())
	assert.ErrorIs(t, err, delivery.ErrNotClusterAgent)

	other := identity.Caller{
		Actor:          identity.Actor{PrincipalID: "prn_unknown", Kind: identity.ActorWorkload},
		OrganizationID: org,
	}
	_, err = w.svc.Targets(ctx, other)
	assert.ErrorIs(t, err, delivery.ErrNotClusterAgent)
}

func TestAssignments(t *testing.T) {
	w := newWorld(t)
	w.assignments = []delivery.DeploymentAssignment{{HelmReleaseName: "ivaldi-app"}}

	items, err := w.svc.Assignments(ctx, agentCaller())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "ivaldi-app", items[0].HelmReleaseName)
}

func TestHeartbeat(t *testing.T) {
	w := newWorld(t)
	in := HeartbeatInput{
		AgentVersion: "1.4.0", KubernetesVersion: "v1.31.4", WatchMode: "cluster",
		Deployers: []string{"helm"}, SecretStoreKinds: []string{"kubernetes"},
		Targets: []TargetHeartbeat{{TargetID: targetID, Observe: true, Reason: "RBAC denies create on pods"}},
	}
	require.NoError(t, w.svc.Heartbeat(ctx, agentCaller(), in))

	cluster := w.clusters[clusterID]
	assert.Equal(t, "1.4.0", cluster.AgentCapabilities()["agent_version"])
	assert.Equal(t, "v1.31.4", cluster.AgentCapabilities()["kubernetes_version"])
	assert.NotNil(t, cluster.LastReportedAt())
	assert.True(t, w.clusterSaved)
	assert.Equal(t, []string{targetID}, w.updated, "a changed capability is written")
	assert.True(t, w.targets[targetID].CanObserve())
	assert.Equal(t, "RBAC denies create on pods", w.targets[targetID].CapabilityReason())
	assert.Equal(t, []string{"target:" + targetID, "cluster:" + clusterID}, w.locks,
		"targets are locked before the cluster, the order reports and restores use")

	w.updated, w.locks = nil, nil
	require.NoError(t, w.svc.Heartbeat(ctx, agentCaller(), in))
	assert.Empty(t, w.updated, "a steady heartbeat writes no target rows")
	assert.Equal(t, []string{"cluster:" + clusterID}, w.locks, "an unchanged target is not locked")
}

func TestHeartbeatRefusesWhatItCannotWrite(t *testing.T) {
	cases := map[string]HeartbeatInput{
		"a target in another cluster": {
			AgentVersion: "1.4.0", KubernetesVersion: "v1.31.4", WatchMode: "cluster",
			Targets: []TargetHeartbeat{{TargetID: otherTargetID, Observe: true}},
		},
		"an unknown target": {
			AgentVersion: "1.4.0", KubernetesVersion: "v1.31.4", WatchMode: "cluster",
			Targets: []TargetHeartbeat{{TargetID: "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAZ"}},
		},
		"an unknown watch mode": {
			AgentVersion: "1.4.0", KubernetesVersion: "v1.31.4", WatchMode: "sideways",
		},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			in := in
			err := w.svc.Heartbeat(ctx, agentCaller(), in)
			require.ErrorIs(t, err, delivery.ErrInvalid)
			assert.Empty(t, w.updated)
			assert.False(t, w.clusterSaved)
			assert.Nil(t, w.clusters[clusterID].LastReportedAt())
		})
	}
}

func TestHeartbeatRefusesANonAgent(t *testing.T) {
	w := newWorld(t)
	err := w.svc.Heartbeat(ctx, userCaller(), HeartbeatInput{
		AgentVersion: "1.4.0", KubernetesVersion: "v1.31.4", WatchMode: "cluster",
	})
	assert.ErrorIs(t, err, delivery.ErrNotClusterAgent)
}

func TestObserveFillsTheClusterAndCarriesADecision(t *testing.T) {
	w := newWorld(t)
	in := delivery.ObserveInput{
		TargetID: targetID, ApplicationID: appID,
		Artifacts: []delivery.ObservedArtifact{{ArtifactName: "ghcr.io/acme/api", Digest: "sha256:" + strings.Repeat("c", 64)}},
	}
	_, err := w.svc.Observe(ctx, agentCaller(), in)
	require.NoError(t, err)

	require.Len(t, w.observed, 1)
	assert.Equal(t, org, w.observed[0].OrganizationID)
	assert.Equal(t, clusterID, w.observed[0].ClusterID, "the Cluster comes from the caller, never the request")
	decision, ok := authorization.DecisionFor(w.observedCtx[0], "")
	require.True(t, ok)
	assert.Equal(t, authorization.OutcomeAllow, decision.Outcome)
	assert.True(t, id.Valid(authorization.DecisionPrefix, decision.ID), "the decision has an ID the audit record can carry: %q", decision.ID)
}

// HTTP.

type verifier struct{}

func (verifier) Verify(_ context.Context, token string) (*identity.Claims, error) {
	switch token {
	case "agent":
		return &identity.Claims{Subject: agentID, Kind: identity.ActorWorkload, OrganizationID: org}, nil
	case "ada":
		return &identity.Claims{Subject: "prn_ada", Kind: identity.ActorUser, OrganizationID: org}, nil
	}
	return nil, identity.ErrInvalidToken
}

type httpWorld struct {
	*world
	server *api.Server
}

func newHTTPWorld(t *testing.T) *httpWorld {
	t.Helper()
	w := newWorld(t)
	port := 0
	server, err := api.New(&config.Config{Port: &port, ExternalURL: "https://ivaldi.example.com"}, "test", logging.NewMockLogger(),
		api.WithRoutes(NewController(w.svc)), api.WithVerifier(verifier{}))
	require.NoError(t, err)
	return &httpWorld{world: w, server: server}
}

func (h *httpWorld) call(t *testing.T, method, path, token, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "/v1"+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.server.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestEveryAgentOperationNeedsAToken(t *testing.T) {
	h := newHTTPWorld(t)
	for _, op := range []struct{ method, path string }{
		{http.MethodGet, "/agent/targets"},
		{http.MethodGet, "/agent/assignments"},
		{http.MethodPost, "/agent/heartbeat"},
		{http.MethodPost, "/agent/observations"},
	} {
		rec, _ := h.call(t, op.method, op.path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", op.method, op.path)
	}
}

func TestAgentOverHTTP(t *testing.T) {
	h := newHTTPWorld(t)

	rec, out := h.call(t, http.MethodGet, "/agent/targets", "agent", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	data := out["data"].(map[string]any)
	assert.Equal(t, clusterID, data["cluster"].(map[string]any)["id"])

	rec, _ = h.call(t, http.MethodPost, "/agent/heartbeat", "agent",
		`{"agent_version":"1.4.0","kubernetes_version":"v1.31.4","watch_mode":"cluster"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec, _ = h.call(t, http.MethodPost, "/agent/observations", "agent",
		`{"report_id":"obs-1","target_id":"`+targetID+`","application_id":"`+appID+`",`+
			`"artifacts":[{"artifact_name":"ghcr.io/acme/api","digest":"sha256:`+strings.Repeat("c", 64)+`"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, h.observed, 1)
}

func TestAgentOverHTTPRefuses(t *testing.T) {
	cases := []struct {
		name               string
		token              string
		method, path, body string
		want               int
	}{
		{"a person", "ada", http.MethodGet, "/agent/targets", "", http.StatusForbidden},
		{"an unknown watch mode", "agent", http.MethodPost, "/agent/heartbeat",
			`{"agent_version":"1.4.0","kubernetes_version":"v1.31.4","watch_mode":"sideways"}`, http.StatusUnprocessableEntity},
		{"a target outside the cluster", "agent", http.MethodPost, "/agent/heartbeat",
			`{"agent_version":"1.4.0","kubernetes_version":"v1.31.4","watch_mode":"cluster","targets":[{"target_id":"` + otherTargetID + `","observe":true}]}`,
			http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHTTPWorld(t)
			rec, _ := h.call(t, tc.method, tc.path, tc.token, tc.body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}
