# Migrations

goose-managed SQL files. `make migrate-up` applies pending migrations against
the database at `TEMPO_DB` (default `sqlite://./data/tempo.db`).

Naming: `NNNN_<slug>.up.sql` / `NNNN_<slug>.down.sql`. Tasks 0008–0011 add
the v1 baseline.
