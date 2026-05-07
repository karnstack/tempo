-- +goose Up

CREATE TABLE tenants (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  email TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX users_tenant_email_uniq ON users(tenant_id, email);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE gh_tokens (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  label TEXT NOT NULL,
  encrypted_pat BLOB NOT NULL,
  scopes TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX gh_tokens_tenant_idx ON gh_tokens(tenant_id);

CREATE TABLE connections (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  kind TEXT NOT NULL,
  owner TEXT NOT NULL,
  name TEXT,
  token_id INTEGER NOT NULL,
  backfill_from TIMESTAMP NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  last_sync_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX connections_repo_uniq
  ON connections(tenant_id, owner, name) WHERE kind = 'repo';
CREATE UNIQUE INDEX connections_org_uniq
  ON connections(tenant_id, owner) WHERE kind = 'org';
CREATE INDEX connections_token_idx ON connections(token_id);

CREATE TABLE repos (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  connection_id INTEGER NOT NULL,
  gh_id INTEGER NOT NULL,
  owner TEXT NOT NULL,
  name TEXT NOT NULL,
  default_branch TEXT NOT NULL DEFAULT 'main',
  archived INTEGER NOT NULL DEFAULT 0,
  added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX repos_tenant_gh_uniq ON repos(tenant_id, gh_id);
CREATE INDEX repos_connection_idx ON repos(connection_id);
CREATE INDEX repos_owner_name_idx ON repos(owner, name);

CREATE TABLE gh_users (
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  gh_id INTEGER NOT NULL,
  login TEXT NOT NULL,
  name TEXT,
  avatar_url TEXT,
  last_seen_at TIMESTAMP
);
CREATE UNIQUE INDEX gh_users_tenant_gh_uniq ON gh_users(tenant_id, gh_id);
CREATE INDEX gh_users_login_idx ON gh_users(login);

-- +goose Down

DROP TABLE IF EXISTS gh_users;
DROP TABLE IF EXISTS repos;
DROP TABLE IF EXISTS connections;
DROP TABLE IF EXISTS gh_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
