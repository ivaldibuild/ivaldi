package server

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance/changes"
	provenancepg "github.com/ivaldibuild/ivaldi-control-plane/internal/provenance/postgres"
)

// wireProvenance builds the provenance capability's Change service and HTTP
// API. Delivery reads recorded Changes through it, so it is wired before
// delivery.
func (a *app) wireProvenance() error {
	service, err := changes.NewChangeService(
		changes.WithChangeRepo(provenancepg.NewChangeRepository(a.db)),
		changes.WithAuthorizer(a.authz),
		changes.WithTransactor(a.tx),
		changes.WithOutboxRecorder(a.outbox),
		changes.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.changes = service
	a.services.changes = service
	return nil
}
