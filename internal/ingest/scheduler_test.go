package ingest_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/github"
	"github.com/karnstack/tempo/internal/ingest"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// --- fakes ---

type fakeRunner struct {
	name  string
	calls int32
	out   ingest.Outcome
	err   error
	seen  chan sqlitedb.Connection
}

func (r *fakeRunner) Name() string { return r.name }
func (r *fakeRunner) Run(_ context.Context, conn sqlitedb.Connection, _ *github.Client) (ingest.Outcome, error) {
	atomic.AddInt32(&r.calls, 1)
	if r.seen != nil {
		select {
		case r.seen <- conn:
		default:
		}
	}
	return r.out, r.err
}

// --- helpers ---

func newIntegrationStore(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "ingest_integration.db")
	cfg := &config.Config{Database: config.Database{
		Driver: "sqlite", DSN: path, Raw: "sqlite://" + path,
	}}
	s, err := sqlite.New(lc, l, cfg)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	if err := migrations.Apply(context.Background(), s.DB()); err != nil {
		t.Fatalf("migrations.Apply: %v", err)
	}
	lc.RequireStart()
	t.Cleanup(lc.RequireStop)
	return sqlitedb.New(s.DB())
}

func newBox(t *testing.T) *secret.Box {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	b, err := secret.NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	return b
}

func seedTenant(t *testing.T, q *sqlitedb.Queries) int64 {
	t.Helper()
	tn, err := q.CreateTenant(context.Background(), "acme")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	return tn.ID
}

func seedToken(t *testing.T, q *sqlitedb.Queries, box *secret.Box, tenantID int64, plaintext string) int64 {
	t.Helper()
	ct, err := box.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	tok, err := q.CreateGhToken(context.Background(), sqlitedb.CreateGhTokenParams{
		TenantID:     tenantID,
		Label:        "test",
		EncryptedPat: ct,
		Scopes:       "repo",
	})
	if err != nil {
		t.Fatalf("CreateGhToken: %v", err)
	}
	return tok.ID
}

func seedConnection(t *testing.T, q *sqlitedb.Queries, tenantID, tokenID int64, status, owner string, name *string) sqlitedb.Connection {
	t.Helper()
	c, err := q.CreateConnection(context.Background(), sqlitedb.CreateConnectionParams{
		TenantID:     tenantID,
		Kind:         "repo",
		Owner:        owner,
		Name:         name,
		TokenID:      tokenID,
		BackfillFrom: time.Now().Add(-90 * 24 * time.Hour).UTC(),
		Status:       status,
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	return c
}

func strPtr(s string) *string { return &s }

func newScheduler(t *testing.T, q *sqlitedb.Queries, box *secret.Box, runners []ingest.Runner, opts ...ingest.Option) *ingest.Scheduler {
	t.Helper()
	cfg := &config.Config{Poll: config.Poll{Interval: 50 * time.Millisecond, BackfillDays: 90}}
	stubBuilder := ingest.WithClientBuilder(func(string, *zap.Logger) *github.Client {
		return github.New("test-token")
	})
	all := append([]ingest.Option{stubBuilder}, opts...)
	return ingest.New(zaptest.NewLogger(t), cfg, q, box, runners, all...)
}

// --- tests ---

func TestTick_NoConnections(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	s := newScheduler(t, q, box, nil)
	s.Tick(context.Background())

	// No connections → no sync_runs anywhere. (No tenant created either,
	// so this also guards against the ListActive query needing a tenant.)
}

func TestTick_OneConnection_NoRunners_OkRunRecorded(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	s := newScheduler(t, q, box, nil)
	s.Tick(context.Background())

	runs, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: conn.ID,
		LimitN:       10,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	r := runs[0]
	if r.Ok != 1 {
		t.Errorf("ok = %d, want 1", r.Ok)
	}
	if r.Items != 0 {
		t.Errorf("items = %d, want 0", r.Items)
	}
	if r.Error != "" {
		t.Errorf("error = %q, want empty", r.Error)
	}
	if r.FinishedAt == nil {
		t.Fatal("finished_at is nil")
	}

	got, err := q.GetConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if got.LastSyncAt == nil {
		t.Fatal("last_sync_at not advanced")
	}
}

func TestTick_HappyPathRunner_CapturesOutcome(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	remain := int64(4999)
	r := &fakeRunner{
		name: "prs",
		out:  ingest.Outcome{Items: 42, RateLimitRemaining: &remain},
		seen: make(chan sqlitedb.Connection, 1),
	}
	s := newScheduler(t, q, box, []ingest.Runner{r})
	s.Tick(context.Background())

	if got := atomic.LoadInt32(&r.calls); got != 1 {
		t.Fatalf("runner calls = %d, want 1", got)
	}
	select {
	case c := <-r.seen:
		if c.ID != conn.ID {
			t.Errorf("runner saw connection_id %d, want %d", c.ID, conn.ID)
		}
	default:
		t.Fatal("runner did not record a connection")
	}

	runs, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: conn.ID, LimitN: 1,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	if runs[0].Ok != 1 {
		t.Errorf("ok = %d, want 1", runs[0].Ok)
	}
	if runs[0].Items != 42 {
		t.Errorf("items = %d, want 42", runs[0].Items)
	}
	if runs[0].RateLimitRemaining == nil || *runs[0].RateLimitRemaining != 4999 {
		t.Errorf("rate_limit_remaining = %v, want 4999", runs[0].RateLimitRemaining)
	}
}

func TestTick_MultipleRunners_SumItems_MinRemaining_Order(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	r1Remain := int64(4900)
	r2Remain := int64(4500)
	var order []string
	var mu sync.Mutex
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
	}

	r1 := &fakeRunner{name: "prs", out: ingest.Outcome{Items: 10, RateLimitRemaining: &r1Remain}}
	r2 := &fakeRunner{name: "reviews", out: ingest.Outcome{Items: 5, RateLimitRemaining: &r2Remain}}
	wrap := func(name string, base *fakeRunner) ingest.Runner {
		return runnerFn{
			name: name,
			run: func(ctx context.Context, c sqlitedb.Connection, gh *github.Client) (ingest.Outcome, error) {
				record(name)
				return base.Run(ctx, c, gh)
			},
		}
	}
	s := newScheduler(t, q, box, []ingest.Runner{wrap("prs", r1), wrap("reviews", r2)})
	s.Tick(context.Background())

	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "prs" || got[1] != "reviews" {
		t.Fatalf("order = %v, want [prs reviews]", got)
	}

	runs, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: conn.ID, LimitN: 1,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection: %v", err)
	}
	if runs[0].Items != 15 {
		t.Errorf("items = %d, want 15", runs[0].Items)
	}
	if runs[0].RateLimitRemaining == nil || *runs[0].RateLimitRemaining != 4500 {
		t.Errorf("rate_limit_remaining = %v, want 4500 (min)", runs[0].RateLimitRemaining)
	}
}

func TestTick_RunnerError_ShortCircuits_NoLastSyncBump(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	r1 := &fakeRunner{name: "prs", out: ingest.Outcome{Items: 10}}
	r2 := &fakeRunner{name: "reviews", err: errors.New("boom")}
	r3 := &fakeRunner{name: "commits"}

	s := newScheduler(t, q, box, []ingest.Runner{r1, r2, r3})
	s.Tick(context.Background())

	if atomic.LoadInt32(&r1.calls) != 1 {
		t.Errorf("r1 calls = %d, want 1", r1.calls)
	}
	if atomic.LoadInt32(&r2.calls) != 1 {
		t.Errorf("r2 calls = %d, want 1", r2.calls)
	}
	if atomic.LoadInt32(&r3.calls) != 0 {
		t.Errorf("r3 calls = %d, want 0 (short-circuit)", r3.calls)
	}

	runs, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: conn.ID, LimitN: 1,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection: %v", err)
	}
	if runs[0].Ok != 0 {
		t.Errorf("ok = %d, want 0", runs[0].Ok)
	}
	if runs[0].Items != 10 {
		t.Errorf("items = %d, want 10 (partial)", runs[0].Items)
	}
	wantPrefix := "reviews: "
	if got := runs[0].Error; len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Errorf("error = %q, want prefix %q", got, wantPrefix)
	}

	got, err := q.GetConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if got.LastSyncAt != nil {
		t.Errorf("last_sync_at = %v, want nil (no advance on error)", got.LastSyncAt)
	}
}

func TestTick_TokenMissing_RecordsErrorOnSyncRun(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	// Create connection pointing to a non-existent token id.
	conn := seedConnection(t, q, tn, 9999, "active", "octo", strPtr("hello"))

	s := newScheduler(t, q, box, []ingest.Runner{&fakeRunner{name: "prs"}})
	s.Tick(context.Background())

	runs, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: conn.ID, LimitN: 1,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	if runs[0].Ok != 0 {
		t.Errorf("ok = %d, want 0", runs[0].Ok)
	}
	if runs[0].Error == "" {
		t.Fatal("error empty, want token lookup failure")
	}

	got, err := q.GetConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("GetConnection: %v", err)
	}
	if got.LastSyncAt != nil {
		t.Errorf("last_sync_at = %v, want nil", got.LastSyncAt)
	}
}

func TestTick_SkipsInactiveConnections(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	inactive := seedConnection(t, q, tn, tok, "paused", "octo", strPtr("paused-repo"))
	active := seedConnection(t, q, tn, tok, "active", "octo", strPtr("active-repo"))

	r := &fakeRunner{name: "prs"}
	s := newScheduler(t, q, box, []ingest.Runner{r})
	s.Tick(context.Background())

	if got := atomic.LoadInt32(&r.calls); got != 1 {
		t.Fatalf("runner calls = %d, want 1 (only active)", got)
	}

	runsInactive, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: inactive.ID, LimitN: 1,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection inactive: %v", err)
	}
	if len(runsInactive) != 0 {
		t.Errorf("inactive runs = %d, want 0", len(runsInactive))
	}
	runsActive, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: active.ID, LimitN: 1,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection active: %v", err)
	}
	if len(runsActive) != 1 {
		t.Errorf("active runs = %d, want 1", len(runsActive))
	}
}

// runnerFn adapts a closure into a Runner. Used in the order-tracking test.
type runnerFn struct {
	name string
	run  func(context.Context, sqlitedb.Connection, *github.Client) (ingest.Outcome, error)
}

func (r runnerFn) Name() string { return r.name }
func (r runnerFn) Run(ctx context.Context, c sqlitedb.Connection, gh *github.Client) (ingest.Outcome, error) {
	return r.run(ctx, c, gh)
}

func TestLoop_TicksImmediately_AndOnInterval(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	r := &fakeRunner{name: "prs"}
	s := newScheduler(t, q, box, []ingest.Runner{r})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.Loop(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		if atomic.LoadInt32(&r.calls) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("runner calls = %d after 2s, want >= 2 (immediate + at least one ticker fire)", r.calls)
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not exit after cancel")
	}
}

func TestLoop_CtxCancelMidTick_RecordsCanceledRun(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	box := newBox(t)
	tn := seedTenant(t, q)
	tok := seedToken(t, q, box, tn, "ghp_secret")
	conn := seedConnection(t, q, tn, tok, "active", "octo", strPtr("hello"))

	entered := make(chan struct{})
	unblock := make(chan struct{})
	blocking := runnerFn{
		name: "prs",
		run: func(ctx context.Context, _ sqlitedb.Connection, _ *github.Client) (ingest.Outcome, error) {
			close(entered)
			select {
			case <-ctx.Done():
				return ingest.Outcome{}, ctx.Err()
			case <-unblock:
				return ingest.Outcome{}, nil
			}
		},
	}
	s := newScheduler(t, q, box, []ingest.Runner{blocking})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Loop(ctx)
		close(done)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("runner did not enter")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(unblock)
		t.Fatal("loop did not exit after cancel")
	}

	runs, err := q.ListSyncRunsByConnection(context.Background(), sqlitedb.ListSyncRunsByConnectionParams{
		ConnectionID: conn.ID, LimitN: 5,
	})
	if err != nil {
		t.Fatalf("ListSyncRunsByConnection: %v", err)
	}
	if len(runs) == 0 {
		t.Fatal("no sync_runs recorded; expected the in-flight one to be closed")
	}
	r := runs[0]
	if r.Ok != 0 {
		t.Errorf("ok = %d, want 0", r.Ok)
	}
	if r.FinishedAt == nil {
		t.Error("finished_at is nil; expected canceled run to be finalised")
	}
	if r.Error == "" {
		t.Error("error is empty; expected cancel error message")
	}
}
