# Matt entry and handoff

Load on an explicit Matt skill invocation or resume of a recorded Matt task. This is routing
and runtime adaptation; methodology stays in the genuine installed skills.

## Select and authorize

Use the genuine installed skills with their stock invocation policy. Claude Code
uses its skill mechanism; Codex uses its supported skill loading, including
ordinary file reads when permitted. Grok Build has no Skill tool: it loads a skill
by reading its `SKILL.md` under `.grok/skills/`; a `disable-model-invocation: true`
skill stays a user-typed slash command. Record the source path. A catalog listing
isn't execution, and task-context loading doesn't authorize skill invocation.
For policy inspection, Codex uses `agents/openai.yaml`, not Claude's frontmatter;
Claude Code uses `disable-model-invocation`. Keep both installer-produced files intact.
Setup and installation evidence are linked from README.

The entry skill sets the record size; add no Trellis task unless it says so:

- `grill-with-docs` (discovery in a repo) is stateful and **bootstraps at entry**:
  before the first interview question, run `get_context.py` and
  `get_context.py --mode phase`, then `task.py create "…" --slug … --description "…" --no-start`
  (or reuse the task path the user named). Grill into that task: persist settled
  decisions in `prd.md` / Notes as you go. Do not run a recoverable grill with no
  task. At a later user-only boundary, refresh the same record and give the exact
  next command.
- `wayfinder` (uncertainty spanning sessions) is stateful: use the task record
  below at user-only boundaries. Wayfinder is planning-only until the user
  authorizes delivery; a resolved decision ticket isn't an implementation ticket.
- `grill-me` is stateless: no task, glossary or ADR. Its result stays in the
  conversation; suggest `grill-with-docs` when a recoverable record is wanted.
  Confirmed understanding is not edit authority: end with the `implement` command
  for the agreed one-line scope and pause; do not edit product code as part of grill-me itself.
- `implement` builds small work from the request or conversation. No spec,
  tickets or task unless the work must survive the session.
- `implement-spec` needs a published spec with tickets. Without them, say so and
  offer `implement`; never fabricate a spec or tickets to satisfy it.
- Model-invocable skills such as `diagnosing-bugs` and `tdd` may serve either
  route without switching it.

Use the configured Trellis Other tracker, never infer GitHub/GitLab publication
from a remote.
Explicit selection keeps this route for its work item until done or until the user
switches it. Settled requirements, small size or general implementation authority
don't switch it. Unrelated later work defaults to normal Trellis.

Resolve material questions with the real skill. Record the user's answers and
scope, testing seam, delegated engineering decisions, commit/closeout permission,
and any stop condition. Combine overlapping questions and reuse recorded answers.
Existing answers satisfy later skill questions only when they actually cover
them. Never invent approval. Explicit
planning-only requests remain planning-only.

## Continue or hand off

Select only the next needed skill: `to-spec` when a durable spec is useful;
`to-tickets` when decomposition helps; `implement` for ready work; `code-review`
for qualitative review. A small settled contract may go directly to `implement`.
Avoid duplicate Trellis interviews, specifications and reviews.

Continue while the installed skill and harness permit it. When the next skill
requires user invocation, save the current context: for a stateful entry that has
not yet bootstrapped (`wayfinder`, or a resumed grill), create (workflow 1.0,
`--no-start`) or reuse the minimal task; `grill-with-docs` should already hold a
task from entry. For same-session small work after `grill-me`, the conversation
suffices. Give the exact next command: `$<skill> <reference>` in
Codex or `/<skill> <reference>` in Claude Code or Grok Build, replacing the placeholders with the
selected skill and the actual task path or a one-line agreed scope. Explain
the stock policy boundary and pause. Scope authorization remains valid, but
doesn't replace the required invocation. Do not bypass that boundary
through direct reads, copied or renamed skills, wrappers, metadata edits, or
ordinary inline work in place of the needed skill.
If a required skill is missing, report the prerequisite without reconstructing it.

With a task, persist actual decisions, route, source/output paths, authorization,
fixed point, ticket/blocker/ownership map, progress, checks and the next command in
its Notes/implement.md; record only what happened.
Keep one source for each fact. Resume reads this record and current files, retains
approved decisions and honors any pending invocation boundary. A Trellis resume
command does not invoke a pending user-only Matt skill.
When the user invokes the skill for a Trellis task still in `planning`, activate
first (workflow 1.4, `task.py start <task>`); the skill's own steps follow.

## Delegate and join

Delegate within the invoked skill's permitted scope; dispatch cannot satisfy a
different skill's user-only boundary. Use actual runtime tools. When useful,
spawn independent ready tickets before waiting for either (Claude Code: several
`Agent` calls in one message; Grok Build: several `spawn_subagent` calls with
`subagent_type` `trellis-implement`, `trellis-check` or `trellis-research`, not a
generic executor). Give each worker an exact task path as a line
`Active task: <path>`, real skill path, acceptance, blockers, owned files and
applicable spec pointers. Claude's hook resolves that marker; Grok workers load it themselves as their
`.grok/agents/trellis-*.md` definitions instruct. Codex's native
start event lacks the dispatch prompt: it supplies loading instructions only;
the worker validates and explicitly loads its task after receiving the dispatch.
Invalid or conflicting identity stops loading, never falls back to another task.
Parent-session fallback is only for a dispatch with no explicit task marker.
Tell workers they share a checkout, must preserve others' edits, and must not
mutate task lifecycle, shared indexes or Git. Use
separate worktrees if ownership overlaps. A worker reads its missing context
explicitly; native injection must be observed, not inferred from the agent name.
Claude Code rejects report-file writes from subagents: have research workers
return findings, then persist them as coordinator.

The coordinator owns shared task state, commits and final review. Workers may
execute the real implement skill's code/test responsibilities; its shared review
and commit responsibilities return to the coordinator. This avoids nested review
agents hitting agent depth limits and competing Git index writes. Join all results,
verify accepted blockers, then start dependent integration. Capacity or unavailable
tools can require sequential execution; record that accurately.

## Review, fixing and closeout

Capture a resolvable base before implementation and record the initial dirty
paths. Inspect the installed `code-review` contract when invoked; updates may
change which diff it reviews. If it reviews only `git diff <base>...HEAD`, run
focused checks and create an authorized local checkpoint commit before review
against the saved base and exact spec/ticket paths. Without commit permission,
record that review as pending. If the installed skill supports working-tree
review, follow its actual contract. Report the revision and changes actually
reviewed; an empty diff never establishes acceptance of uncommitted work.

After verified findings, fix in scope, recheck affected behavior and commit the
correction when authorized; rerun affected review axes when invocation is permitted.
Limit unsuccessful repair cycles to three. Use the Trellis checker for remaining
project/executable checks, allowing bounded fixes. Reuse successful checks tied to
an unchanged revision and scope. Final acceptance covers the combined result.

Use real `retro` when installed and requested or warranted, honoring its policy
and dependencies. If unavailable, report that fact; do not restore old copies or
patch the installation to supply it. Retro proposes environment improvements;
use `trellis-update-spec` only for a durable contract learned. Neither requires
the other. Record no-change honestly. Authorized closeout follows workflow 3.5;
archive children before parent, use no-auto-commit flags, and never push by default.
