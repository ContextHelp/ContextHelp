package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// querier is what a store runs its statements on: the database, or the
// transaction an InTx put in the context.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txKey keys the transaction inTx carries in a context.
type txKey struct{}

// boundTx is a transaction and the database it belongs to: a store of
// another database ignores it.
type boundTx struct {
	db *sql.DB
	tx *sql.Tx
}

// conn returns the transaction ctx carries for db, or db itself.
func conn(ctx context.Context, db *sql.DB) querier {
	if b, ok := ctx.Value(txKey{}).(*boundTx); ok && b.db == db {
		return b.tx
	}
	return db
}

// inTx runs fn in one transaction on db, committing when fn returns nil
// and rolling back on an error or a panic. A ctx that already carries a
// transaction on db joins it: fn runs in it and the outer inTx commits.
// The DSN's _txlock=immediate takes the write lock at BEGIN, so
// transactions that read before they write never race each other.
func inTx(ctx context.Context, db *sql.DB, fn func(ctx context.Context) error) error {
	if b, ok := ctx.Value(txKey{}).(*boundTx); ok && b.db == db {
		return fn(ctx)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// After Commit, Rollback is a no-op that reports sql.ErrTxDone.
	defer func() { _ = tx.Rollback() }()
	if err := fn(context.WithValue(ctx, txKey{}, &boundTx{db: db, tx: tx})); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// inTransaction reports whether ctx carries a transaction on db.
func inTransaction(ctx context.Context, db *sql.DB) bool {
	b, ok := ctx.Value(txKey{}).(*boundTx)
	return ok && b.db == db
}
