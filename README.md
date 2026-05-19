# tempo

**Engineering metrics for GitHub, built for the engineers being measured.**

Most dashboards show your manager your cadence, your throughput, your review latency. You don't get to see any of it. tempo is the same dashboard — and the data is yours.

Point it at a repo or an org. It pulls from the GitHub API, snapshots daily, and surfaces per-engineer, per-repo, and per-org views of how work actually moves: commits, PRs, review cycle time, who's drowning in reviews, deploys, lead time.

The whole thing is a single Go binary with the UI baked in. No SaaS, no per-seat pricing, no "talk to sales".

```bash
mise install
mise run dev
```

Open `http://localhost:4810`, register, paste a GitHub PAT, add a connection.
(Vite serves the SPA on `:4810` and proxies `/api` to the Go server on `:4811`.
Both honor `PORT` so wrappers like [portless.sh](https://portless.sh) work
without code changes — `portless tempo mise run dev-web` for the SPA,
`portless api.tempo mise run dev-api` for the Go server.)

Tasks live in `.mise.toml` — `mise tasks` lists them all. The dailies:

| Task | What it does |
|---|---|
| `mise run dev` | Migrate + run Go (air) + Vite together |
| `mise run dev-api` | Just the Go API (use under portless) |
| `mise run dev-web` | Just the Vite server (use under portless) |
| `mise run test` | Full test suite |
| `mise run lint` / `fmt` | Lint / format |
| `mise run migrate-up` / `migrate-status` | DB migrations |

For production, one binary and a SQLite file next to it:

```bash
mise run build
TEMPO_SECRET=$(openssl rand -base64 32) ./tempo
```

## Why

Metrics about your work shouldn't be a one-way mirror.

## License

MIT.
