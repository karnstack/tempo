package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/karnstack/tempo/internal/logger"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newObservedEcho wires the same middleware chain Run uses (RequestID →
// requestLogger → Recover) onto a fresh echo with an observed zap core.
func newObservedEcho(t *testing.T) (*echo.Echo, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	l := zap.New(core)
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.RequestID())
	e.Use(requestLogger(l))
	e.Use(middleware.Recover())
	return e, logs
}

func mustEntry(t *testing.T, logs *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()
	all := logs.AllUntimed()
	if len(all) != 1 {
		t.Fatalf("want exactly one entry, got %d: %+v", len(all), all)
	}
	return all[0]
}

func fieldString(t *testing.T, e observer.LoggedEntry, key string) string {
	t.Helper()
	for _, f := range e.Context {
		if f.Key == key {
			return f.String
		}
	}
	t.Fatalf("entry has no field %q: %+v", key, e.Context)
	return ""
}

func fieldInt(t *testing.T, e observer.LoggedEntry, key string) int64 {
	t.Helper()
	for _, f := range e.Context {
		if f.Key == key {
			return f.Integer
		}
	}
	t.Fatalf("entry has no field %q: %+v", key, e.Context)
	return 0
}

func TestRequestLogger_2xxLogsAtInfo(t *testing.T) {
	e, logs := newObservedEcho(t)
	e.GET("/ok", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	entry := mustEntry(t, logs)
	if entry.Level != zapcore.InfoLevel {
		t.Errorf("level: want info, got %s", entry.Level)
	}
	if got := fieldString(t, entry, "method"); got != http.MethodGet {
		t.Errorf("method: want GET, got %q", got)
	}
	if got := fieldString(t, entry, "path"); got != "/ok" {
		t.Errorf("path: want /ok, got %q", got)
	}
	if got := fieldInt(t, entry, "status"); got != http.StatusOK {
		t.Errorf("status: want 200, got %d", got)
	}
	if rid := fieldString(t, entry, "request_id"); rid == "" {
		t.Error("request_id is empty")
	}
	if got := fieldInt(t, entry, "latency_ms"); got < 0 {
		t.Errorf("latency_ms: want ≥ 0, got %d", got)
	}
}

func TestRequestLogger_4xxLogsAtWarn(t *testing.T) {
	e, logs := newObservedEcho(t)
	// Hit an unregistered path to trigger echo's 404 handler.
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: want 404, got %d", rec.Code)
	}
	entry := mustEntry(t, logs)
	if entry.Level != zapcore.WarnLevel {
		t.Errorf("level: want warn, got %s", entry.Level)
	}
	if got := fieldInt(t, entry, "status"); got != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", got)
	}
}

func TestRequestLogger_HandlerErrorLogsAtError(t *testing.T) {
	e, logs := newObservedEcho(t)
	e.GET("/boom", func(c echo.Context) error {
		return errors.New("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	entry := mustEntry(t, logs)
	if entry.Level != zapcore.ErrorLevel {
		t.Errorf("level: want error, got %s", entry.Level)
	}
	// echo's default error handler maps a bare error to 500.
	if got := fieldInt(t, entry, "status"); got != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", got)
	}
	// The error field should be present.
	var sawErr bool
	for _, f := range entry.Context {
		if f.Key == "error" {
			sawErr = true
			break
		}
	}
	if !sawErr {
		t.Errorf("entry missing error field: %+v", entry.Context)
	}
}

func TestRequestLogger_PanicLogsAtError(t *testing.T) {
	e, logs := newObservedEcho(t)
	e.GET("/panic", func(c echo.Context) error {
		panic("kaboom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d", rec.Code)
	}
	// Recover swallows the panic and produces the 500 response, then
	// returns nil to the chain — so requestLogger logs at error via the
	// status≥500 branch (no error field expected).
	entry := mustEntry(t, logs)
	if entry.Level != zapcore.ErrorLevel {
		t.Errorf("level: want error, got %s", entry.Level)
	}
	if got := fieldInt(t, entry, "status"); got != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", got)
	}
}

func TestRequestLogger_PropagatesLoggerToContext(t *testing.T) {
	e, logs := newObservedEcho(t)

	var ctxLogger *zap.Logger
	e.GET("/ctx", func(c echo.Context) error {
		ctxLogger = logger.FromContext(c.Request().Context())
		ctxLogger.Info("from-handler")
		return c.NoContent(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/ctx", nil)
	e.ServeHTTP(httptest.NewRecorder(), req)

	if ctxLogger == nil {
		t.Fatal("handler did not see a logger in context")
	}

	all := logs.AllUntimed()
	if len(all) != 2 {
		t.Fatalf("want 2 entries (handler + access), got %d: %+v", len(all), all)
	}
	// Both entries must carry the same request_id field.
	rids := make(map[string]struct{})
	for _, e := range all {
		rid := fieldString(t, e, "request_id")
		rids[rid] = struct{}{}
	}
	if len(rids) != 1 {
		t.Errorf("handler-emitted log and access log have different request_ids: %v", rids)
	}
	for rid := range rids {
		if rid == "" {
			t.Error("request_id propagated to context is empty")
		}
	}
}
