package ingest_test

import (
	"context"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
)

// seedRun inserts a sync_run for connectionID with the given outcome.
// startedAt drives ordering; the row is also finished at startedAt+1ms so
// the GetLastFailed/Successful queries see a populated finished_at.
func seedRun(t *testing.T, q *sqlitedb.Queries, connectionID int64, startedAt time.Time, ok int64, errMsg string) sqlitedb.SyncRun {
	t.Helper()
	ctx := context.Background()
	r, err := q.StartSyncRun(ctx, sqlitedb.StartSyncRunParams{
		ConnectionID: connectionID,
		StartedAt:    startedAt,
	})
	if err != nil {
		t.Fatalf("StartSyncRun: %v", err)
	}
	finishedAt := startedAt.Add(time.Millisecond)
	if err := q.FinishSyncRun(ctx, sqlitedb.FinishSyncRunParams{
		FinishedAt: &finishedAt,
		Ok:         ok,
		Items:      0,
		Error:      errMsg,
		ID:         r.ID,
	}); err != nil {
		t.Fatalf("FinishSyncRun: %v", err)
	}
	r.FinishedAt = &finishedAt
	r.Ok = ok
	r.Error = errMsg
	return r
}

func TestStatusFor_NoRuns(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	tn := seedTenant(t, q)
	box := newBox(t)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	st, err := ingest.StatusFor(context.Background(), q, conn.ID)
	if err != nil {
		t.Fatalf("StatusFor: %v", err)
	}
	if st.ConnectionID != conn.ID {
		t.Errorf("ConnectionID = %d, want %d", st.ConnectionID, conn.ID)
	}
	if st.Latest != nil {
		t.Errorf("Latest = %+v, want nil", *st.Latest)
	}
	if st.LastSuccess != nil {
		t.Errorf("LastSuccess = %+v, want nil", *st.LastSuccess)
	}
	if st.LastFailure != nil {
		t.Errorf("LastFailure = %+v, want nil", *st.LastFailure)
	}
}

func TestStatusFor_OnlySuccesses(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	tn := seedTenant(t, q)
	box := newBox(t)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	base := time.Now().Add(-time.Hour).UTC()
	seedRun(t, q, conn.ID, base, 1, "")
	newest := seedRun(t, q, conn.ID, base.Add(time.Second), 1, "")

	st, err := ingest.StatusFor(context.Background(), q, conn.ID)
	if err != nil {
		t.Fatalf("StatusFor: %v", err)
	}
	if st.Latest == nil || st.Latest.ID != newest.ID {
		t.Errorf("Latest = %+v, want id %d", st.Latest, newest.ID)
	}
	if st.LastSuccess == nil || st.LastSuccess.ID != newest.ID {
		t.Errorf("LastSuccess = %+v, want id %d", st.LastSuccess, newest.ID)
	}
	if st.LastFailure != nil {
		t.Errorf("LastFailure = %+v, want nil", *st.LastFailure)
	}
}

func TestStatusFor_OnlyFailures(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	tn := seedTenant(t, q)
	box := newBox(t)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	base := time.Now().Add(-time.Hour).UTC()
	seedRun(t, q, conn.ID, base, 0, "old failure")
	newest := seedRun(t, q, conn.ID, base.Add(time.Second), 0, "new failure")

	st, err := ingest.StatusFor(context.Background(), q, conn.ID)
	if err != nil {
		t.Fatalf("StatusFor: %v", err)
	}
	if st.Latest == nil || st.Latest.ID != newest.ID {
		t.Errorf("Latest = %+v, want id %d", st.Latest, newest.ID)
	}
	if st.LastSuccess != nil {
		t.Errorf("LastSuccess = %+v, want nil", *st.LastSuccess)
	}
	if st.LastFailure == nil || st.LastFailure.ID != newest.ID {
		t.Errorf("LastFailure = %+v, want id %d", st.LastFailure, newest.ID)
	}
}

func TestStatusFor_Mixed(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	tn := seedTenant(t, q)
	box := newBox(t)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	base := time.Now().Add(-time.Hour).UTC()
	// Order: failure(t), success(t+1), failure(t+2), success(t+3)
	seedRun(t, q, conn.ID, base, 0, "earliest failure")
	successOld := seedRun(t, q, conn.ID, base.Add(time.Second), 1, "")
	failNewest := seedRun(t, q, conn.ID, base.Add(2*time.Second), 0, "mid failure")
	successNewest := seedRun(t, q, conn.ID, base.Add(3*time.Second), 1, "")

	st, err := ingest.StatusFor(context.Background(), q, conn.ID)
	if err != nil {
		t.Fatalf("StatusFor: %v", err)
	}
	if st.Latest == nil || st.Latest.ID != successNewest.ID {
		t.Errorf("Latest = %+v, want id %d", st.Latest, successNewest.ID)
	}
	if st.LastSuccess == nil || st.LastSuccess.ID != successNewest.ID {
		t.Errorf("LastSuccess = %+v, want id %d", st.LastSuccess, successNewest.ID)
	}
	if st.LastFailure == nil || st.LastFailure.ID != failNewest.ID {
		t.Errorf("LastFailure = %+v, want id %d", st.LastFailure, failNewest.ID)
	}
	_ = successOld
}
