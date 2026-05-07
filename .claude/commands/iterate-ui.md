---
description: Resume a UI iteration loop after a context reset. Reloads shadcn + refactoring-ui skills, reads the latest TASK.md and RESULT.md, and restores conversation context.
argument-hint: <task-id>
---

You are resuming work on an in-progress UI task. The previous session may have ended; this command restores the working context.

## Step 1 — Resolve the task

The user passes a task id as `$ARGUMENTS` (e.g. `0051`). Find `.plans/upnext/<id>-*`. If not found, check `.plans/completed/<id>-*` and error: "Task already completed. Use `/plan-task` for further changes."

## Step 2 — Reload skills

Invoke these via the `Skill` tool, in order:
1. `shadcn`
2. `refactoring-ui`
3. If `frontend-design` appears in the task's `skills:` frontmatter, invoke it too.

## Step 3 — Reload context

Read in this order:
1. The task's `TASK.md` (full file).
2. The task's `RESULT.md` if present (this is the agent's prior summary of what was implemented).
3. The task's `notes.md` and any other scratch files in the task dir.
4. The most recent ~5 git commits scoped to the task (use the task id in the commit message as the filter).
5. `docs/superpowers/specs/2026-05-08-tempo-design.md` (frontend section + the relevant route(s)).

## Step 4 — Boot the dev server

If the dev server is not already running, start it: `make dev` (or print the command for the user to run).

## Step 5 — Surface the resumption

Print to the user:
- What the task is.
- What's been implemented so far (from RESULT.md).
- Where to look in the running app.
- A 1-sentence prompt: "What would you like to iterate on?"

Then wait for the user's direction.
