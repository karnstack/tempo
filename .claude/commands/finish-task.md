---
description: Promote an in_progress (review-mode) task to completed/. Re-runs verify, writes final RESULT.md, moves the dir, commits.
argument-hint: <task-id>
---

You are promoting a task from `.plans/upnext/` to `.plans/completed/` after the user has approved its UI/output.

## Step 1 — Resolve the task

The user passes a task id as `$ARGUMENTS` (e.g. `0047`). Find the matching directory under `.plans/upnext/<id>-*`. If multiple match, error. If none match, error.

## Step 2 — Verify state

- Read `TASK.md`. Confirm `status` is `in_progress`. If `status` is `pending` or `failed`, ask the user to confirm before proceeding (a `pending` task hasn't been touched; a `failed` one wasn't fixed).
- Confirm a `RESULT.md` exists. If not, generate one from the current diff.

## Step 3 — Re-run verify

Run `./verify.sh` from the task dir. If it fails, surface the failure and ask the user whether to override. **Do not auto-promote on a failed verify** unless the user explicitly says "override".

## Step 4 — Update RESULT.md

Append a "Final" block to `RESULT.md`:
- Final verify output (last ~30 lines).
- Any user-driven changes since the initial implementation (look at recent commits).
- Final approval timestamp.

## Step 5 — Promote

1. Update `TASK.md` frontmatter: `status: done`.
2. `git mv .plans/upnext/<id>-<slug> .plans/completed/<id>-<slug>`
3. `git add -A && git commit` with message `feat(<area>): <task title> (#<id>)`.
4. Print a 1-line summary plus the next available task (peek `.plans/upnext/` for unblocked tasks).
