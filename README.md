# tempo

**Engineering metrics for GitHub, built for the engineers being measured.**

Most dashboards show your manager your cadence, your throughput, your review latency. You don't get to see any of it. tempo is the same dashboard — and the data is yours.

Point it at a repo or an org. It pulls from the GitHub API, snapshots daily, and surfaces per-engineer, per-repo, and per-org views of how work actually moves: commits, PRs, review cycle time, who's drowning in reviews, deploys, lead time.

The whole thing is a single Go binary with the UI baked in. No SaaS, no per-seat pricing, no "talk to sales".

```bash
mise install
make dev
```

Open `http://localhost:5173`, register, paste a GitHub PAT, add a connection.

For production, one binary and a SQLite file next to it:

```bash
make build
TEMPO_SECRET=$(openssl rand -base64 32) ./tempo
```

## Why

Metrics about your work shouldn't be a one-way mirror.

## License

MIT.
