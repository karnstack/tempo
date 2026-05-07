# tempo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. **Note:** This plan uses tempo's custom `.plans/upnext/` workflow (per the design spec) — each task is its own directory with `TASK.md` + `verify.sh`. The bite-sized steps live inside each `TASK.md`, not in this master file. Run `/next-task` to pick up the next unblocked task.

**Goal:** Ship v1 of tempo — an open-source GitHub engineering metrics dashboard delivered as a single Go binary with embedded TanStack/shadcn SPA, SQLite default, snapshot-based metric storage, runnable by any engineer with `mise install && make run`.

**Architecture:** Three goroutines in one Go process — HTTP server, ingest worker (15-min poll), rollup worker (daily 02:00). Storage behind an interface (SQLite primary, Postgres future). GitHub via GraphQL-first with REST fallback and rate-limit-aware backoff. Frontend is a Vite + TanStack Router SPA built with shadcn/ui (preset `bcivVNFh`, base library Base UI), embedded into the Go binary at build time.

**Tech Stack:**
- Go 1.24, chi router, sqlc, goose migrations, shurcooL/githubv4, go-github, log/slog, argon2id (golang.org/x/crypto/argon2)
- Node 24, pnpm 10, Vite, React 19, TanStack Router/Query, Tailwind v4, shadcn/ui (Base UI), lucide-react, Recharts (via shadcn Chart)
- Tooling: mise, Make, golangci-lint, sqlc, goose, swaggo (OpenAPI), openapi-typescript, air (Go hot reload), release-please
- Deploy: single binary, Docker (distroless), Railway (later)

---

## File Structure (high level)

```
tempo/
├── .mise.toml
├── .editorconfig
├── .gitignore
├── .golangci.yml
├── .air.toml                  # Go dev hot-reload
├── Makefile
├── Dockerfile
├── README.md
├── ARCHITECTURE.md
├── CONTRIBUTING.md
├── LICENSE
├── CODEOWNERS
├── .github/
│   ├── workflows/ci.yml
│   ├── ISSUE_TEMPLATE/
│   └── PULL_REQUEST_TEMPLATE.md
├── go.mod / go.sum
├── sqlc.yaml
├── goose.yaml
├── cmd/tempo/main.go          # entrypoint
├── internal/
│   ├── config/                # env loader
│   ├── server/                # HTTP, middleware, SPA serve
│   ├── api/                   # REST handlers
│   ├── auth/                  # argon2id, sessions, register/login
│   ├── storage/
│   │   ├── storage.go         # interface
│   │   ├── sqlite/            # primary impl, sqlc-generated
│   │   └── postgres/          # stub for future
│   ├── github/                # GraphQL+REST client, rate limiter, fixtures
│   ├── ingest/                # poll scheduler, per-resource fetchers
│   ├── rollup/                # daily aggregator
│   ├── metrics/               # cycle time, review latency, DORA primitives
│   └── webui/                 # //go:embed web/dist + SPA fallback
├── migrations/                # 0001..NNNN .up.sql / .down.sql
├── web/                       # frontend
│   ├── package.json
│   ├── vite.config.ts
│   ├── components.json        # shadcn config
│   ├── index.html
│   └── src/
│       ├── main.tsx
│       ├── routes/
│       ├── components/        # app components (charts, panels)
│       ├── components/ui/     # shadcn components (CLI-managed)
│       ├── lib/api.ts         # generated TS client
│       └── styles/
├── docs/
│   ├── superpowers/
│   │   ├── specs/2026-05-08-tempo-design.md
│   │   └── plans/2026-05-08-tempo-implementation.md   ← you are here
│   └── screenshots/
├── .claude/
│   └── commands/
│       ├── next-task.md
│       ├── finish-task.md
│       ├── iterate-ui.md
│       └── plan-task.md
└── .plans/
    ├── upnext/                # active and pending task dirs
    └── completed/             # done task dirs
        └── .gitkeep
```

Files split by responsibility, not technical layer. Each Go package has one job. Frontend components stay small (one component per file, panels assembled from primitives).

---

## Workflow notes

- **Picking up work:** Run `/next-task`. The slash command picks the lowest-id `pending` task in `.plans/upnext/` whose `depends_on` ids are all in `.plans/completed/`. It then loads any skills listed in `skills:`, runs the steps in `TASK.md`, and runs `verify.sh`.
- **Backend tasks (`autonomy: full`)**: agent runs end-to-end. On verify success → write `RESULT.md`, set `status: done`, `git mv` to `completed/`, commit. On failure → write `FAILURE.md`, leave in `upnext/`, status `failed`.
- **UI tasks (`autonomy: review`)**: agent runs implement + verify, writes `RESULT.md`, leaves task in `upnext/` with status `in_progress`, prints "ready for UI review". Human iterates conversationally; runs `/finish-task <id>` to promote it.
- **Adding new tasks:** `/plan-task "<title>"` scaffolds a new dir with smart-defaulted frontmatter.
- **Stubs:** Tasks beyond the first batch may exist as TASK.md frontmatter only. Their bodies get expanded just before they're picked up (either inline or via `/plan-task`). This avoids stale plan rot.

---

## Phases & tasks

Each row: `id | title | autonomy | skills | depends_on`. Status of "fleshed" means the task dir under `.plans/upnext/` has full `TASK.md` body + `verify.sh`. Status "stub" means only frontmatter exists; body gets written when the task is picked up.

### Phase 1 — Bootstrap

| ID    | Title                                          | Autonomy | Skills                            | Deps   | Status   |
| ----- | ---------------------------------------------- | -------- | --------------------------------- | ------ | -------- |
| 0001  | Repo scaffolding (mise, Makefile, LICENSE, …)  | full     | —                                 | —      | fleshed  |
| 0002  | Go module + skeleton package layout            | full     | —                                 | 0001   | fleshed  |
| 0003  | Frontend bootstrap (shadcn init, preset)       | review   | shadcn                            | 0001   | fleshed  |
| 0004  | Layer TanStack Router + Query                  | review   | shadcn                            | 0003   | fleshed  |
| 0005  | Embed SPA into Go binary (`//go:embed`)        | full     | —                                 | 0002, 0004 | fleshed |
| 0006  | Dev tooling — air, concurrent dev script       | full     | —                                 | 0005   | fleshed  |

### Phase 2 — Storage & migrations

| ID    | Title                                                     | Autonomy | Skills                | Deps      | Status |
| ----- | --------------------------------------------------------- | -------- | --------------------- | --------- | ------ |
| 0007  | Storage interface + SQLite driver wiring (sqlc, goose)    | full     | —                     | 0002      | stub   |
| 0008  | Migration 0001 — identity & config tables                 | full     | —                     | 0007      | stub   |
| 0009  | Migration 0002 — raw event tables                         | full     | —                     | 0008      | stub   |
| 0010  | Migration 0003 — daily rollup tables                      | full     | —                     | 0009      | stub   |
| 0011  | Migration 0004 — sync state tables                        | full     | —                     | 0009      | stub   |
| 0012  | sqlc-generated repository methods + repo unit tests       | full     | —                     | 0011      | stub   |

### Phase 3 — Config, logging, auth

| ID    | Title                                          | Autonomy | Skills                | Deps      | Status |
| ----- | ---------------------------------------------- | -------- | --------------------- | --------- | ------ |
| 0013  | Config package + env loader + validation       | full     | —                     | 0002      | stub   |
| 0014  | Logger (slog) + request/correlation middleware | full     | —                     | 0013      | stub   |
| 0015  | Argon2id password hashing module               | full     | —                     | 0013      | stub   |
| 0016  | Server-validated cookie sessions               | full     | —                     | 0012, 0015 | stub  |
| 0017  | `/auth/register` + first-run gate              | full     | —                     | 0016      | stub   |
| 0018  | `/auth/login` + `/auth/logout` + middleware    | full     | —                     | 0017      | stub   |

### Phase 4 — GitHub client

| ID    | Title                                                              | Autonomy | Skills                  | Deps      | Status |
| ----- | ------------------------------------------------------------------ | -------- | ----------------------- | --------- | ------ |
| 0019  | GitHub client base (REST + GraphQL) + rate limiter                 | full     | systematic-debugging    | 0013      | stub   |
| 0020  | VCR-style fixture recorder/replayer for tests                      | full     | systematic-debugging    | 0019      | stub   |
| 0021  | PR fetcher (GraphQL, with cursors)                                 | full     | systematic-debugging    | 0020      | stub   |
| 0022  | Reviews + review-comments + issue-comments fetchers                | full     | systematic-debugging    | 0021      | stub   |
| 0023  | Commits fetcher (REST `since=` + ETag)                             | full     | systematic-debugging    | 0020      | stub   |
| 0024  | Deployments + Releases fetcher                                     | full     | systematic-debugging    | 0020      | stub   |
| 0025  | Org repos enumerator                                               | full     | systematic-debugging    | 0019      | stub   |

### Phase 5 — Ingest worker

| ID    | Title                                                  | Autonomy | Skills                  | Deps             | Status |
| ----- | ------------------------------------------------------ | -------- | ----------------------- | ---------------- | ------ |
| 0026  | Worker scheduler (ticker, per-connection iteration)    | full     | systematic-debugging    | 0019, 0011       | stub   |
| 0027  | PR ingest end-to-end with cursor persistence           | full     | systematic-debugging    | 0021, 0026       | stub   |
| 0028  | Reviews/comments ingest                                | full     | systematic-debugging    | 0022, 0027       | stub   |
| 0029  | Commits ingest                                         | full     | systematic-debugging    | 0023, 0026       | stub   |
| 0030  | Deployments ingest                                     | full     | systematic-debugging    | 0024, 0026       | stub   |
| 0031  | Sync runs + error tracking + sync status endpoint hook | full     | —                       | 0027–0030        | stub   |

### Phase 6 — Rollup worker

| ID    | Title                                                       | Autonomy | Skills | Deps        | Status |
| ----- | ----------------------------------------------------------- | -------- | ------ | ----------- | ------ |
| 0032  | Rollup scheduler (daily 02:00 instance-local)               | full     | —      | 0010, 0031  | stub   |
| 0033  | Engineer stats rollup (commits, PRs, reviews, comments)     | full     | —      | 0032        | stub   |
| 0034  | Repo stats rollup (counts, deploys)                         | full     | —      | 0032        | stub   |
| 0035  | Cycle time + lead time rollup with p50/p90 percentiles      | full     | —      | 0033, 0034  | stub   |
| 0036  | Review latency + load rollup                                | full     | —      | 0033        | stub   |
| 0037  | Idempotent re-aggregation hook (retro-rebuild a date range) | full     | —      | 0033–0036   | stub   |

### Phase 7 — REST API

| ID    | Title                                                   | Autonomy | Skills | Deps              | Status |
| ----- | ------------------------------------------------------- | -------- | ------ | ----------------- | ------ |
| 0038  | `/api/v1/me` + auth middleware wiring                   | full     | —      | 0018              | stub   |
| 0039  | `/api/v1/tokens` CRUD with encrypted PAT storage        | full     | —      | 0015, 0018        | stub   |
| 0040  | `/api/v1/connections` CRUD                              | full     | —      | 0039              | stub   |
| 0041  | `/api/v1/repos` + `/api/v1/repos/:owner/:name/metrics`  | full     | —      | 0033–0036         | stub   |
| 0042  | `/api/v1/orgs/:org/metrics`                             | full     | —      | 0041              | stub   |
| 0043  | `/api/v1/engineers` + per-engineer metrics              | full     | —      | 0033, 0036        | stub   |
| 0044  | `/api/v1/sync/status` + `/api/v1/system/health`         | full     | —      | 0031              | stub   |

### Phase 8 — OpenAPI + TS client

| ID    | Title                                                   | Autonomy | Skills | Deps           | Status |
| ----- | ------------------------------------------------------- | -------- | ------ | -------------- | ------ |
| 0045  | OpenAPI 3 spec generation (swaggo)                      | full     | —      | 0038–0044      | stub   |
| 0046  | Generate TS client into `web/src/lib/api.ts` + CI check | full     | —      | 0045           | stub   |

### Phase 9 — Frontend (UI iteration loop)

All UI tasks default `autonomy: review` so the human iterates after first pass.

| ID    | Title                                                   | Autonomy | Skills                                  | Deps             | Status |
| ----- | ------------------------------------------------------- | -------- | --------------------------------------- | ---------------- | ------ |
| 0047  | App shell (shadcn Sidebar nav + top bar + root layout)  | review   | shadcn, refactoring-ui                  | 0046             | stub   |
| 0048  | Auth pages — register (first-run) + login               | review   | shadcn                                  | 0047             | stub   |
| 0049  | Connections page (list/add/delete)                      | review   | shadcn                                  | 0046, 0047       | stub   |
| 0050  | Settings page (Tabs: profile, tokens, polling, danger)  | review   | shadcn                                  | 0047             | stub   |
| 0051  | Global dashboard (overview cards + charts)              | review   | frontend-design, shadcn, refactoring-ui | 0046, 0047       | stub   |
| 0052  | Repo dashboard (cycle time histogram, deploys)          | review   | frontend-design, shadcn, refactoring-ui | 0051             | stub   |
| 0053  | Org dashboard                                           | review   | shadcn, refactoring-ui                  | 0052             | stub   |
| 0054  | Engineer profile page                                   | review   | shadcn, refactoring-ui                  | 0051             | stub   |
| 0055  | Sync status panel + connections health visuals          | review   | shadcn, refactoring-ui                  | 0044, 0049       | stub   |

### Phase 10 — OSS scaffolding & CI

| ID    | Title                                                                  | Autonomy | Skills | Deps            | Status |
| ----- | ---------------------------------------------------------------------- | -------- | ------ | --------------- | ------ |
| 0056  | ARCHITECTURE.md, CONTRIBUTING.md, README polish + screenshots          | full     | —      | 0055            | stub   |
| 0057  | GitHub issue + PR templates, CODEOWNERS                                | full     | —      | 0001            | stub   |
| 0058  | GitHub Actions CI (Go test/lint, pnpm test/build, sqlc, openapi check) | full     | —      | 0046, 0012      | stub   |
| 0059  | Pre-commit hooks (gofmt, golangci-lint, pnpm typecheck/lint)           | full     | —      | 0058            | stub   |
| 0060  | Dockerfile (multi-stage, distroless) + docker-compose for local        | full     | —      | 0005            | stub   |
| 0061  | release-please config + first tagged release                           | full     | —      | 0058, 0060      | stub   |

---

## Self-review

**Spec coverage**

- Single Go binary with embedded SPA → covered by 0002 (Go skeleton), 0005 (embed), 0060 (Docker).
- SQLite primary, Postgres-extendable → 0007 (interface), 0008–0011 (migrations), 0012 (queries).
- Auth: first-run register + admin login → 0015–0018.
- GitHub PAT, encrypted at rest → 0015, 0039.
- GraphQL-first with rate-limit handling → 0019, 0021.
- VCR fixtures for tests → 0020.
- 90-day backfill, 15-min polling, daily 02:00 rollup → 0026, 0032.
- All four metric families (per-engineer, cycle time, review latency, DORA) → 0033–0036.
- All routes & API surface from the spec → 0038–0044.
- shadcn via preset `bcivVNFh`, Base UI, lucide, shadcn Chart → 0003, 0047–0055.
- mise + Make + Dockerfile → 0001, 0060.
- `.plans/` workflow + slash commands → bootstrapped in this writing-plans session.
- OpenAPI + generated TS client → 0045, 0046.
- All 14 build-velocity recommendations → folded into the relevant tasks (sqlc into 0007/0012, OpenAPI into 0045/0046, structured logs into 0014, bundle budget + storybook into 0051+, CI cache into 0058, conventional commits + release-please into 0061).

**Placeholder scan:** No TBDs. The "stub" status for non-fleshed tasks is intentional — bodies get expanded just-in-time. This is by design per the workflow notes above.

**Type consistency:** Names align with the spec — `Storage`, `connections.kind ∈ {repo,org}`, `daily_engineer_stats(date, repo_id, gh_user_id, …)`, etc.

---

## Execution

**The fleshed tasks (0001–0006) are immediately runnable.** Open a fresh session, type `/next-task`, and the agent picks up `0001-repo-scaffolding`. After Phase 1 lands, expand the next batch (0007–0012) by running `/next-task` against the stubs — the agent will write the body then implement.

For the recommended subagent-driven flow, dispatch a fresh agent per task and review the diff before merging — see `superpowers:subagent-driven-development`.
