---
name: trellis-continue
description: "Resume work on the current task. Loads the workflow Phase Index, figures out which phase/step to pick up at, then pulls the step-level detail via get_context.py --mode phase. Use when coming back to an in-progress task and you need to know what to do next."
---

# Continue current work

Run `python3 .trellis/scripts/get_context.py` and
`python3 .trellis/scripts/get_context.py --mode phase`. The loaded project
workflow owns routing; this skill supplies recovery, not a second phase policy.

Resolve the exact active task with `task.py current --source` or the user's
reference. Read its contract, authorization, latest progress and current Git
state. Resolve archived references before loading moved context pointers.
Missing identity or a material decision warrants clarification; missing optional
artifacts, completed stage names, or an unchanged authorization do not.

Resume the first unfinished responsibility. For Matt, load
`docs/agents/matt-flow.md`, preserving recorded route and scope. Honor the needed
skill's stock invocation policy; persist context and give the exact next command
at a user-only boundary. This resume command does not invoke that skill.
Load `get_context.py --mode phase --step <X.Y> --platform codex` only
when its detail is needed. Never restart finished interviews or execute blocked
work because a lifecycle status suggests a numbered step.
