---
id: 0039
slug: api-tokens
title: /api/v1/tokens CRUD with encrypted PAT
status: done
depends_on: [0015, 0018]
owner: ""
est_minutes: 75
tags: [api]
autonomy: full
skills: []
---

## Goal

Ship the GitHub PAT vault: `GET /api/v1/tokens`, `POST /api/v1/tokens`, `DELETE /api/v1/tokens/:id`. PATs are encrypted at rest with AES-256-GCM keyed off `cfg.Secret.Key` (already loaded from `TEMPO_SECRET` in 0013). The plaintext PAT is **never** returned by the API after creation — list/delete deal in opaque DTOs. Token deletion is blocked if any connection still references it (the worker scheduler in 0026 will need the row).

Scope:
- Symmetric crypto helpers (`internal/secret/`) + their tests.
- New SQL query `CountConnectionsByToken` for the 409-on-delete check.
- `internal/api/tokens/` handler package.
- Wire into `api.Run`.

Out of scope: the actual ingest worker that consumes tokens (0026), tying tokens to connections in the create flow (0040 owns connections), surfacing PAT previews (no preview column exists; the label is the user-facing identifier).

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md`
- line 78 (PATs encrypted at rest with `TEMPO_SECRET`-derived key)
- line 91 (`gh_tokens(id, tenant_id, label, encrypted_pat, scopes, expires_at)`)
- lines 213–215 (route list)
- line 287 (`TEMPO_SECRET` is the source of truth for the symmetric key)

Master-plan row: line 175.

## Design decisions

- **AES-256-GCM, output = `nonce ‖ ciphertext`.** Standard authenticated encryption from `crypto/aes` + `crypto/cipher`. 12-byte nonce, random per encrypt. Single-flat-blob format keeps the storage column simple and means decrypting only needs the key.
- **`internal/secret/` is its own package, not under `internal/auth/`.** Auth deals with humans (passwords, sessions, login). PAT-at-rest is a different concern that will also be used by the ingest worker (0026) and future credentials. Putting it in its own package keeps both boundaries clean.
- **`*secret.Box` carries the key, not raw `[]byte`.** Constructor validates key length once (32 bytes for AES-256). fx-provided. Handlers and the ingest worker take `*secret.Box`, never raw key material.
- **Tenant scoping via `q.GetUser(sess.UserID)`.** Same pattern as `/me`. v1 has one tenant so this is one extra query in the happy path. We do **not** stuff `tenant_id` into the session row or context yet — when a second tenant arrives we revisit. (Could also be a future `WithCurrentUser` middleware; not warranted for two endpoints.)
- **`DELETE` is 409 if connections reference the token.** No FK constraint at the DB level (per the project memory: enforce in Go). Add `CountConnectionsByToken` query and reject the delete with 409 when count > 0. Body returns `{"error":"token in use","connection_count":N}` so the SPA can guide the user to the connections page.
- **No PAT preview.** The user-facing identifier is `label`. Adding a preview column means decrypt-on-list (or storing last-4 in plaintext, leaking metadata). Skip until users ask. The settings UI distinguishes by label.
- **`expires_at` is just metadata.** GitHub doesn't surface expiry to the API consumer; we trust the user to set it. We do not auto-expire or block use of an expired token — the ingest worker will see the 401 from GitHub and surface that as a connection-status error in 0026.
- **Validation: label non-empty after trim, PAT non-empty.** No prefix check on PAT — GitHub has `ghp_*`, `github_pat_*`, fine-grained tokens, and Apps. Reject by length / emptiness only. Strip whitespace from PAT (paste-from-browser tends to add a trailing newline) before encrypting.
- **No update endpoint.** Tokens are write-once, delete-and-recreate. PATs aren't rotatable in place — relabel + replace is what users actually do, and skipping `PUT` keeps the surface small.
- **Adding `secret.NewBoxFx` to `cmd/tempo/main.go`.** First time `*secret.Box` is provided to fx; future PAT consumers (0026) get it for free.

## Acceptance criteria

- [ ] `internal/secret/secret.go` exports `Box`, `NewBox(key []byte) (*Box, error)`, `(*Box).Encrypt(plaintext []byte) ([]byte, error)`, `(*Box).Decrypt(ciphertext []byte) ([]byte, error)`.
- [ ] `internal/secret/fx.go` exports `NewBoxFx(*config.Config) (*Box, error)`.
- [ ] `internal/secret/secret_test.go` covers: round-trip, distinct nonces yield distinct ciphertexts for same input, tampered ciphertext fails, wrong key fails, empty plaintext OK, NewBox rejects non-32-byte keys.
- [ ] `internal/storage/sqlite/queries/connections.sql` gains `CountConnectionsByToken :one`. `make sqlc-generate` produces clean updates to `connections.sql.go` and `querier.go`.
- [ ] `internal/api/tokens/tokens.go` exports `Configure(e *echo.Echo, l *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries, box *secret.Box)` mounting:
  - `GET /api/v1/tokens` → 200 `{tokens:[TokenDTO,...]}` (tenant-scoped)
  - `POST /api/v1/tokens` → 201 `{token: TokenDTO}` on success; 400 on validation failure
  - `DELETE /api/v1/tokens/:id` → 204 on success; 404 if not found / wrong tenant; 409 if connections ref the token
  - All three behind `web.RequireSession(m)`.
- [ ] `TokenDTO` is `{id, label, scopes, expires_at, created_at}`. No PAT, no encrypted_pat.
- [ ] `cmd/tempo/main.go` provides `secret.NewBoxFx`.
- [ ] `internal/api/run.go` takes `*secret.Box` and threads it into `tokens.Configure`.
- [ ] Behavioural tests in `internal/api/tokens/tokens_test.go`:
  - **POST happy** — 201, response shape, encrypted_pat in DB decrypts back to the input PAT.
  - **POST trims whitespace** on PAT and label.
  - **POST empty label** → 400.
  - **POST empty PAT** → 400.
  - **POST malformed JSON** → 400.
  - **POST no cookie** → 401.
  - **POST with expires_at** → roundtrips through DTO.
  - **GET happy** — multiple tokens, sorted by created_at ASC, no PAT exposure.
  - **GET no cookie** → 401.
  - **DELETE happy** → 204; row gone.
  - **DELETE missing id** → 404.
  - **DELETE wrong tenant** would be ideal but v1 only has one tenant — assert that other-tenant rows aren't returned by GET (covered above) and skip the DELETE-cross-tenant case with a `// TODO` if needed. (Trivially: insert a token under tenant_id=999 directly, attempt DELETE → 404. That's clean.)
  - **DELETE referenced by connection** → 409, body `{error:"token in use", connection_count:1}`. Token row still exists.
  - **DELETE no cookie** → 401.
- [ ] `go vet ./...`, `go build ./...`, `go test ./internal/... -race -count=1` all pass.
- [ ] `verify.sh` exits 0.

## Files to touch

- `internal/secret/secret.go` (new)
- `internal/secret/secret_test.go` (new)
- `internal/secret/fx.go` (new)
- `internal/storage/sqlite/queries/connections.sql` (add `CountConnectionsByToken`)
- `internal/storage/sqlite/sqlitedb/connections.sql.go` (regenerated by sqlc)
- `internal/storage/sqlite/sqlitedb/querier.go` (regenerated by sqlc)
- `internal/api/tokens/tokens.go` (new)
- `internal/api/tokens/tokens_test.go` (new)
- `internal/api/run.go` (add `*secret.Box` param + `tokens.Configure`)
- `cmd/tempo/main.go` (add `secret.NewBoxFx` to fx.Provide)
- `.plans/upnext/0039-api-tokens/verify.sh` (replace stub)

## Steps

### 1. Build the secret package

`internal/secret/secret.go`:

```go
// Package secret hosts symmetric encryption helpers used to protect
// at-rest credentials (today: GitHub PATs). The key is a 32-byte symmetric
// key derived from TEMPO_SECRET; the wire format is nonce ‖ ciphertext.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

const keyLen = 32 // AES-256

// Box wraps a 32-byte AES key and exposes Encrypt/Decrypt for short blobs.
// Safe for concurrent use — the underlying cipher is constructed once.
type Box struct {
	gcm cipher.AEAD
}

// NewBox validates the key length and constructs the AEAD. The key is
// retained only inside the cipher block; callers can zero their copy
// after this returns.
func NewBox(key []byte) (*Box, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("secret: key must be %d bytes, got %d", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: gcm: %w", err)
	}
	return &Box{gcm: gcm}, nil
}

// Encrypt returns nonce ‖ ciphertext. A fresh random nonce is generated
// per call so the same plaintext encrypts to different bytes each time.
func (b *Box) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret: random nonce: %w", err)
	}
	ct := b.gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// ErrCipherTooShort is returned when the input cannot possibly hold a
// nonce + at least an empty AEAD tag. Distinct from auth-failure so tests
// can tell them apart.
var ErrCipherTooShort = errors.New("secret: ciphertext too short")

// Decrypt splits the input into nonce + ciphertext and returns the
// authenticated plaintext. Returns an error on tamper or wrong key.
func (b *Box) Decrypt(blob []byte) ([]byte, error) {
	ns := b.gcm.NonceSize()
	if len(blob) < ns+b.gcm.Overhead() {
		return nil, ErrCipherTooShort
	}
	nonce, ct := blob[:ns], blob[ns:]
	pt, err := b.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("secret: open: %w", err)
	}
	return pt, nil
}
```

`internal/secret/fx.go`:

```go
package secret

import "github.com/karnstack/tempo/internal/config"

// NewBoxFx is the fx adapter that builds a *Box from cfg.Secret.Key.
func NewBoxFx(cfg *config.Config) (*Box, error) {
	return NewBox(cfg.Secret.Key)
}
```

Commit: `feat(secret): AES-256-GCM Box for at-rest credential encryption`

### 2. Test the secret package

`internal/secret/secret_test.go`:

```go
package secret_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/karnstack/tempo/internal/secret"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return k
}

func TestNewBox_RejectsShortKey(t *testing.T) {
	if _, err := secret.NewBox(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	b, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	plain := []byte("ghp_secrettokenvalue1234567890")
	ct, err := b.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := b.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("decrypted = %q, want %q", got, plain)
	}
}

func TestEncrypt_DistinctNonces(t *testing.T) {
	b, _ := secret.NewBox(newKey(t))
	a, _ := b.Encrypt([]byte("same"))
	c, _ := b.Encrypt([]byte("same"))
	if bytes.Equal(a, c) {
		t.Fatal("two encrypts of same plaintext produced identical ciphertext (nonce reuse?)")
	}
}

func TestDecrypt_TamperFails(t *testing.T) {
	b, _ := secret.NewBox(newKey(t))
	ct, _ := b.Encrypt([]byte("hello"))
	ct[len(ct)-1] ^= 0xff // flip a byte in the auth tag
	if _, err := b.Decrypt(ct); err == nil {
		t.Fatal("expected tamper to fail decrypt")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	b1, _ := secret.NewBox(newKey(t))
	b2, _ := secret.NewBox(newKey(t))
	ct, _ := b1.Encrypt([]byte("hello"))
	if _, err := b2.Decrypt(ct); err == nil {
		t.Fatal("expected wrong-key decrypt to fail")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	b, _ := secret.NewBox(newKey(t))
	if _, err := b.Decrypt([]byte{1, 2, 3}); !errors.Is(err, secret.ErrCipherTooShort) {
		t.Fatalf("err = %v, want ErrCipherTooShort", err)
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	b, _ := secret.NewBox(newKey(t))
	ct, err := b.Encrypt(nil)
	if err != nil {
		t.Fatalf("Encrypt(nil): %v", err)
	}
	got, err := b.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %q, want empty", got)
	}
}
```

Run:

```
go test ./internal/secret/... -race -count=1
```

Commit: `test(secret): AES-GCM round-trip, tamper, wrong-key, too-short`

### 3. Add CountConnectionsByToken query

Append to `internal/storage/sqlite/queries/connections.sql`:

```sql
-- name: CountConnectionsByToken :one
SELECT COUNT(*) FROM connections WHERE token_id = @token_id;
```

Regenerate sqlc:

```
make sqlc-generate
```

Verify build:

```
go build ./...
```

Commit: `feat(storage): CountConnectionsByToken for token-delete safety`

### 4. Build the tokens handler

`internal/api/tokens/tokens.go`:

```go
// Package tokens hosts the /api/v1/tokens CRUD: list, create with PAT
// encryption, delete with referential safety against connections.
package tokens

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/karnstack/tempo/internal/api/web"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"
)

// TokenDTO is the wire shape — never carries the PAT.
type TokenDTO struct {
	ID        int64      `json:"id"`
	Label     string     `json:"label"`
	Scopes    string     `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type ListTokensResponse struct {
	Tokens []TokenDTO `json:"tokens"`
}

type CreateTokenRequest struct {
	Label     string     `json:"label"`
	PAT       string     `json:"pat"`
	Scopes    string     `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type CreateTokenResponse struct {
	Token TokenDTO `json:"token"`
}

type DeleteConflictResponse struct {
	Error           string `json:"error"`
	ConnectionCount int64  `json:"connection_count"`
}

// Configure mounts /api/v1/tokens behind RequireSession.
func Configure(e *echo.Echo, _ *zap.Logger, m *intauth.Manager, q *sqlitedb.Queries, box *secret.Box) {
	g := e.Group("/api/v1", web.RequireSession(m))
	g.GET("/tokens", listHandler(q))
	g.POST("/tokens", createHandler(q, box))
	g.DELETE("/tokens/:id", deleteHandler(q))
}

func listHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}
		rows, err := q.ListGhTokensByTenant(ctx.Request().Context(), tenantID)
		if err != nil {
			ctx.L.Error("list tokens failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "list failed")
		}
		out := make([]TokenDTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, dtoFrom(r))
		}
		return ctx.JSON(http.StatusOK, ListTokensResponse{Tokens: out})
	})
}

func createHandler(q *sqlitedb.Queries, box *secret.Box) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		var req CreateTokenRequest
		if err := ctx.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		req.Label = strings.TrimSpace(req.Label)
		req.PAT = strings.TrimSpace(req.PAT)
		req.Scopes = strings.TrimSpace(req.Scopes)
		if req.Label == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "label is required")
		}
		if req.PAT == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "pat is required")
		}

		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}

		ct, err := box.Encrypt([]byte(req.PAT))
		if err != nil {
			ctx.L.Error("encrypt pat failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "encrypt failed")
		}

		row, err := q.CreateGhToken(ctx.Request().Context(), sqlitedb.CreateGhTokenParams{
			TenantID:     tenantID,
			Label:        req.Label,
			EncryptedPat: ct,
			Scopes:       req.Scopes,
			ExpiresAt:    req.ExpiresAt,
		})
		if err != nil {
			ctx.L.Error("create token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "create failed")
		}

		return ctx.JSON(http.StatusCreated, CreateTokenResponse{Token: dtoFrom(row)})
	})
}

func deleteHandler(q *sqlitedb.Queries) echo.HandlerFunc {
	return web.WrapPublic(func(ctx *web.Context) error {
		idStr := ctx.Param("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "id must be an integer")
		}

		tenantID, err := tenantIDFromSession(ctx, q)
		if err != nil {
			return err
		}

		row, err := q.GetGhToken(ctx.Request().Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "token not found")
		}
		if err != nil {
			ctx.L.Error("get token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		if row.TenantID != tenantID {
			return echo.NewHTTPError(http.StatusNotFound, "token not found")
		}

		count, err := q.CountConnectionsByToken(ctx.Request().Context(), id)
		if err != nil {
			ctx.L.Error("count connections by token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
		}
		if count > 0 {
			return ctx.JSON(http.StatusConflict, DeleteConflictResponse{
				Error:           "token in use",
				ConnectionCount: count,
			})
		}

		if err := q.DeleteGhToken(ctx.Request().Context(), id); err != nil {
			ctx.L.Error("delete token failed", zap.Error(err))
			return echo.NewHTTPError(http.StatusInternalServerError, "delete failed")
		}
		return ctx.NoContent(http.StatusNoContent)
	})
}

func tenantIDFromSession(ctx *web.Context, q *sqlitedb.Queries) (int64, error) {
	sess, ok := intauth.FromContext(ctx.Request().Context())
	if !ok {
		return 0, echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
	}
	user, err := q.GetUser(ctx.Request().Context(), sess.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, echo.NewHTTPError(http.StatusUnauthorized, "unauthorized")
	}
	if err != nil {
		ctx.L.Error("get user failed", zap.Error(err))
		return 0, echo.NewHTTPError(http.StatusInternalServerError, "lookup failed")
	}
	return user.TenantID, nil
}

func dtoFrom(r sqlitedb.GhToken) TokenDTO {
	return TokenDTO{
		ID:        r.ID,
		Label:     r.Label,
		Scopes:    r.Scopes,
		ExpiresAt: r.ExpiresAt,
		CreatedAt: r.CreatedAt,
	}
}
```

Commit: `feat(api): /api/v1/tokens CRUD with PAT encryption`

### 5. Wire into router + main fx graph

Edit `internal/api/run.go`: add `*secret.Box` param to `Run` and `configureRoutes`, add the `tokens.Configure(e, l, m, q, box)` call. Imports: `tokens` package + `secret`.

Edit `cmd/tempo/main.go`: add `secret.NewBoxFx` to the `fx.Provide(...)` list.

Build:

```
go build ./...
```

Commit: `feat(api): wire tokens.Configure + secret.Box into fx graph`

### 6. Write integration tests

`internal/api/tokens/tokens_test.go`. Mirror the `me_test.go` harness (real SQLite + migrations + small Echo with auth + tokens mounted). The encryption box uses a fixed 32-byte key per test. Test cases hit each path in the acceptance criteria.

Sketch:

```go
package tokens_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiauth "github.com/karnstack/tempo/internal/api/auth"
	"github.com/karnstack/tempo/internal/api/tokens"
	intauth "github.com/karnstack/tempo/internal/auth"
	"github.com/karnstack/tempo/internal/config"
	"github.com/karnstack/tempo/internal/secret"
	"github.com/karnstack/tempo/internal/storage/sqlite"
	"github.com/karnstack/tempo/internal/storage/sqlite/sqlitedb"
	"github.com/karnstack/tempo/migrations"
	"github.com/labstack/echo/v4"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap/zaptest"
)

const (
	seedEmail    = "admin@acme.test"
	seedPassword = "hunter22hunter22"
)

func newIntegrationStore(t *testing.T) *sqlitedb.Queries {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	l := zaptest.NewLogger(t)
	path := filepath.Join(t.TempDir(), "tokens_integration.db")
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

func newTokensEcho(t *testing.T, q *sqlitedb.Queries) (*echo.Echo, *intauth.Manager, *secret.Box) {
	t.Helper()
	m := intauth.NewManager(q, time.Hour, false)
	r := intauth.NewRegistrar(q)
	a := intauth.NewAuthenticator(q)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	l := zaptest.NewLogger(t)
	apiauth.Configure(e, l, m, r, a)
	tokens.Configure(e, l, m, q, box)
	return e, m, box
}

func seedAndLogin(t *testing.T, e http.Handler, q *sqlitedb.Queries) *http.Cookie {
	t.Helper()
	if _, err := intauth.NewRegistrar(q).Register(context.Background(), seedEmail, seedPassword); err != nil {
		t.Fatalf("seed Register: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"`+seedEmail+`","password":"`+seedPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == intauth.CookieName {
			return c
		}
	}
	t.Fatal("login did not set session cookie")
	return nil
}

func doJSON(t *testing.T, e http.Handler, method, path string, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	var req *http.Request
	if rdr != nil {
		req = httptest.NewRequest(method, path, rdr)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestPostTokens_HappyPath_201(t *testing.T) {
	t.Parallel()
	q := newIntegrationStore(t)
	e, _, box := newTokensEcho(t, q)
	cookie := seedAndLogin(t, e, q)

	rec := doJSON(t, e, http.MethodPost, "/api/v1/tokens", cookie,
		`{"label":"main","pat":"ghp_secrettoken1234567890","scopes":"repo,read:org"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp tokens.CreateTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token.Label != "main" {
		t.Errorf("label = %q", resp.Token.Label)
	}
	if resp.Token.ID == 0 {
		t.Error("id = 0")
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("ghp_")) {
		t.Errorf("response body leaks PAT: %s", rec.Body.String())
	}

	// PAT decrypts back from DB.
	row, err := q.GetGhToken(context.Background(), resp.Token.ID)
	if err != nil {
		t.Fatalf("GetGhToken: %v", err)
	}
	plain, err := box.Decrypt(row.EncryptedPat)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "ghp_secrettoken1234567890" {
		t.Errorf("decrypted = %q", string(plain))
	}
}

func TestPostTokens_TrimsWhitespace(t *testing.T) { /* … */ }
func TestPostTokens_EmptyLabel_400(t *testing.T)   { /* … */ }
func TestPostTokens_EmptyPAT_400(t *testing.T)     { /* … */ }
func TestPostTokens_BadJSON_400(t *testing.T)      { /* … */ }
func TestPostTokens_NoCookie_401(t *testing.T)     { /* … */ }
func TestPostTokens_WithExpiresAt(t *testing.T)    { /* … */ }
func TestGetTokens_Multiple_200(t *testing.T)      { /* … */ }
func TestGetTokens_NoCookie_401(t *testing.T)      { /* … */ }
func TestDeleteToken_Happy_204(t *testing.T)       { /* … */ }
func TestDeleteToken_Missing_404(t *testing.T)     { /* … */ }
func TestDeleteToken_OtherTenant_404(t *testing.T) { /* … */ }
func TestDeleteToken_InUse_409(t *testing.T)       { /* … */ }
func TestDeleteToken_NoCookie_401(t *testing.T)    { /* … */ }
```

Fill in the bodies — most are 5–10 lines. The 409 case needs to insert a connection row referencing the token before attempting delete; do that via `q.CreateConnection` directly.

Run:

```
go test ./internal/api/tokens/... -race -count=1
```

Commit: `test(api): /tokens CRUD coverage incl. encrypt round-trip + 409`

### 7. Replace verify.sh

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./..."
go vet ./...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/secret/... ./internal/api/... -race -count=1"
go test ./internal/secret/... ./internal/api/... -race -count=1
echo "  ok"

echo "VERIFY OK"
```

### 8. Run verify

```
./.plans/upnext/0039-api-tokens/verify.sh
```

## Notes

- Encrypted blobs grow by `gcm.NonceSize() + gcm.Overhead()` = 12 + 16 = 28 bytes vs. plaintext. SQLite BLOB column has no size impact.
- `expires_at` stays `*time.Time` end-to-end via the `emit_pointers_for_null_types: true` sqlc setting.
- The `tenantIDFromSession` helper duplicates a small slice of the `me` handler's logic. We could factor it into `web.CurrentUser(ctx, q)` later, but with two endpoints right now the duplication is two lines per handler — DRY is premature.
- DELETE returns the conflict count so the SPA can show "this token is used by 2 connections — remove them first." We deliberately don't list the connection IDs to keep the response cheap; the SPA can navigate to /connections to find them.
- The handler trims whitespace before validating — both `\n` from clipboard paste and accidental leading spaces. We do not strip from `scopes` aggressively (a single trim).
- Future: when the ingest worker (0026) lands, it will need a `*secret.Box` to decrypt `encrypted_pat` per token before constructing each `github.Client`. The `NewBoxFx` provider added here wires it up for free.
