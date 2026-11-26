package revocation

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type Option func(*Handler)

func WithGrantRepo(r credentials.SecretStoreGrantRepository) Option {
	return func(h *Handler) { h.grants = r }
}

func WithStoreRepo(r credentials.SecretStoreRepository) Option {
	return func(h *Handler) { h.stores = r }
}

// WithBroker revokes what was minted, with the minting credentials keeper
// opens.
func WithBroker(b credentials.Broker, keeper credentials.CredentialKeeper) Option {
	return func(h *Handler) { h.broker, h.keeper = b, keeper }
}

func WithTransactor(tx transaction.Transactor) Option { return func(h *Handler) { h.w.Tx = tx } }

func WithAuditRecorder(r audit.Recorder) Option { return func(h *Handler) { h.w.Audit = r } }

func WithLogger(l logging.Logger) Option { return func(h *Handler) { h.logger = l } }
