// Package transaction defines how capabilities make writes commit together.
//
// It holds only the interface, with no database code, so domain packages can
// depend on it without importing a database driver (ADR 0005). The
// PostgreSQL implementation is database.Transactor in pkg/database.
package transaction

import "context"

// Transactor runs fn in a transaction carried by the context fn receives, so
// writes by several capabilities commit or roll back together.
//
// The guarantee holds only if every write in fn joins that transaction:
//   - Repositories and other capabilities' implementations must write through
//     the context they are given (with pkg/database, through database.Conn).
//   - They must share one database handle, opened once per process.
//
// If ctx already carries a transaction, fn joins it rather than starting a
// new one, so a capability can make its writes atomic without knowing whether
// its caller started a transaction. The transaction commits if fn returns nil
// and rolls back otherwise.
type Transactor interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
