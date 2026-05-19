# tempo

**Engineering metrics for GitHub, built for the engineers being measured.**

Most dashboards show your manager your cadence, your throughput, your review latency. You don't get to see any of it. tempo is the same dashboard — and the data is yours.

Point it at a repo or an org. It pulls from the GitHub API, snapshots daily, and surfaces per-engineer, per-repo, and per-org views of how work actually moves: commits, PRs, review cycle time, who's drowning in reviews, deploys, lead time.

The whole thing is a single Go binary with the UI baked in. No SaaS, no per-seat pricing, no "talk to sales".

```bash
mise install

# In one terminal — backend (Go API, runs migrations first, hot reload via air):
mise run dev-api

# In another — frontend (Vite SPA):
mise run dev-web
```

Open `https://tempo.localhost`, register, paste a GitHub PAT, add a connection.

The two dev tasks wrap each server with [portless.sh](https://portless.sh) —
the SPA lands on `https://tempo.localhost` and the API on
`https://api.tempo.localhost`, with portless assigning ports inside its
4000–4999 range and handling TLS via a locally-trusted CA. Sudo is
requested once on first proxy start; subsequent runs are sudoless.

If you don't want portless in the loop (CI, headless work, or a fresh box
where you'd rather skip the sudo prompt), use `dev-api-raw` / `dev-web-raw`
— they bind plain `:4811` and `:4810` and the SPA proxies `/api → :4811`.

Tasks live in `.mise.toml` — `mise tasks` lists them all. The dailies:

| Task | What it does |
|---|---|
| `mise run dev-api` | Backend on `https://api.tempo.localhost` (portless + air) |
| `mise run dev-web` | Frontend on `https://tempo.localhost` (portless + Vite) |
| `mise run dev-api-raw` / `dev-web-raw` | Same, without portless (binds :4811 / :4810) |
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
