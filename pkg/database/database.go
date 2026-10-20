// Package database opens PostgreSQL connections with GORM and carries one
// transaction across the capabilities that share them.
//
// Some writes must commit together even though they belong to different
// capabilities: creating a Workspace also registers its IAM resource and
// writes an audit record. Transactor.WithinTx starts a transaction and stores
// it in the context; each capability's repository calls Conn to use that
// transaction when there is one.
//
// A transaction only spans repositories built on the same *gorm.DB. Open the
// database once per process and give that handle to every capability's
// repository, rather than letting each capability open its own pool.
//
// This package holds connection and transaction helpers only. Capabilities'
// row types and queries stay in their own packages (ADR 0005).
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Open connects to PostgreSQL. Schemas are managed by each capability's
// golang-migrate migrations, never by GORM's AutoMigrate.
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:  logger.Discard,
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

type txKey struct{}

// Transactor runs functions inside a database transaction. It is the
// PostgreSQL implementation of transaction.Transactor.
type Transactor struct {
	db *gorm.DB
}

var _ transaction.Transactor = (*Transactor)(nil)

// NewTransactor returns a Transactor for db.
func NewTransactor(db *gorm.DB) *Transactor {
	return &Transactor{db: db}
}

// WithinTx runs fn in a transaction, passing a context that carries it. If ctx
// already carries a transaction, fn joins it, so a capability can make its
// writes atomic without knowing whether its caller started a transaction.
// The transaction commits if fn returns nil and rolls back otherwise.
func (t *Transactor) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return fn(ctx)
	}
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// Conn returns the transaction carried by ctx, or db bound to ctx if there is none.
func Conn(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx
	}
	return db.WithContext(ctx)
}

// PostgreSQL error codes the repositories translate into domain errors.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
)

// UniqueViolation reports the constraint name if err is a unique violation.
func UniqueViolation(err error) (constraint string, ok bool) {
	return violation(err, codeUniqueViolation)
}

// ForeignKeyViolation reports the constraint name if err is a foreign-key violation.
func ForeignKeyViolation(err error) (constraint string, ok bool) {
	return violation(err, codeForeignKeyViolation)
}

// CheckViolation reports the constraint name if err is a check-constraint violation.
func CheckViolation(err error) (constraint string, ok bool) {
	return violation(err, codeCheckViolation)
}

func violation(err error, code string) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == code {
		return pgErr.ConstraintName, true
	}
	return "", false
}
