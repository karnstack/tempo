---
description: Scaffold a new task directory under .plans/upnext/ with smart-defaulted frontmatter.
argument-hint: "<title>"
---

You are creating a new task in tempo's `.plans/upnext/`. The user has passed the title as `$ARGUMENTS`.

## Step 1 — Pick an id and slug

1. Scan both `.plans/upnext/` and `.plans/completed/` for existing ids (the leading 4-digit prefix on each directory name).
2. Pick the next free id (lowest unused 4-digit number, zero-padded).
3. Generate a slug from the title: lowercase, dashes for spaces, no other punctuation. Cap at ~50 chars.

## Step 2 — Smart defaults

Infer from the title and the master plan (`docs/superpowers/plans/2026-05-08-tempo-implementation.md`):

- `autonomy`:
  - If the title mentions UI, frontend, dashboard, page, route, component, panel, settings, login, register → `review`.
  - Else → `full`.
- `skills`:
  - UI tasks → `[shadcn, refactoring-ui]`. Add `frontend-design` if the task is a brand-new page or surface.
  - Backend tasks involving GitHub API, ingestion, or hard concurrency → `[systematic-debugging]`.
  - Else → `[]`.
- `tags`: extract obvious topical tags from the title (e.g. `[ui, dashboard]`, `[ingest, github]`).
- `est_minutes`: leave blank for the user to fill in.
- `depends_on`: leave empty unless the user has hinted at deps in the title.

## Step 3 — Create the directory

Create `.plans/upnext/<id>-<slug>/` with:

- `TASK.md` containing the frontmatter (filled per Step 2) and a body skeleton:

  ```markdown
  ## Goal

  …one paragraph describing what done looks like…

  ## Acceptance criteria

  - [ ] …
  - [ ] …

  ## Files to touch

  - …

  ## Steps

  - [ ] **Step 1** — …
  - [ ] **Step 2** — …

  ## Notes

  …anything the next agent needs to know…
  ```

- `verify.sh` with shebang and a placeholder body that exits 1 (forces the agent to write a real verification later):

  ```bash
  #!/usr/bin/env bash
  set -euo pipefail
  echo "TODO: write verification" >&2
  exit 1
  ```
  Make it executable (`chmod +x verify.sh`).

## Step 4 — Confirm with the user

Print the chosen id, slug, frontmatter, and dir path. Ask the user to fill in the Goal / acceptance criteria, or offer to flesh them out yourself based on the title and the master plan.

Do **not** add the new task to the master plan automatically — let the user decide whether it's planned scope or ad-hoc work.
