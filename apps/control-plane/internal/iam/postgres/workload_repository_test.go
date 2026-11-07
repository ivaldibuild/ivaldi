package postgres

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

func TestWorkloadRepository(t *testing.T) {
	f := newFixture(t)
	clock.Fix(t, t0)
	workloads := NewWorkloadRepository(f.db)
	org := f.writeOrg(t, "acme")
	w := must(iam.NewWorkload(iam.NewWorkloadProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "Runtime agent"},
		Component:      iam.ComponentRuntimeAgent, Pool: "target:one",
	}, nil))
	require.NoError(t, workloads.Write(f.ctx, w))
	got, err := workloads.Get(f.ctx, org.ID(), w.ID())
	require.NoError(t, err)
	assert.Equal(t, w.Snapshot(), got.Snapshot())

	clock.Fix(t, t0.Add(time.Minute))
	w.Seen()
	require.NoError(t, workloads.Write(f.ctx, w))
	got, err = workloads.Get(f.ctx, org.ID(), w.ID())
	require.NoError(t, err)
	assert.Equal(t, w.Snapshot(), got.Snapshot())

	t.Run("recording contact leaves a concurrent suspension in place", func(t *testing.T) {
		stale, err := workloads.Get(f.ctx, org.ID(), w.ID())
		require.NoError(t, err)
		suspended, err := workloads.Get(f.ctx, org.ID(), w.ID())
		require.NoError(t, err)
		require.NoError(t, suspended.Suspend())
		require.NoError(t, workloads.Write(f.ctx, suspended))

		clock.Fix(t, t0.Add(2*time.Minute))
		stale.Seen()
		require.NoError(t, workloads.UpdateLastSeen(f.ctx, stale))
		got, err := workloads.Get(f.ctx, org.ID(), w.ID())
		require.NoError(t, err)
		assert.Equal(t, iam.PrincipalSuspended, got.Status())
		assert.Equal(t, stale.LastSeenAt(), got.LastSeenAt())
	})
}

func TestWorkloadEnrollmentTokenRepository(t *testing.T) {
	f := newFixture(t)
	clock.Fix(t, t0)
	tokens := NewWorkloadEnrollmentTokenRepository(f.db)
	org := f.writeOrg(t, "acme")
	target := "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	creator := newUser(org.ID(), f.writeAccount(t, "ada@example.com").ID())
	require.NoError(t, f.users.Write(f.ctx, creator))
	secret := must(iam.NewWorkloadEnrollmentTokenSecret())
	token := must(iam.NewWorkloadEnrollmentToken(iam.NewWorkloadEnrollmentTokenProps{
		OrganizationID: org.ID(), TokenHash: secret.Hash, Pool: "target:" + target,
		TargetID: &target, CredentialKind: iam.CredentialMTLSCertificate,
		CreatedByPrincipalID: creator.ID(),
		ExpiresAt:            t0.Add(time.Hour),
	}, nil))
	require.NoError(t, tokens.Write(f.ctx, token))

	got, err := tokens.GetByHashForUpdate(f.ctx, secret.Hash)
	require.NoError(t, err)
	assert.Equal(t, token.Snapshot(), got.Snapshot())
	assert.Equal(t, iam.CredentialMTLSCertificate, got.CredentialKind())
	assert.Equal(t, target, *got.TargetID())
}

func TestWorkloadCredentialRepository(t *testing.T) {
	f := newFixture(t)
	clock.Fix(t, t0)
	credentials := NewWorkloadCredentialRepository(f.db)
	org := f.writeOrg(t, "acme")
	w := must(iam.NewWorkload(iam.NewWorkloadProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "Runtime agent"},
		Component:      iam.ComponentRuntimeAgent, Pool: "target:one",
	}, nil))
	require.NoError(t, NewWorkloadRepository(f.db).Write(f.ctx, w))
	secret := must(iam.NewWorkloadTokenSecret())
	expires := t0.Add(24 * time.Hour)
	cred := must(iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: org.ID(), PrincipalID: w.ID(), Kind: iam.CredentialBearerToken,
		Identifier: secret.Prefix, SecretHash: secret.Hash, NotAfter: &expires,
	}, nil))
	require.NoError(t, credentials.Write(f.ctx, cred))

	got, err := credentials.Get(f.ctx, org.ID(), cred.ID())
	require.NoError(t, err)
	assert.Equal(t, cred.Snapshot(), got.Snapshot())
	byHash, err := credentials.GetBySecretHash(f.ctx, secret.Hash)
	require.NoError(t, err)
	assert.Equal(t, cred.ID(), byHash.ID())

	list, err := credentials.ListByPrincipal(f.ctx, org.ID(), w.ID())
	require.NoError(t, err)
	require.Len(t, list, 1)

	clock.Fix(t, t0.Add(time.Hour))
	nextExpiry := t0.Add(25 * time.Hour)
	next := must(iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: org.ID(), PrincipalID: w.ID(), Kind: iam.CredentialBearerToken,
		Identifier: "iwt_next", SecretHash: []byte("01234567890123456789012345678901"),
		NotAfter: &nextExpiry,
	}, nil))
	require.NoError(t, credentials.Write(f.ctx, next))
	require.NoError(t, cred.Supersede(next.ID(), t0.Add(5*time.Minute)))
	require.NoError(t, credentials.Write(f.ctx, cred))
	got, err = credentials.Get(f.ctx, org.ID(), cred.ID())
	require.NoError(t, err)
	assert.Equal(t, next.ID(), *got.ReplacedBy())
	assert.Equal(t, t0.Add(5*time.Minute), *got.NotAfter())
}

func TestWorkloadCredentialRepositoryReenrolment(t *testing.T) {
	f := newFixture(t)
	clock.Fix(t, t0)
	credentials := NewWorkloadCredentialRepository(f.db)
	workloads := NewWorkloadRepository(f.db)
	org := f.writeOrg(t, "acme")
	first := must(iam.NewWorkload(iam.NewWorkloadProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "First agent"},
		Component:      iam.ComponentRuntimeAgent, Pool: "target:one",
	}, nil))
	require.NoError(t, workloads.Write(f.ctx, first))
	expires := t0.Add(24 * time.Hour)
	old := must(iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: org.ID(), PrincipalID: first.ID(), Kind: iam.CredentialMTLSCertificate,
		Identifier: "same-fingerprint", NotAfter: &expires,
	}, nil))
	require.NoError(t, credentials.Write(f.ctx, old))

	// Re-enrolment presents the same certificate: the old row is revoked and
	// kept for lineage, and a new one is registered.
	old.Revoke()
	require.NoError(t, credentials.Write(f.ctx, old))
	clock.Fix(t, t0.Add(time.Minute))
	second := must(iam.NewWorkload(iam.NewWorkloadProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: org.ID(), DisplayName: "Second agent"},
		Component:      iam.ComponentRuntimeAgent, Pool: "target:one",
	}, nil))
	require.NoError(t, workloads.Write(f.ctx, second))
	nextExpiry := t0.Add(25 * time.Hour)
	next := must(iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: org.ID(), PrincipalID: second.ID(), Kind: iam.CredentialMTLSCertificate,
		Identifier: "same-fingerprint", NotAfter: &nextExpiry,
	}, nil))
	require.NoError(t, credentials.Write(f.ctx, next))

	got, err := credentials.GetByKindAndIdentifier(f.ctx, iam.CredentialMTLSCertificate, "same-fingerprint")
	require.NoError(t, err)
	assert.Equal(t, next.ID(), got.ID())
	assert.Equal(t, second.ID(), got.PrincipalID())

	// A second live credential for the same identity is refused: an identity
	// must name one workload.
	clock.Fix(t, t0.Add(2*time.Minute))
	clash := must(iam.NewWorkloadCredential(iam.NewWorkloadCredentialProps{
		OrganizationID: org.ID(), PrincipalID: first.ID(), Kind: iam.CredentialMTLSCertificate,
		Identifier: "same-fingerprint", NotAfter: &nextExpiry,
	}, nil))
	assert.ErrorIs(t, credentials.Write(f.ctx, clash), iam.ErrAlreadyExists)
}
