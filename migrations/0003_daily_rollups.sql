-- +goose Up

CREATE TABLE daily_engineer_stats (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  gh_user_id INTEGER NOT NULL,
  commits INTEGER NOT NULL DEFAULT 0,
  prs_opened INTEGER NOT NULL DEFAULT 0,
  prs_merged INTEGER NOT NULL DEFAULT 0,
  reviews_given INTEGER NOT NULL DEFAULT 0,
  comments INTEGER NOT NULL DEFAULT 0,
  additions INTEGER NOT NULL DEFAULT 0,
  deletions INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, repo_id, gh_user_id)
);
CREATE INDEX daily_engineer_stats_user_date_idx ON daily_engineer_stats(gh_user_id, date);
CREATE INDEX daily_engineer_stats_repo_date_idx ON daily_engineer_stats(repo_id, date);

CREATE TABLE daily_repo_stats (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  prs_opened INTEGER NOT NULL DEFAULT 0,
  prs_merged INTEGER NOT NULL DEFAULT 0,
  prs_closed INTEGER NOT NULL DEFAULT 0,
  deploys INTEGER NOT NULL DEFAULT 0,
  lead_time_seconds_p50 INTEGER,
  lead_time_seconds_p90 INTEGER,
  PRIMARY KEY (date, repo_id)
);
CREATE INDEX daily_repo_stats_repo_date_idx ON daily_repo_stats(repo_id, date);

CREATE TABLE daily_review_latency (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  time_to_first_review_seconds_p50 INTEGER,
  time_to_first_review_seconds_p90 INTEGER,
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (date, repo_id)
);
CREATE INDEX daily_review_latency_repo_date_idx ON daily_review_latency(repo_id, date);

CREATE TABLE daily_review_load (
  date TEXT NOT NULL,
  repo_id INTEGER NOT NULL,
  reviewer_gh_user_id INTEGER NOT NULL,
  reviews INTEGER NOT NULL DEFAULT 0,
  response_minutes_p50 INTEGER,
  PRIMARY KEY (date, repo_id, reviewer_gh_user_id)
);
CREATE INDEX daily_review_load_reviewer_date_idx ON daily_review_load(reviewer_gh_user_id, date);

CREATE TABLE rollup_runs (
  date TEXT NOT NULL,
  kind TEXT NOT NULL,
  started_at TIMESTAMP NOT NULL,
  finished_at TIMESTAMP,
  ok INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (date, kind)
);
CREATE INDEX rollup_runs_date_idx ON rollup_runs(date);

-- +goose Down

DROP TABLE IF EXISTS rollup_runs;
DROP TABLE IF EXISTS daily_review_load;
DROP TABLE IF EXISTS daily_review_latency;
DROP TABLE IF EXISTS daily_repo_stats;
DROP TABLE IF EXISTS daily_engineer_stats;
