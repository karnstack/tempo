---
id: 0015
slug: argon2id-password-hashing
title: Argon2id password hashing module
status: done
depends_on: [0013]
owner: ""
est_minutes: 25
tags: [auth]
autonomy: full
skills: []
---

## Goal

Ship the password hashing primitive that 0017 (`/auth/register`) and 0018 (`/auth/login`) will use to store and verify the admin user's password. Self-contained, dependency-light: just `Hash` and `Verify` on top of `golang.org/x/crypto/argon2` with sensible Argon2id defaults and the standard PHC-encoded output format.

The module is intentionally tiny — no DB, no users table, no echo handlers. Those land in 0017+. This task delivers the building block.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` line 62 (Argon2id named as the password algorithm), line 73-74 (first-run admin uses Argon2id-hashed password). `golang.org/x/crypto` is already in `go.mod` as indirect (pulled by zap/echo); after this task it becomes a direct dep.

## Acceptance criteria

- [ ] `internal/auth/password.go` exports:
  - `Params` struct with fields `Memory uint32` (KiB), `Iterations uint32`, `Parallelism uint8`, `SaltLen uint32`, `KeyLen uint32`.
  - `DefaultParams` matching OWASP 2024 guidance for Argon2id: `Memory: 64 * 1024` (64 MiB), `Iterations: 3`, `Parallelism: 2`, `SaltLen: 16`, `KeyLen: 32`.
  - `Hash(password string) (string, error)` — uses `DefaultParams`. Returns PHC-formatted `$argon2id$v=19$m=…,t=…,p=…$<b64salt>$<b64hash>` (raw-stdencoding base64, no padding, per the de-facto PHC convention).
  - `HashWithParams(password string, p Params) (string, error)` — same but with caller-supplied params (kept exported for tuning/tests).
  - `Verify(password, encoded string) (bool, error)` — parses the PHC string, recomputes, and uses `subtle.ConstantTimeCompare`. Returns `(false, nil)` for a clean password mismatch; `(false, err)` for a malformed/foreign-algo/unsupported-version encoded string.
- [ ] `Hash("")` returns an error (we never store empty admin passwords).
- [ ] `Verify` rejects: empty `encoded`, missing/extra `$` fields, algorithm != `argon2id`, version != `19`, unparseable `m=`/`t=`/`p=` triple, base64 errors, salt or hash of zero length.
- [ ] `Verify` round-trips against hashes produced with non-default params (forward compat: parameters are read from the encoded string, not assumed).
- [ ] `internal/auth/password_test.go` covers:
  - Round-trip with `DefaultParams`: `Hash("hunter2")` → `Verify("hunter2", h)` → `(true, nil)`.
  - Wrong password: `Verify("nope", h)` → `(false, nil)`.
  - Tampered hash (flip a base64 char in the final segment): `(false, nil)`.
  - `Hash("")` returns a non-nil error.
  - Each malformed-encoded case: empty string, wrong algorithm prefix (`$argon2i$…`), wrong version (`v=18`), missing fields, bad base64 in salt, bad base64 in hash. Each returns `(false, err)`.
  - Round-trip with custom small params (`Memory: 8 * 1024, Iterations: 1, Parallelism: 1`) — keeps the test fast and exercises the param-from-string path.
  - Two `Hash` calls for the same password produce different encoded strings (salt randomness).
- [ ] `go vet ./internal/auth/...`, `go build ./...`, `go test ./internal/auth/... -count=1` all pass.
- [ ] `go.mod` shows `golang.org/x/crypto` as a direct dep (no `// indirect`) after `go mod tidy`.

## Files to touch

- `internal/auth/password.go` (new)
- `internal/auth/password_test.go` (new)
- `go.mod` / `go.sum` (auto-updated by `go mod tidy`)
- `.plans/upnext/0015-argon2id-password-hashing/verify.sh` (replace stub)

## Steps

### 1. Add the hashing module

Create `internal/auth/password.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params controls the cost of a single Argon2id hash.
type Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

// DefaultParams follows OWASP 2024 guidance for Argon2id: 64 MiB memory,
// 3 iterations, 2 lanes. ~80ms on a modern laptop — heavy enough to make
// offline cracking expensive without making the admin login flow feel slow.
var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLen:     16,
	KeyLen:      32,
}

// ErrEmptyPassword is returned by Hash when the input is empty. We never
// store an empty admin password — surface it as an error so the caller has
// to reject it explicitly.
var ErrEmptyPassword = errors.New("auth: password is empty")

// Hash returns a PHC-formatted Argon2id hash of password using DefaultParams.
func Hash(password string) (string, error) {
	return HashWithParams(password, DefaultParams)
}

// HashWithParams is Hash with caller-supplied cost. Useful for tests and
// future tuning. Production code should call Hash.
func HashWithParams(password string, p Params) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		enc.EncodeToString(salt), enc.EncodeToString(key),
	), nil
}

// Verify reports whether password matches the PHC-encoded Argon2id hash.
// Returns (false, nil) for a clean mismatch; (false, err) for malformed
// input. Uses subtle.ConstantTimeCompare so the timing channel doesn't
// leak partial matches.
func Verify(password, encoded string) (bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLen)
	if subtle.ConstantTimeCompare(candidate, key) == 1 {
		return true, nil
	}
	return false, nil
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>" splits into 6 parts with
	// an empty leading element.
	if len(parts) != 6 {
		return Params{}, nil, nil, errors.New("auth: malformed argon2 hash: wrong field count")
	}
	if parts[1] != "argon2id" {
		return Params{}, nil, nil, fmt.Errorf("auth: unsupported algorithm %q (want argon2id)", parts[1])
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: parse version: %w", err)
	}
	if version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("auth: unsupported argon2 version %d (want %d)", version, argon2.Version)
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: parse params: %w", err)
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: decode salt: %w", err)
	}
	if len(salt) == 0 {
		return Params{}, nil, nil, errors.New("auth: empty salt")
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: decode hash: %w", err)
	}
	if len(key) == 0 {
		return Params{}, nil, nil, errors.New("auth: empty hash")
	}
	p.SaltLen = uint32(len(salt))
	p.KeyLen = uint32(len(key))
	return p, salt, key, nil
}
```

Commit: `feat(auth): argon2id password hashing with PHC encoding`

### 2. Add the test file

Create `internal/auth/password_test.go` with the cases listed in Acceptance criteria. Use small params (`Memory: 8*1024, Iterations: 1, Parallelism: 1`) for the round-trip-with-custom-params case; default-param round-trip happens once (it's the slow one — keep it to a single call so `go test` stays under a couple of seconds).

Commit: `test(auth): argon2id round-trip and malformed-input rejection`

### 3. Tidy modules

```
go mod tidy
```

This promotes `golang.org/x/crypto` from indirect to direct since `internal/auth/password.go` now imports it.

Commit: `chore(go): promote golang.org/x/crypto to direct dep`

### 4. Replace verify.sh

Replace the stub with a script that runs vet/build/test for the auth package and the whole tree:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

echo "==> go vet ./internal/auth/..."
go vet ./internal/auth/...
echo "  ok"

echo "==> go build ./..."
go build ./...
echo "  ok"

echo "==> go test ./internal/auth/... -count=1"
go test ./internal/auth/... -count=1
echo "  ok"

echo "==> go test ./... (no regressions)"
go test ./... -count=1
echo "  ok"

echo "==> golang.org/x/crypto is a direct dep"
if grep -E '^\s*golang\.org/x/crypto\s+v[0-9]+\.[0-9]+\.[0-9]+\s*$' go.mod >/dev/null; then
  echo "  ok"
else
  echo "FAIL: golang.org/x/crypto is not a direct dep in go.mod" >&2
  grep -n 'golang.org/x/crypto' go.mod >&2 || true
  exit 1
fi

echo "VERIFY OK"
```

### 5. Run verify

```
./.plans/upnext/0015-argon2id-password-hashing/verify.sh
```

## Notes

- We use `RawStdEncoding` (no padding) to match the de-facto PHC convention used by `argon2-cffi`, `passlib`, `node-argon2`, and the reference C implementation. It also makes the encoded string ~2 chars shorter per segment.
- We don't expose a "rehash on outdated params" helper here. When 0017/0018 wire this into the login flow, that helper can live in the login handler — keeping the primitive narrow.
- `argon2.Version` is `0x13` (decimal 19); the encoded string carries the literal integer `19` per spec.
- We deliberately don't add an exported `Decode` function. The PHC parser is an internal detail; callers go through `Verify`.
- The 64 MiB / 3-iter default is for a self-hosted instance with a real CPU. If we ever need to tune down for low-memory deploys, that's a `TEMPO_AUTH_*` env var added at the same time as the login handler — not now.
