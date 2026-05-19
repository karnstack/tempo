# Migrations

goose-managed SQL files. `mise run migrate-up` applies pending migrations
against the database at `TEMPO_DB` (default `sqlite://./data/tempo.db`).

Naming: one file per version, `NNNN_<slug>.sql`, with `-- +goose Up`
and `-- +goose Down` sections inside. Tasks 0008–0011 add the v1 baseline.
