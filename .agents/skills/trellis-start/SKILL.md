---
name: trellis-start
description: "Initializes an AI development session by reading workflow guides, developer identity, git status, active tasks, and project guidelines from .trellis/. Classifies incoming tasks and routes to brainstorm, direct edit, or task workflow. Use when beginning a new coding session, resuming work, starting a new task, or re-establishing project context."
---

# Start session

Run `python3 .trellis/scripts/get_context.py` for identity, active task and Git
state, then `python3 .trellis/scripts/get_context.py --mode phase` for this
project's authoritative routing and authorization rules. Report any full
`Trellis update available:` line verbatim.

Use an exact task path when continuing existing work; read its contract,
authorization and latest progress. Do not infer permission or next action from
status alone. No active task is normal for small settled work. Use the loaded
workflow to decide whether a task or discovery is needed.

Before coding, discover applicable spec layers with `get_context.py --mode
packages`, then use `trellis-before-dev` or a context-loaded implementation
agent. Load step details only when needed. Do not preload the full workflow,
README, every guide or validation history.
