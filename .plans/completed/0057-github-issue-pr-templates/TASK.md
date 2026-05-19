---
id: 0057
slug: github-issue-pr-templates
title: GitHub issue + PR templates, CODEOWNERS
status: done
depends_on: [0001]
owner: ""
est_minutes: 15
tags: [oss]
autonomy: full
skills: []
---

## Goal

Drop in the OSS-housekeeping files so a fresh visitor sees a credible repo and so contributors get a structured intake form. Five files total, all under `.github/`. No tooling changes; CI and pre-commit handling of these is 0058's job.

Spec reference: `docs/superpowers/specs/2026-05-08-tempo-design.md` lines 459–460:
> "OSS scaffolding from day 1: README with screenshots, ARCHITECTURE.md, CONTRIBUTING.md, GitHub issue templates, MIT license, CODEOWNERS. Repo looks credible on day 1."

`README.md` and `LICENSE` already exist; `ARCHITECTURE.md` / `CONTRIBUTING.md` are 0056's scope. This task carves off the issue+PR+CODEOWNERS slice.

## Acceptance criteria

- [ ] `.github/ISSUE_TEMPLATE/bug_report.yml` — GitHub Issue Forms (`yml`, not legacy `md`) with sections: short description, reproduction steps, expected vs actual, tempo version (`./tempo --version` once available, or commit SHA), browser/OS, GitHub connection kind (repo/org), relevant logs.
- [ ] `.github/ISSUE_TEMPLATE/feature_request.yml` — Issue Forms with: problem statement, proposed solution, alternatives considered, who's affected.
- [ ] `.github/ISSUE_TEMPLATE/config.yml` — `blank_issues_enabled: false`, one contact link to GitHub Discussions (placeholder URL pointing to `https://github.com/karnstack/tempo/discussions` — repo may not have Discussions enabled yet but the link is harmless).
- [ ] `.github/PULL_REQUEST_TEMPLATE.md` — sections: Summary, Why, How to test, Screenshots (UI changes), Checklist (linked task id, tests added, docs updated, breaking changes).
- [ ] `.github/CODEOWNERS` — `* @karngyan` as the only entry. Future maintainers get added one line at a time as the project grows.
- [ ] `verify.sh` — checks each file exists, is non-empty, and that the two issue-form `.yml` files parse as valid YAML.
- [ ] No Go/Web code is touched; `go build ./...` and `pnpm` are unaffected (sanity checks live in `verify.sh`).

## Files to touch

- `.github/ISSUE_TEMPLATE/bug_report.yml` (new)
- `.github/ISSUE_TEMPLATE/feature_request.yml` (new)
- `.github/ISSUE_TEMPLATE/config.yml` (new)
- `.github/PULL_REQUEST_TEMPLATE.md` (new)
- `.github/CODEOWNERS` (new)
- `.plans/upnext/0057-github-issue-pr-templates/verify.sh` (replace stub)

## Steps

### 1. Create the .github tree

```
mkdir -p .github/ISSUE_TEMPLATE
```

### 2. `.github/ISSUE_TEMPLATE/config.yml`

```yaml
blank_issues_enabled: false
contact_links:
  - name: Discussions
    url: https://github.com/karnstack/tempo/discussions
    about: Have a question or want to share how you're using tempo? Start here.
```

### 3. `.github/ISSUE_TEMPLATE/bug_report.yml`

```yaml
name: Bug report
description: Something isn't working the way the docs/UI suggest it should.
labels: [bug]
body:
  - type: markdown
    attributes:
      value: |
        Thanks for taking the time. Please give enough detail that someone else can reproduce this on their own machine.
  - type: textarea
    id: summary
    attributes:
      label: Summary
      description: One or two sentences. What's broken?
    validations:
      required: true
  - type: textarea
    id: repro
    attributes:
      label: Steps to reproduce
      description: Numbered list, starting from a fresh `make dev` if relevant.
      placeholder: |
        1. Add a connection for `owner/repo`
        2. Wait for first sync to finish
        3. Open `/dashboard/repo/owner/repo`
    validations:
      required: true
  - type: textarea
    id: expected
    attributes:
      label: What you expected
    validations:
      required: true
  - type: textarea
    id: actual
    attributes:
      label: What actually happened
      description: Include the response, the log line, the screenshot — whatever's relevant.
    validations:
      required: true
  - type: input
    id: version
    attributes:
      label: tempo version or commit
      description: Output of `./tempo --version` once available, or the commit SHA you built from.
    validations:
      required: true
  - type: dropdown
    id: connection-kind
    attributes:
      label: GitHub connection kind
      options:
        - repo
        - org
        - n/a (issue is in the UI / setup flow)
    validations:
      required: true
  - type: input
    id: env
    attributes:
      label: OS / browser
      description: e.g. macOS 14.4 / Chrome 124 / curl
  - type: textarea
    id: logs
    attributes:
      label: Relevant logs
      description: Run with `TEMPO_LOG_LEVEL=debug` if you can. Trim PII.
      render: shell
```

### 4. `.github/ISSUE_TEMPLATE/feature_request.yml`

```yaml
name: Feature request
description: A new metric, view, integration, or UX improvement.
labels: [enhancement]
body:
  - type: markdown
    attributes:
      value: |
        We bias toward v1 being small and opinionated. Requests that broaden scope (new metric, new dashboard) are most useful when grounded in a concrete situation you're hitting.
  - type: textarea
    id: problem
    attributes:
      label: Problem
      description: What are you trying to do that tempo currently makes hard or impossible?
    validations:
      required: true
  - type: textarea
    id: proposal
    attributes:
      label: Proposed solution
      description: What would the change look like? Sketch the UI / API / behaviour.
    validations:
      required: true
  - type: textarea
    id: alternatives
    attributes:
      label: Alternatives considered
      description: Other approaches you weighed and why you ruled them out.
  - type: textarea
    id: audience
    attributes:
      label: Who's affected
      description: Just you? A team you're on? A class of users (OSS maintainers, eng managers, individual contributors)?
```

### 5. `.github/PULL_REQUEST_TEMPLATE.md`

```markdown
## Summary

<!-- One or two sentences on what this changes. -->

## Why

<!-- Link to the task (`.plans/...`) or issue this addresses, and the user-visible reason it matters. -->

## How to test

<!-- Commands a reviewer can run + what they should see. -->

## Screenshots

<!-- Required for any UI change. Drag-and-drop into the editor. -->

## Checklist

- [ ] Linked task / issue:
- [ ] Tests added or updated (or N/A — explain)
- [ ] Docs updated (README / ARCHITECTURE / inline comments) where behaviour changed
- [ ] No new dependencies, or new dependency justified in the description
- [ ] Breaking change? If yes, called out in the summary and noted for release-please
```

### 6. `.github/CODEOWNERS`

```
# CODEOWNERS for tempo. Lines later in the file take precedence.
# Pattern reference: https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners
* @karngyan
```

### 7. `verify.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

required=(
  .github/ISSUE_TEMPLATE/config.yml
  .github/ISSUE_TEMPLATE/bug_report.yml
  .github/ISSUE_TEMPLATE/feature_request.yml
  .github/PULL_REQUEST_TEMPLATE.md
  .github/CODEOWNERS
)

for f in "${required[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "FAIL: missing or empty: $f" >&2
    exit 1
  fi
  echo "  $f ok"
done

# YAML sanity for the issue forms — use python (mise installs it for the
# tooling stack; if absent, fall back to a no-op with a warning).
if command -v python3 >/dev/null; then
  for y in .github/ISSUE_TEMPLATE/*.yml; do
    python3 -c "import sys, yaml; yaml.safe_load(open('$y'))" || {
      echo "FAIL: YAML parse error in $y" >&2
      exit 1
    }
    echo "  $y parses"
  done
else
  echo "  WARN: python3 not found; skipping YAML parse check"
fi

echo "VERIFY OK"
```

Make it executable (`chmod +x verify.sh`).

### 8. Commit + verify

One commit covers the lot — these files are a single OSS-hygiene drop:

```
git add .github/ verify.sh
git commit -m "chore(oss): GitHub issue/PR templates + CODEOWNERS"
./.plans/upnext/0057-github-issue-pr-templates/verify.sh
```

## Notes

- Issue Forms (`.yml`) over legacy templates (`.md`) — they enforce structure (validations, dropdowns) and render nicer in the GitHub UI. The legacy `.md` form is still supported but deprecated for new repos.
- `blank_issues_enabled: false` plus a Discussions link is the standard "channel people to the right place" config. If Discussions isn't enabled on the repo, the link 404s but doesn't break anything.
- CODEOWNERS owners need write access to merge — `@karngyan` is the only maintainer for now. When more land, add one line per directory pattern.
- We do **not** add `.github/dependabot.yml` here — that's CI/automation territory and belongs with 0058.
- We do **not** add `SECURITY.md` here — out of scope, separate hygiene task if/when we want it.
