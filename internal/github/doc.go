// Package github is tempo's GitHub client. A Client is bound to one PAT and
// exposes REST and GraphQL methods that transparently honour GitHub's rate
// limits, retry transient failures, and pass conditional-request headers
// through unchanged. Resource-specific fetchers (PRs, commits, reviews) live
// in their own packages and call this one.
package github
