# Result — 0015-argon2id-password-hashing

## What changed

- `internal/auth/password.go` (new) — `Hash`, `HashWithParams`, `Verify`, `Params`, `DefaultParams` (OWASP 2024: 64 MiB / 3 iter / 2 lanes / 16-byte salt / 32-byte key), `ErrEmptyPassword`. PHC-encoded output: `$argon2id$v=19$m=…,t=…,p=…$<b64salt>$<b64hash>` (raw-stdencoding base64). `Verify` reads params from the encoded string for forward compatibility and uses `subtle.ConstantTimeCompare`.
- `internal/auth/password_test.go` (new) — round-trip with `DefaultParams` and with custom small params, wrong password, tampered hash, empty-password rejection (both `Hash` and `HashWithParams`), salt randomness, and 11 malformed-encoded cases (empty, wrong algorithm, wrong version, missing/extra fields, bad params triple, bad version, bad-base64 salt/hash, empty salt/hash).
- `go.mod` — `golang.org/x/crypto` promoted from indirect to direct after `go mod tidy`.
- `.plans/upnext/0015-argon2id-password-hashing/verify.sh` — replaced stub with vet/build/test/`go test ./...` regression check + a grep that asserts `golang.org/x/crypto` is direct.

## Verify output (last lines)

```
==> go vet ./internal/auth/...
  ok
==> go build ./...
  ok
==> go test ./internal/auth/... -count=1
ok  	github.com/karnstack/tempo/internal/auth	0.578s
  ok
==> go test ./... (no regressions)
ok  	github.com/karnstack/tempo/internal/api	2.281s
ok  	github.com/karnstack/tempo/internal/auth	0.623s
ok  	github.com/karnstack/tempo/internal/config	0.909s
ok  	github.com/karnstack/tempo/internal/logger	1.505s
ok  	github.com/karnstack/tempo/internal/storage/sqlite	2.857s
  ok
==> golang.org/x/crypto is a direct dep
  ok
VERIFY OK
```

## Followups

- 0017 (`/auth/register` + first-run gate) is the natural next consumer. It will need a tiny "rehash-if-params-outdated" helper at the login site — out of scope here, lives with the login handler.
- Argon2id default cost (~80ms per hash on a modern CPU) is intentional. If self-hosters running on tiny VMs report login latency, expose a `TEMPO_AUTH_ARGON2_*` triple alongside the login handler — not now.
