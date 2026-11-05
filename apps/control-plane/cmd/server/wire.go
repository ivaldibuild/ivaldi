package server

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	outboxpg "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// app is what serve runs, built from the configuration. It is the one place
// concrete implementations are chosen; everything else depends on
// interfaces.
type app struct {
	db     *gorm.DB
	tx     transaction.Transactor
	outbox outbox.Store
	routes []api.Routes
}

// wire opens the database and builds the capabilities. The caller closes the
// database with close once the server has stopped.
func wire(ctx context.Context, cfg *config.Config, logger logging.Logger) (a *app, close func() error, err error) {
	db, close, err := openDatabase(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = close()
		}
	}()

	a = &app{
		db:     db,
		tx:     database.NewTransactor(db),
		outbox: outboxpg.NewRepository(db),
	}
	logger.Info("database connected")
	return a, close, nil
}

// openDatabase connects with the configured pool and checks the connection,
// so a wrong address fails at start rather than at the first request.
func openDatabase(ctx context.Context, cfg *config.Config) (*gorm.DB, func() error, error) {
	dsn, err := cfg.DatabaseURL()
	if err != nil {
		return nil, nil, err
	}
	maxPool, minPool, idleTimeout, connectTimeout, err := cfg.DatabasePoolOptions()
	if err != nil {
		return nil, nil, err
	}

	db, err := database.Open(dsn)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxPool)
	sqlDB.SetMaxIdleConns(minPool)
	sqlDB.SetConnMaxIdleTime(idleTimeout)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("connect to database: %w", err)
	}
	return db, sqlDB.Close, nil
}
