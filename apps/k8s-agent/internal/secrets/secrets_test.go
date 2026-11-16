package secrets

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, keySize) }

func TestFingerprints(t *testing.T) {
	one, err := newFingerprinter(map[int][]byte{1: key(1)})
	require.NoError(t, err)
	rotated, err := newFingerprinter(map[int][]byte{1: key(1), 2: key(2)})
	require.NoError(t, err)
	other, err := newFingerprinter(map[int][]byte{1: key(9)})
	require.NoError(t, err)

	fp := one.Fingerprint("s3cret")
	assert.Regexp(t, `^hmac-sha256:v1:[0-9a-f]{64}$`, fp)
	assert.Equal(t, fp, one.Fingerprint("s3cret"), "stable for one key")
	assert.NotEqual(t, fp, other.Fingerprint("s3cret"), "different across keys")
	assert.Regexp(t, `^hmac-sha256:v2:`, rotated.Fingerprint("s3cret"), "new fingerprints use the newest key")

	same, err := rotated.Matches(fp, "s3cret")
	require.NoError(t, err)
	assert.True(t, same, "an older key version still checks its fingerprints")
	same, err = rotated.Matches(fp, "changed")
	require.NoError(t, err)
	assert.False(t, same)

	gone, err := newFingerprinter(map[int][]byte{2: key(2)})
	require.NoError(t, err)
	_, err = gone.Matches(fp, "s3cret")
	assert.EqualError(t, err, "fingerprint key v1 is gone")
}

func TestLoadFingerprinter_CreatesAKeyOnceAndKeepsIt(t *testing.T) {
	kube := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "fp", Namespace: "ivaldi-system"}})
	first, err := LoadFingerprinter(t.Context(), kube, "ivaldi-system", "fp")
	require.NoError(t, err)
	second, err := LoadFingerprinter(t.Context(), kube, "ivaldi-system", "fp")
	require.NoError(t, err)
	assert.Equal(t, first.Fingerprint("x"), second.Fingerprint("x"))
}

func TestMaskingHandler(t *testing.T) {
	masker := NewMasker()
	masker.Add("hunter22", "abc")
	var out bytes.Buffer
	log := slog.New(NewMaskingHandler(slog.NewTextHandler(&out, nil), masker)).With("context", "dsn=postgres://u:hunter22@db")

	log.Info("connecting with hunter22",
		"error", errors.New("auth failed for hunter22"),
		slog.Group("detail", "password", "hunter22"),
		"short", "abc")
	line := out.String()
	assert.NotContains(t, line, "hunter22")
	assert.Contains(t, line, "****")
	assert.Contains(t, line, "short=abc", "values shorter than four characters are not masked")

	masker.Remove("hunter22")
	assert.Equal(t, "hunter22", masker.Mask("hunter22"))
}

func newTestResolver(t *testing.T, kube *fake.Clientset) (*resolver, *Masker, Fingerprinter) {
	t.Helper()
	masker := NewMasker()
	r := NewResolver(Config{Kube: kube, AgentNamespace: "ivaldi-system", Masker: masker, Logger: slog.New(slog.DiscardHandler)}).(*resolver)
	r.retryBase = time.Millisecond
	keys, err := newFingerprinter(map[int][]byte{1: key(1)})
	require.NoError(t, err)
	r.loadKeys = func(context.Context) (Fingerprinter, error) { return keys, nil }
	return r, masker, keys
}

func kubernetesSecret(name, field string, fingerprint *string) apiclient.AssignmentSecret {
	return apiclient.AssignmentSecret{
		Name: name, DeliverAs: apiclient.AssignmentSecretDeliverAsSecret, Fingerprint: fingerprint,
		Store: apiclient.StoreSettings{Id: "sst_1", Kind: apiclient.StoreSettingsKindKubernetes},
		Ref:   apiclient.SecretRef{Path: "db", Field: &field},
	}
}

func TestResolver(t *testing.T) {
	kube := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "payments", ResourceVersion: "7"},
		Data:       map[string][]byte{"password": []byte("hunter22")},
	})
	r, masker, keys := newTestResolver(t, kube)

	resolved, err := r.Resolve(t.Context(), "payments", []apiclient.AssignmentSecret{kubernetesSecret("DB_PASSWORD", "password", nil)})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "hunter22", resolved[0].Value)
	assert.Equal(t, "7", resolved[0].Version)
	assert.Equal(t, "****", masker.Mask("hunter22"), "a resolved value is masked")
	pin := resolved[0].Pin()
	assert.Equal(t, "db", pin.Path, "the pin names the path the agent read")
	assert.Equal(t, keys.Fingerprint("hunter22"), pin.Fingerprint)
	assert.Equal(t, "secret", pin.DeliveredAs)

	r.Release(resolved)
	assert.Equal(t, "hunter22", masker.Mask("hunter22"))

	t.Run("a rollback whose secret changed fails naming it", func(t *testing.T) {
		stale := keys.Fingerprint("an-older-password")
		_, err := r.Resolve(t.Context(), "payments", []apiclient.AssignmentSecret{kubernetesSecret("DB_PASSWORD", "password", &stale)})
		var failure *deploy.Failure
		require.ErrorAs(t, err, &failure)
		assert.Contains(t, failure.Reason, "secret DB_PASSWORD (db#password) has changed")
		assert.Equal(t, "hunter22", masker.Mask("hunter22"), "a failed resolve stops masking what it added")
	})

	t.Run("a missing field fails naming the secret and reference, not the value", func(t *testing.T) {
		_, err := r.Resolve(t.Context(), "payments", []apiclient.AssignmentSecret{kubernetesSecret("DB_USER", "user", nil)})
		var failure *deploy.Failure
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, "could not resolve secret DB_USER", failure.Reason)
		assert.Contains(t, err.Error(), "db#user")
		assert.NotContains(t, err.Error(), "hunter22")
	})

	t.Run("an unknown store kind fails by name", func(t *testing.T) {
		s := kubernetesSecret("X", "password", nil)
		s.Store.Kind = "onepassword"
		_, err := r.Resolve(t.Context(), "payments", []apiclient.AssignmentSecret{s})
		assert.ErrorContains(t, err, `this agent cannot read "onepassword" secret stores`)
	})
}

func TestWriter(t *testing.T) {
	kube := fake.NewClientset()
	w := NewWriter(kube)
	ctx := t.Context()

	name, err := w.Write(ctx, "payments", "app_01ABC", "dep_02DEF", []Resolved{
		{Name: "DB_PASSWORD", Value: "hunter22"},
		{Name: "API_KEY", Value: "in-values", AsValue: true},
	})
	require.NoError(t, err)
	assert.Equal(t, "ivaldi-app-01abc-dep-02def", name)
	secret, err := kube.CoreV1().Secrets("payments").Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)
	assert.True(t, *secret.Immutable)
	assert.Equal(t, map[string][]byte{"DB_PASSWORD": []byte("hunter22")}, secret.Data, "only secrets delivered as a Secret")

	_, err = w.Write(ctx, "payments", "app_01ABC", "dep_02DEF", []Resolved{{Name: "DB_PASSWORD", Value: "hunter22"}})
	require.NoError(t, err, "a retry keeps the existing Secret")

	// Three older Deployments' Secrets: only the newest of them is kept.
	for i, dep := range []string{"dep_a", "dep_b", "dep_c"} {
		_, err := kube.CoreV1().Secrets("payments").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: SecretName("app_01ABC", dep), Namespace: "payments",
			CreationTimestamp: metav1.NewTime(time.Unix(int64(1000+i), 0)),
			Labels:            map[string]string{deploy.LabelApplication: "app_01ABC", labelDeployment: dep},
		}}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	require.NoError(t, w.Prune(ctx, "payments", "app_01ABC", "dep_02DEF"))
	list, err := kube.CoreV1().Secrets("payments").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	var kept []string
	for _, s := range list.Items {
		kept = append(kept, s.Labels[labelDeployment])
	}
	assert.ElementsMatch(t, []string{"dep_02DEF", "dep_c"}, kept)
}

func TestWriter_ARetryWithAChangedValueFails(t *testing.T) {
	kube := fake.NewClientset()
	w := NewWriter(kube)
	_, err := w.Write(t.Context(), "payments", "app_1", "dep_1", []Resolved{{Name: "DB_PASSWORD", Value: "old-password"}})
	require.NoError(t, err)

	// The provider rotated before the retry.
	_, err = w.Write(t.Context(), "payments", "app_1", "dep_1", []Resolved{{Name: "DB_PASSWORD", Value: "new-password"}})
	var failure *deploy.Failure
	require.ErrorAs(t, err, &failure)
	assert.Contains(t, failure.Reason, "secret DB_PASSWORD changed since an earlier attempt of this Deployment")
	assert.NotContains(t, err.Error(), "new-password")
}

func TestResolver_ReadsTheKeysForEachDeployment(t *testing.T) {
	kube := fake.NewClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "fp", Namespace: "ivaldi-system"}, Data: map[string][]byte{"v1": key(1)}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "payments"}, Data: map[string][]byte{"password": []byte("hunter22")}},
	)
	r := NewResolver(Config{Kube: kube, AgentNamespace: "ivaldi-system", FingerprintSecret: "fp", Masker: NewMasker(), Logger: slog.New(slog.DiscardHandler)})
	first, err := r.Resolve(t.Context(), "payments", []apiclient.AssignmentSecret{kubernetesSecret("DB_PASSWORD", "password", nil)})
	require.NoError(t, err)
	assert.Regexp(t, `^hmac-sha256:v1:`, first[0].Fingerprint)

	// An operator rotates: v2 added, v1 removed, while the agent runs.
	_, err = kube.CoreV1().Secrets("ivaldi-system").Update(t.Context(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "fp", Namespace: "ivaldi-system"}, Data: map[string][]byte{"v2": key(2)},
	}, metav1.UpdateOptions{})
	require.NoError(t, err)

	next, err := r.Resolve(t.Context(), "payments", []apiclient.AssignmentSecret{kubernetesSecret("DB_PASSWORD", "password", nil)})
	require.NoError(t, err)
	assert.Regexp(t, `^hmac-sha256:v2:`, next[0].Fingerprint, "the newest key is used at once")
	_, err = r.Resolve(t.Context(), "payments", []apiclient.AssignmentSecret{kubernetesSecret("DB_PASSWORD", "password", &first[0].Fingerprint)})
	assert.ErrorContains(t, err, "fingerprint key v1 is gone", "the removed key is refused at once")
}

// A store's address comes from the control plane, and logging in sends a
// service-account token there, so the agent only uses a Vault its operator
// listed.
func TestResolver_OnlyLogsInToAVaultItsOperatorListed(t *testing.T) {
	var logins int
	vault := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		logins++
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(vault.Close)
	secret := apiclient.AssignmentSecret{
		Name: "Api.Key", DeliverAs: apiclient.AssignmentSecretDeliverAsSecret,
		Store: apiclient.StoreSettings{Id: "sst_1", Kind: apiclient.StoreSettingsKindVault,
			Vault: &apiclient.VaultStore{Address: vault.URL, Mount: "kv", AuthRole: "ivaldi"}},
		Ref: apiclient.SecretRef{Path: "api"},
	}
	token := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(token, []byte("vault-audience-token"), 0o600))

	r, _, _ := newTestResolver(t, fake.NewClientset())
	r.cfg.VaultTokenFile = token
	_, err := r.Resolve(context.Background(), "payments", []apiclient.AssignmentSecret{secret})
	require.ErrorContains(t, err, "not set up to log in to")
	assert.Zero(t, logins, "nothing is sent to an address the operator did not list")
	assert.NotContains(t, r.cfg.StoreKinds(), "vault", "and the heartbeat does not claim the agent reads Vault")

	r.cfg.VaultAddresses = []string{vault.URL + "/"}
	_, err = r.Resolve(context.Background(), "payments", []apiclient.AssignmentSecret{secret})
	require.Error(t, err)
	assert.Equal(t, 1, logins, "a listed address is logged in to")
	assert.Contains(t, r.cfg.StoreKinds(), "vault")
}
