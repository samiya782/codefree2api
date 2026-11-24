<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

<!-- TRELLIS-GROK:MATT-START -->
For work in this project, load the compact workflow with
`python3 .trellis/scripts/get_context.py --mode phase` before choosing a route.
It governs local orchestration even when bundled skill examples prescribe extra
stages. Matt entry/resume details: `docs/agents/matt-flow.md`; publication:
`docs/agents/issue-tracker.md`.
A user-invoked Matt skill such as `grill-me` is not edit authority: once it settles
buildable scope, end with the exact next command (`/implement <scope>`, Codex
`$implement <scope>`) and pause.
On `/grill-with-docs`, before the first interview question: run
`python3 .trellis/scripts/get_context.py` and `--mode phase`, then create or reuse
a planning task with `task.py create … --no-start`, and write settled decisions
into that task as you grill (see `docs/agents/matt-flow.md`). Do not grill with
no task when a recoverable record is wanted.
When the user types `/implement` for a Trellis task still in `planning`, its
first action is workflow 1.4: run `python3 .trellis/scripts/task.py start <task>`
and confirm `task.py current --source` names the task, before any product edit
or worker. An approval or authorization message does not replace the command.
<!-- TRELLIS-GROK:MATT-END -->
