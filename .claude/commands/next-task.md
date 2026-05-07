---
description: Pick the next unblocked task from .plans/upnext/, load its skills, implement, verify, and (for backend tasks) move to completed.
---

You are picking up the next task from tempo's `.plans/upnext/` queue.

## Step 1 — Find the next task

1. List all task directories under `.plans/upnext/`. Each has a `TASK.md` with YAML frontmatter.
2. Parse each `TASK.md` frontmatter. Skip directories whose `status` is not `pending`.
3. For the remaining pending tasks, check `depends_on`. A dependency `<id>` is satisfied if a directory matching `.plans/completed/<id>-*` exists.
4. From tasks whose deps are all satisfied, pick the **lowest `id`**. Tie-break by alphabetical slug.
5. If no task qualifies, print: "No unblocked tasks. Run `/plan-task` to add one or check `.plans/upnext/` for failed tasks." and stop.

## Step 2 — Surface the task

Read the chosen `TASK.md` in full. Print to the user:

- The task id, slug, title.
- The Goal section.
- The Acceptance criteria checklist.
- The `autonomy` value (`full` or `review`).
- The `skills` list.

## Step 3 — Load skills

For each skill in the `skills:` frontmatter list, invoke it via the `Skill` tool **before any implementation work**. Examples:

- `shadcn` → loads composition rules, CLI workflow.
- `refactoring-ui` → loads visual hierarchy / spacing rules.
- `frontend-design` → loads design quality guidance for new surfaces.
- `systematic-debugging` → loads the debugging workflow for hard backend issues.

## Step 4 — Mark in_progress

Edit the task's `TASK.md` frontmatter: `status: in_progress`. Commit nothing yet — this is a working state.

## Step 5 — If TASK.md body is a stub

Many tasks live as frontmatter-only stubs. If the `TASK.md` lacks a "## Goal" or "## Steps" section beyond frontmatter, you must first **flesh it out**:

- Read the master plan at `docs/superpowers/plans/2026-05-08-tempo-implementation.md` to find the task's row (deps, autonomy, scope hint).
- Read the spec at `docs/superpowers/specs/2026-05-08-tempo-design.md` for the relevant section.
- Write a complete `TASK.md` body: Goal, Acceptance criteria, Files to touch, Steps (bite-sized 2–5 min each, with code blocks where code is being written), Notes.
- Write a `verify.sh` script that exits 0 on success.
- **Pause here and surface the fleshed-out plan to the user** for approval before continuing to Step 6. Do not auto-implement a stub task without showing what's about to happen.

## Step 6 — Implement

Follow the Steps section of `TASK.md` exactly. Make commits per the steps (small, frequent). If a step fails, debug it (load `systematic-debugging` if not already loaded). If you hit a true blocker, stop and write `FAILURE.md`.

## Step 7 — Verify

Run `./verify.sh` from the task directory. Capture stdout/stderr.

## Step 8 — Branch on autonomy

### If `autonomy: full`

- **On verify success:**
  1. Write `RESULT.md` in the task dir: what changed (file list), test/verify output (last ~30 lines), any followups.
  2. Update `TASK.md` frontmatter: `status: done`.
  3. `git mv .plans/upnext/<id>-<slug> .plans/completed/<id>-<slug>`
  4. `git add -A && git commit` with message `feat(<area>): <task title> (#<id>)`.
  5. Print a 1-line summary to the user.
- **On verify failure:**
  1. Write `FAILURE.md`: error output + your hypothesis about the cause.
  2. Update `TASK.md` frontmatter: `status: failed`.
  3. Leave the task dir in `.plans/upnext/`.
  4. Surface the failure to the user.

### If `autonomy: review`

- **On verify success:**
  1. Write `RESULT.md` in the task dir: what changed, verify output, **how to view it locally** (e.g., `make dev` then visit `http://localhost:5173/dashboard`), and which screens/components changed.
  2. Leave `status: in_progress` in `TASK.md`. Do **not** move the dir.
  3. Surface a "ready for UI review" message to the user with the screens to look at and a list of expected aesthetic concerns to consider (spacing, hierarchy, color usage, empty states).
  4. Stop. The user will iterate conversationally and run `/finish-task <id>` when satisfied.
- **On verify failure:** same as `full` (write `FAILURE.md`, status `failed`, leave in upnext).

## Notes

- Always commit between steps, not just at the end. Small commits make rollback easy.
- If a step's verification fails mid-way, debug before proceeding — don't paper over.
- For UI tasks, run `make dev` (or document how) so the user can poke the running app while you're stopped for review.
