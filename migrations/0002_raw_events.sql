-- +goose Up

CREATE TABLE commits (
  repo_id INTEGER NOT NULL,
  sha TEXT NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  committer_gh_user_id INTEGER NOT NULL,
  authored_at TIMESTAMP NOT NULL,
  additions INTEGER NOT NULL DEFAULT 0,
  deletions INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (repo_id, sha)
);
CREATE INDEX commits_repo_authored_idx ON commits(repo_id, authored_at);
CREATE INDEX commits_author_authored_idx ON commits(author_gh_user_id, authored_at);

CREATE TABLE pull_requests (
  repo_id INTEGER NOT NULL,
  number INTEGER NOT NULL,
  gh_id INTEGER NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  state TEXT NOT NULL,
  title TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL,
  merged_at TIMESTAMP,
  closed_at TIMESTAMP,
  additions INTEGER NOT NULL DEFAULT 0,
  deletions INTEGER NOT NULL DEFAULT 0,
  base_ref TEXT NOT NULL,
  head_ref TEXT NOT NULL,
  draft INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (repo_id, number)
);
CREATE UNIQUE INDEX pull_requests_repo_gh_uniq ON pull_requests(repo_id, gh_id);
CREATE INDEX pull_requests_author_created_idx ON pull_requests(author_gh_user_id, created_at);
CREATE INDEX pull_requests_repo_merged_idx ON pull_requests(repo_id, merged_at);
CREATE INDEX pull_requests_repo_state_idx ON pull_requests(repo_id, state);

CREATE TABLE pr_reviews (
  gh_id INTEGER PRIMARY KEY,
  pr_repo_id INTEGER NOT NULL,
  pr_number INTEGER NOT NULL,
  reviewer_gh_user_id INTEGER NOT NULL,
  state TEXT NOT NULL,
  submitted_at TIMESTAMP NOT NULL
);
CREATE INDEX pr_reviews_pr_idx ON pr_reviews(pr_repo_id, pr_number);
CREATE INDEX pr_reviews_reviewer_submitted_idx ON pr_reviews(reviewer_gh_user_id, submitted_at);

CREATE TABLE pr_review_comments (
  gh_id INTEGER PRIMARY KEY,
  pr_repo_id INTEGER NOT NULL,
  pr_number INTEGER NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX pr_review_comments_pr_idx ON pr_review_comments(pr_repo_id, pr_number);
CREATE INDEX pr_review_comments_author_created_idx ON pr_review_comments(author_gh_user_id, created_at);

CREATE TABLE pr_issue_comments (
  gh_id INTEGER PRIMARY KEY,
  pr_repo_id INTEGER NOT NULL,
  pr_number INTEGER NOT NULL,
  author_gh_user_id INTEGER NOT NULL,
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX pr_issue_comments_pr_idx ON pr_issue_comments(pr_repo_id, pr_number);
CREATE INDEX pr_issue_comments_author_created_idx ON pr_issue_comments(author_gh_user_id, created_at);

CREATE TABLE deployments (
  gh_id INTEGER PRIMARY KEY,
  repo_id INTEGER NOT NULL,
  environment TEXT NOT NULL,
  ref TEXT NOT NULL,
  sha TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL
);
CREATE INDEX deployments_repo_created_idx ON deployments(repo_id, created_at);
CREATE INDEX deployments_repo_env_idx ON deployments(repo_id, environment);

-- +goose Down

DROP TABLE IF EXISTS deployments;
DROP TABLE IF EXISTS pr_issue_comments;
DROP TABLE IF EXISTS pr_review_comments;
DROP TABLE IF EXISTS pr_reviews;
DROP TABLE IF EXISTS pull_requests;
DROP TABLE IF EXISTS commits;
