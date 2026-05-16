package ingest

import (
	"context"
	"database/sql"
	"errors"

	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// Status is the per-connection sync health snapshot consumed by
// /api/v1/sync/status. Pointer fields are nil when no row of that kind
// exists yet.
type Status struct {
	ConnectionID int64
	Latest       *sqlitedb.SyncRun
	LastSuccess  *sqlitedb.SyncRun
	LastFailure  *sqlitedb.SyncRun
}

// StatusFor reads three indexed rows (latest, last success, last failure)
// for a connection. Missing rows are reported as nil pointers, not
// errors. Any other DB error short-circuits.
func StatusFor(ctx context.Context, q *sqlitedb.Queries, connectionID int64) (Status, error) {
	st := Status{ConnectionID: connectionID}

	if r, err := q.GetLatestSyncRun(ctx, connectionID); err == nil {
		st.Latest = &r
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}

	if r, err := q.GetLastSuccessfulSyncRun(ctx, connectionID); err == nil {
		st.LastSuccess = &r
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}

	if r, err := q.GetLastFailedSyncRun(ctx, connectionID); err == nil {
		st.LastFailure = &r
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}

	return st, nil
}
