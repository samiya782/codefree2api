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
