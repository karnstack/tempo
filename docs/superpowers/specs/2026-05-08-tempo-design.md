# tempo — design spec

**Date:** 2026-05-08
**Repo:** `github.com/karnstack/tempo`
**Status:** draft, awaiting approval before `writing-plans`

## What is tempo

An open-source engineering metrics tool for GitHub. The pitch: every engineer can run their own
instance, point it at a repo or org, and see the same data their manager sees in tools like
GetDX — commits, PR throughput, cycle time, review latency, DORA metrics. Transparency is the
differentiator: there is no admin-only view in v1.

v1 ships as a single Go binary with an embedded React/TanStack SPA, default SQLite storage, and
a built-in ingest worker that talks to the GitHub API directly.

## Non-goals (v1)

- No SaaS. No multi-tenancy in practice (schema is multi-tenant-ready, instance has one tenant).
- No GitLab, Bitbucket, Jira, Linear, or any non-GitHub source.
- No live "raw query" interface on top of GitHub. Everything is snapshotted.
- No mobile app.
- No custom dashboards / metric builder. Dashboards are fixed in v1.

## Target users

- An engineer who wants their own visibility into their team's data without asking IT to buy GetDX.
- An eng manager at a small/mid-sized team who wants the data without paying per-seat.
- An OSS maintainer monitoring contributor cadence and review backlog.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  tempo (single Go binary)                                │
│                                                          │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐  │
│  │ HTTP server  │   │ Ingest worker│   │ Rollup worker│  │
│  │  - REST API  │   │  - GraphQL   │   │  - daily agg │  │
│  │  - SPA serve │   │  - REST+ETag │   │  - idempotent│  │
│  │    (embed.FS)│   │  - rate-limit│   │    re-run    │  │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘  │
│         │                  │                  │          │
│         └──────────────────┴──────────────────┘          │
│                            │                             │
│                  ┌─────────▼─────────┐                   │
│                  │  storage iface    │                   │
│                  │ ─── SQLite (default)                  │
│                  │ ─── Postgres (future, same iface)     │
│                  └───────────────────┘                   │
└──────────────────────────────────────────────────────────┘

Frontend (Vite + TanStack Router SPA)
  → built into web/dist
  → embedded into Go binary via embed.FS
  → served at "/" by HTTP server
```

Three goroutines in one process:

1. **HTTP server** (chi or echo). Serves `/api/v1/*` and the embedded SPA (with SPA-fallback for
   client-side routes). Cookie sessions, server-validated. Argon2id password hashing.
2. **Ingest worker**. Tickers (default 15m) walk active connections, hit GitHub via GraphQL
   (preferred) and REST (where GraphQL gaps exist), persist raw events.
3. **Rollup worker**. Once per day at 02:00 instance-local, aggregates raw events into
   `daily_*` tables. Idempotent: rerunning a day's rollup overwrites it from raw events.

Storage is fronted by a single `Storage` interface so SQLite is v1's default and Postgres can be
added later without changing call sites. SQLite is the priority — Postgres is a future add.

## Auth model

- First-run: `/register` shows when no users exist. Creates the admin (email + Argon2id-hashed
  password). After that, `/register` is hidden and only `/login` is reachable.
- v1 has one user but the schema is multi-user (`users.tenant_id`, `users.role`). Adding more
  users in a future version is additive, not a rewrite.
- GitHub auth is via Personal Access Tokens (and optionally GitHub App installs in v1.1). Tokens
  are encrypted at rest using a key derived from `TEMPO_SECRET`.
- Sessions are server-side rows; cookie carries a random session id. No JWTs.

## Data model (SQLite-first, Postgres-compatible)

All tables carry `tenant_id INTEGER NOT NULL` (single-row in v1). Times are `INTEGER` Unix
seconds. SQL is mostly portable; a small `dialect.go` handles UPSERT and `now()` divergences.

### Identity & config

- `tenants(id, name, created_at)` — one row in v1.
- `users(id, tenant_id, email, password_hash, role, created_at)`
- `sessions(id, user_id, expires_at)`
- `gh_tokens(id, tenant_id, label, encrypted_pat, scopes, expires_at)`
- `connections(id, tenant_id, kind, owner, name, token_id, backfill_from, status, last_sync_at)`
  — `kind ∈ {repo, org}`; `name` nullable for orgs.
- `repos(id, tenant_id, connection_id, gh_id, owner, name, default_branch, archived, added_at)`
- `gh_users(id, tenant_id, gh_id, login, name, avatar_url, last_seen_at)` — distinct from `users`.

### Raw events (append-only, source of truth)

- `commits(repo_id, sha PK, author_gh_user_id, committer_gh_user_id, authored_at, additions, deletions, message)`
- `pull_requests(repo_id, number, gh_id, author_gh_user_id, state, title, created_at, merged_at, closed_at, additions, deletions, base_ref, head_ref, draft)` — PK `(repo_id, number)`.
- `pr_reviews(gh_id PK, pr_repo_id, pr_number, reviewer_gh_user_id, state, submitted_at)`
- `pr_review_comments(gh_id PK, pr_repo_id, pr_number, author_gh_user_id, created_at)`
- `pr_issue_comments(gh_id PK, pr_repo_id, pr_number, author_gh_user_id, created_at)`
- `deployments(gh_id PK, repo_id, environment, ref, sha, status, created_at)` — sourced from
  GitHub Deployments + Releases.

### Daily rollups (read path for the dashboard)

- `daily_engineer_stats(date, repo_id, gh_user_id, commits, prs_opened, prs_merged, reviews_given, comments, additions, deletions)` — PK `(date, repo_id, gh_user_id)`.
- `daily_repo_stats(date, repo_id, prs_opened, prs_merged, prs_closed, deploys, lead_time_seconds_p50, lead_time_seconds_p90)`
- `daily_review_latency(date, repo_id, time_to_first_review_seconds_p50, p90, count)`
- `daily_review_load(date, repo_id, reviewer_gh_user_id, reviews, response_minutes_p50)`

### Sync state

- `sync_runs(id, connection_id, started_at, finished_at, ok, items, rate_limit_remaining, error)`
- `sync_cursors(connection_id, resource, cursor, updated_at)`

### Migration tooling

`goose` for SQL migrations (`migrations/0001_init.up.sql` etc.). `sqlc` for typed Go queries.

## Ingest strategy

GitHub's PAT rate limit is 5,000 requests/hour for REST and 5,000 points/hour for GraphQL.
tempo stays well under by:

- **GraphQL-first**: a single PR query pulls reviews, review comments, commits, labels, and
  timeline together — typically 1–2 GraphQL points per PR vs. 5+ REST requests.
- **Cursor-based incremental sync**: after first backfill, polls use `since=<last_sync_at>` for
  commits and `updatedAt > <cursor>` for PRs/issues. Only changed data flows.
- **Conditional REST**: ETag / `If-Modified-Since` on remaining REST endpoints — 304 responses
  do not consume the rate limit.
- **Backoff-aware scheduler**: after every call, the worker reads `X-RateLimit-Remaining` and
  pauses until reset if remaining < 200.
- **Bounded backfill**: default 90 days back when adding a connection (configurable via
  `TEMPO_BACKFILL_DAYS`).
- **Webhooks (v1.1, not v1)**: when the instance has a public URL, users can wire a GitHub
  webhook for near-real-time events; polling becomes a safety net.

Polling cadence: 15 min by default (`TEMPO_POLL_INTERVAL=15m`). Daily rollup runs at 02:00
instance-local.

Resources fetched (v1):
- Commits (per default branch + per PR head ref).
- Pull requests (open, merged, closed) with cursors on `updatedAt`.
- Reviews and review comments per PR.
- Issue comments on PRs.
- Deployments and Releases.

## Frontend

**Stack**: Vite + React 19 + TanStack Router (file-based) + TanStack Query + Tailwind v4 +
**shadcn/ui** (added via the shadcn CLI; components live at `web/src/components/ui/`). Icons via
**lucide-react** (the shadcn default). Charts via shadcn's **`Chart`** component (Recharts under
the hood) — one chart system, registry-managed.

**shadcn setup**

- Initialize via `pnpm dlx shadcn@latest init --template vite --base radix` from `web/`. This
  creates `components.json`, sets up Tailwind v4 with `@theme inline` blocks, and wires the
  `@/` import alias.
- Style preset: **`nova`** (the modern default). Locked in at init.
- Add components on demand: `pnpm dlx shadcn@latest add button card dialog table sidebar chart`
  etc. Don't write custom UI when a registry component exists.
- Conventions enforced by the skill rules and CI:
  - Forms use `FieldGroup` + `Field` (never raw `div` + `Label`).
  - Spacing uses `gap-*` (no `space-y-*` / `space-x-*`).
  - Equal dimensions use `size-*` (no `w-* h-*`).
  - Status colors use `Badge` variants and semantic tokens (`bg-primary`,
    `text-muted-foreground`) — never raw color classes.
  - Dialogs/Sheets always carry a Title (visually hidden if needed).
  - `Empty` for empty states, `Skeleton` for loaders, `sonner` for toasts, `Separator` for
    rules, `Alert` for callouts.

**Routes**

- `/register` — first-run only.
- `/login`
- `/` → `/dashboard`
- `/dashboard` — global overview across all connections: PR throughput sparkline, top
  contributors (last 30d), review backlog, recent deploys.
- `/connections` — list/add/remove repo and org connections; per-connection sync status & errors.
- `/repos/:owner/:name` — repo dashboard: PR cycle time histogram, deployment frequency, lead
  time, review load.
- `/orgs/:org` — org dashboard, drill down by repo.
- `/engineers/:login` — per-engineer view: commits, PRs opened/merged/reviewed, cycle time over
  time, review responsiveness.
- `/settings` — admin password, tokens, polling cadence, retention window, danger zone (reset).

**API surface** (all under `/api/v1`):

```
POST   /auth/register                  (only when zero users exist)
POST   /auth/login
POST   /auth/logout
GET    /me

GET    /tokens
POST   /tokens
DELETE /tokens/:id

GET    /connections
POST   /connections
DELETE /connections/:id

GET    /repos
GET    /repos/:owner/:name/metrics?from=&to=
GET    /orgs/:org/metrics?from=&to=
GET    /engineers
GET    /engineers/:login/metrics?from=&to=

GET    /sync/status                    (live ingest health)
GET    /system/health
```

Frontend talks to the API via a thin `fetch` wrapper. **OpenAPI 3** generated from the Go
handlers (via `swaggo` or hand-rolled YAML); TS client generated via `openapi-typescript`. The
SPA never has stale types vs. the backend.

## Repo layout

```
tempo/
├── .mise.toml                  # go 1.24.x, node 24.x, pnpm 10.x
├── Makefile                    # dev, build, test, lint, ci
├── README.md                   # screenshots + 5-line quickstart
├── ARCHITECTURE.md
├── CONTRIBUTING.md
├── LICENSE                     # MIT
├── go.mod / go.sum
├── cmd/tempo/main.go           # binary entrypoint
├── internal/
│   ├── config/                 # env parsing, defaults
│   ├── server/                 # http.Handler, middleware, sessions
│   ├── api/                    # REST handlers
│   ├── auth/                   # register/login, argon2id
│   ├── storage/
│   │   ├── storage.go          # Storage interface (the seam)
│   │   ├── sqlite/             # primary impl, sqlc-generated
│   │   └── postgres/           # secondary impl (stub in v1)
│   ├── github/                 # GraphQL + REST client, rate limiter
│   ├── ingest/                 # poller, per-resource fetchers, cursor mgmt
│   ├── rollup/                 # event → daily_* aggregator
│   ├── metrics/                # cycle time, review latency, DORA primitives
│   └── webui/                  # //go:embed web/dist + SPA fallback
├── migrations/                 # goose .up.sql / .down.sql
├── web/                        # frontend (its own pnpm package)
│   ├── package.json
│   ├── vite.config.ts          # dev proxy /api → :8080
│   ├── tsconfig.json
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       ├── routes/             # TanStack Router
│       ├── lib/api.ts          # generated TS client
│       ├── components/
│       └── styles/
└── .plans/                     # see "Task workflow" below
    ├── upnext/                 # pending task dirs
    └── completed/
        └── .gitkeep
```

## Configuration

Single binary reads env vars on startup:

| Var                    | Default                  | Notes                                                |
| ---------------------- | ------------------------ | ---------------------------------------------------- |
| `TEMPO_DB`             | `sqlite:///data/tempo.db`| `sqlite://...` or `postgres://...`                   |
| `TEMPO_LISTEN`         | `:8080`                  | host:port                                            |
| `TEMPO_SECRET`         | (required)               | base64 32 bytes; sessions + token encryption         |
| `TEMPO_POLL_INTERVAL`  | `15m`                    | go duration                                          |
| `TEMPO_BACKFILL_DAYS`  | `90`                     |                                                       |
| `TEMPO_LOG_LEVEL`      | `info`                   | `debug` / `info` / `warn` / `error`                  |
| `TEMPO_TZ`             | system                   | controls when the daily rollup fires                 |

## Build & deploy

- **Local dev**: `make dev` runs `go run ./cmd/tempo` and `pnpm -C web dev` in parallel
  (concurrently). Vite proxies `/api` → `localhost:8080`.
- **Production build**: `make build` runs `pnpm -C web build` then `go build -o tempo
  ./cmd/tempo` with `//go:embed web/dist`. Output: a single `tempo` binary.
- **Docker**: multi-stage. Final stage `gcr.io/distroless/static-debian12`, ~25MB image. Volume
  for `/data` (SQLite + WAL).
- **Railway (later)**: same binary; Postgres addon → `TEMPO_DB=$DATABASE_URL`. Persistent
  volume for SQLite-mode is also fine.
- **mise.toml** pins all toolchain versions so `mise install` brings up a contributor's machine
  to a known-good state.

## Task workflow (`.plans/`)

Every chunk of work lives in its own directory under `.plans/upnext/`. The directory is the
"skill-style" unit — it carries the instructions, scratch space, fixtures, and a verification
script.

### Layout of a task dir

```
.plans/upnext/0007-ingest-pull-requests-graphql/
├── TASK.md           # required, frontmatter + body
├── verify.sh         # required, exits 0 on success
├── notes.md          # optional, agent scratch
├── fixtures/         # optional, e.g. recorded GraphQL responses
└── snippets/         # optional, code or SQL drafts
```

### `TASK.md` frontmatter (strict)

```yaml
---
id: 0007
slug: ingest-pull-requests-graphql
title: Ingest pull requests via GraphQL with cursors
status: pending           # pending | in_progress | blocked | done | failed
depends_on: [0003, 0006]  # other task ids
owner: ""                 # who claimed it (empty if free)
est_minutes: 45
tags: [ingest, github]
---

## Goal
…what done looks like in one paragraph…

## Acceptance criteria
- [ ] …
- [ ] …

## Files to touch
- internal/github/pull_requests.go
- internal/ingest/pull_requests.go
- migrations/0004_pr_cursors.up.sql

## Verification
`./verify.sh` runs go test + a fixture-replay integration test.

## Notes
…anything the next agent needs to know…
```

### `verify.sh`

Each task ships with a deterministic verification script — go tests, lint, a focused integration
test, whatever proves the acceptance criteria. The slash command shells out to it.

### Slash command: `/next-task`

A reusable command in `.claude/commands/next-task.md` (project-scoped). Behavior:

1. List `.plans/upnext/*/TASK.md` and parse frontmatter.
2. Filter to `status=pending` AND `depends_on` all resolved (i.e., every dep id has a matching
   dir under `.plans/completed/`).
3. Pick the lowest `id` from the unblocked set.
4. Read `TASK.md` fully. Print the goal + acceptance criteria so the user sees what's about to
   run.
5. Set `status: in_progress` in the frontmatter.
6. Implement the task.
7. Run `./verify.sh`.
8. **On success**: write `RESULT.md` (what changed, files touched, any followups), set
   `status: done`, `git mv` the task dir from `.plans/upnext/` to `.plans/completed/`, commit.
9. **On failure**: write `FAILURE.md` (error output, hypothesis), set `status: failed`. Leave it
   in `upnext/` so the human can review.

A second command, `/finish-task <id>`, exists for the manual-override path: human approves a
task that's `in_progress` and moves it to completed without re-running verification (e.g., flaky
test path).

A third, `/plan-task "<title>"`, scaffolds a new task dir with the right frontmatter — to make
adding work fast.

## Build velocity recommendations

These are folded into the implementation tasks, called out here so they aren't lost:

1. **VCR-style GitHub fixtures.** Record real GraphQL/REST responses once, replay in tests via
   `go-vcr` (or hand-rolled JSON). CI never burns rate limit. Re-record on demand with
   `go test -tags=record`.
2. **Sqlc + goose.** Compile-checked SQL queries; migrations are SQL files, not Go code. Cuts
   the "I forgot to update the struct" class of bugs.
3. **OpenAPI contract.** Go server emits OpenAPI 3 (swaggo). Frontend's TS client is generated
   from it. PRs that change a handler must regenerate the client; CI fails if the generated
   client is out of sync with the YAML.
4. **`make ci` mirrors GitHub Actions.** Same commands, same exit codes locally and in CI. No
   "passes locally, fails on CI" surprises.
5. **`air` for Go hot-reload** during dev. Vite already hot-reloads the SPA.
6. **Bundle budget.** SPA target: ≤ 250KB gzipped initial. Enforced via a tiny CI check on
   `web/dist`. Self-host instances often run behind slow links.
7. **Storybook for chart and complex composition.** Iterate on charts and dashboard panels with
   seeded fixture data; no need to boot Go to tweak a histogram. Storybook also catches shadcn
   composition mistakes early (missing `CardHeader`, `SelectGroup`, etc.).
8. **Structured logs from day 1** (`log/slog`). `/api/v1/sync/status` exposes ingest health;
   `/debug/vars` exposes rate-limit state, queue depth, last error per resource. Self-host
   users can debug without external tools.
9. **Pre-commit hook**: `gofmt`, `golangci-lint run`, `pnpm typecheck`, `pnpm lint`. Fast local
   gate.
10. **OSS scaffolding from day 1**: README with screenshots, ARCHITECTURE.md, CONTRIBUTING.md,
    GitHub issue templates, MIT license, CODEOWNERS. Repo looks credible on day 1.
11. **GitHub Actions cache** for Go modules, pnpm store, and the `sqlc` binary. CI under 3
    minutes.
12. **Single SQL contract.** Postgres support in v1.x has to use the same migrations + sqlc
    queries. The test suite runs against both via a `STORAGE_BACKEND` env var.
13. **Conventional commits + release-please.** Every merge to `main` produces a CHANGELOG entry
    and (for tagged releases) a binary + Docker image artifact.
14. **Tasks small enough to verify in one script.** Aim for ≤ 30 minutes of work per task. If
    a task balloons, split it. The slash command rewards small tasks because they fail loud and
    fast.

## v1 scope summary (what the first release ships)

- Single Go binary + embedded SPA.
- SQLite storage.
- Admin register/login (one user).
- Add a repo or org connection with a PAT; backfill 90 days; poll every 15 min.
- Dashboards: global, per-org, per-repo, per-engineer.
- Metrics: per-engineer activity, PR cycle time, review latency & load, DORA (deploy freq +
  lead time).
- mise.toml + Makefile + Dockerfile.
- `.plans/` task workflow with `/next-task`, `/finish-task`, `/plan-task`.

## Open questions for v1.1+ (out of scope now)

- Webhooks for near-real-time updates (instance must have a public URL).
- GitHub App install flow (today: PAT only).
- Postgres parity tested in CI (today: code path exists, primary test target is SQLite).
- Multi-user mode (today: schema-ready, UI hidden).
- Custom dashboards / saved metric views.
- Exporting metrics to Prometheus / OTLP.
