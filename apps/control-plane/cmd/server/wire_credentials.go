package server

import (
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/brokers"
	credentialspg "github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/revocation"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/events/redisstream"
	"github.com/ivaldibuild/ivaldi/pkg/kms"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// wireBrokering loads the key-encryption key brokered Secret Stores'
// minting credentials are kept with, when the installation has one. Without
// it, no store can be in brokered mode.
func (a *app) wireBrokering(cfg *config.Config) error {
	enc := cfg.Encryption
	switch {
	case enc.Provider != config.EncryptionLocal:
		return fmt.Errorf("encryption.provider %s is not supported yet; use local with encryption.local_key_file", enc.Provider)
	case enc.LocalKeyFile == "":
		return nil
	}
	kek, err := kms.LoadLocal(enc.LocalKeyFile)
	if err != nil {
		return fmt.Errorf("encryption.local_key_file: %w", err)
	}
	repo := credentialspg.NewSecretStoreCredentialRepository(a.db)
	if a.keeper, err = credentials.NewCredentialKeeper(kek, repo, repo); err != nil {
		return err
	}
	a.broker = brokers.New()
	return nil
}

// wireRevocation revokes brokered grants when their Deployment leaves
// applying: a consumer reads the outbox stream with its own group. It runs
// only where stores can be brokered.
func (a *app) wireRevocation(cfg *config.Config, logger logging.Logger) error {
	if a.keeper == nil {
		return nil
	}
	handler, err := revocation.NewHandler(
		revocation.WithGrantRepo(credentialspg.NewSecretStoreGrantRepository(a.db)),
		revocation.WithStoreRepo(credentialspg.NewSecretStoreRepository(a.db)),
		revocation.WithBroker(a.broker, a.keeper),
		revocation.WithTransactor(a.tx),
		revocation.WithAuditRecorder(a.audit),
		revocation.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	var router events.Router
	handler.Register(&router)
	name, err := consumerName(cfg)
	if err != nil {
		return err
	}
	rev := cfg.SecretStores.Revocation
	a.revocationEvents, err = redisstream.NewConsumer(redisstream.ConsumerConfig{
		Client:           a.redis,
		Stream:           cfg.Outbox.Stream,
		Group:            rev.ConsumerGroup,
		Consumer:         name,
		Handler:          &router,
		Batch:            100,
		Concurrency:      rev.Concurrency,
		BlockTimeout:     cfg.Events.BlockTimeout,
		ClaimIdleTimeout: cfg.Events.ClaimIdleTimeout,
		MaxDeliveries:    cfg.Events.MaxDeliveries,
		DeadLetterStream: rev.DeadLetterStream,
		Logger:           logger,
	})
	if err != nil {
		return fmt.Errorf("secret store revocation consumer: %w", err)
	}
	return nil
}
