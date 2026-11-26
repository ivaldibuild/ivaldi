// Package brokers mints and revokes the credentials brokered Secret Stores
// are read with (ADR 0017). It is the only code that holds stores' minting
// credentials in the clear, one file per kind, so it needs the same care as
// the agent's masking: nothing here logs, records, or returns a minting
// credential, and its errors never contain one.
package brokers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
)

// requestTimeout bounds each call to a store, so an agent waiting on a
// grant is answered.
const requestTimeout = 15 * time.Second

// New returns the Broker for every kind that can be brokered.
func New(opts ...Option) credentials.Broker {
	o := options{
		client:     &http.Client{Timeout: requestTimeout, CheckRedirect: noRedirect},
		dopplerAPI: secretstore.DefaultDopplerAPI,
		roles:      awsRoles{},
	}
	for _, opt := range opts {
		opt(&o)
	}
	return &broker{kinds: map[secretstore.Kind]credentials.Broker{
		secretstore.KindAWSSecretsManager: &awsBroker{roles: o.roles},
		secretstore.KindVault:             &vaultBroker{client: o.client},
		secretstore.KindDoppler:           &dopplerBroker{client: o.client, api: o.dopplerAPI},
		secretstore.KindInfisical:         &infisicalBroker{client: o.client},
	}}
}

// noRedirect keeps a minting credential from following a reply elsewhere.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

type broker struct {
	kinds map[secretstore.Kind]credentials.Broker
}

func (b *broker) of(store *credentials.SecretStore) (credentials.Broker, error) {
	if store.Access().Mode != credentials.AccessBrokered {
		return nil, credentials.NewErrInvalidInput(fmt.Sprintf("secret store %s is in %s mode, not brokered", store.ID(), store.Access().Mode))
	}
	k, ok := b.kinds[store.Kind()]
	if !ok {
		return nil, credentials.NewErrInvalidInput(fmt.Sprintf("a %s store cannot be brokered", store.Kind()))
	}
	return k, nil
}

func (b *broker) Mint(ctx context.Context, r credentials.MintRequest) (credentials.Minted, error) {
	k, err := b.of(r.Store)
	if err != nil {
		return credentials.Minted{}, err
	}
	m, err := k.Mint(ctx, r)
	if err != nil {
		return credentials.Minted{}, fmt.Errorf("secret store %s: %w", r.Store.ID(), err)
	}
	return m, nil
}

func (b *broker) Revoke(ctx context.Context, r credentials.RevokeRequest) (bool, error) {
	k, err := b.of(r.Store)
	if err != nil {
		return false, err
	}
	revoked, err := k.Revoke(ctx, r)
	if err != nil {
		return false, fmt.Errorf("secret store %s: %w", r.Store.ID(), err)
	}
	return revoked, nil
}
