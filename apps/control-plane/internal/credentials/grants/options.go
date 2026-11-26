package grants

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/federation"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type Option func(*service)

func WithAssignments(a Assignments) Option {
	return func(s *service) { s.assignments = a }
}

func WithApplications(a Applications) Option {
	return func(s *service) { s.applications = a }
}

func WithGrantRepo(r credentials.SecretStoreGrantRepository) Option {
	return func(s *service) { s.grants = r }
}

// WithIssuer signs federated grants. Without it, a federated store is never
// granted.
func WithIssuer(i federation.Issuer) Option {
	return func(s *service) { s.issuer = i }
}

// WithStoreRepo reads the brokered stores a Deployment reads, to mint for.
func WithStoreRepo(r credentials.SecretStoreRepository) Option {
	return func(s *service) { s.stores = r }
}

// WithBroker mints brokered stores' credentials, with the minting
// credentials keeper opens. Without both, a brokered store is never granted.
func WithBroker(b credentials.Broker, keeper credentials.CredentialKeeper) Option {
	return func(s *service) { s.broker, s.keeper = b, keeper }
}

func WithTransactor(tx transaction.Transactor) Option {
	return func(s *service) { s.w.Tx = tx }
}

func WithAuditRecorder(r audit.Recorder) Option {
	return func(s *service) { s.w.Audit = r }
}
