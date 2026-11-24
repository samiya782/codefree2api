# Issue tracker: Trellis Tasks (Other)

Real Matt skills own the content; this backend maps publish/fetch/claim/close to
Trellis. Run `python3 .trellis/scripts/task.py --help` for installed commands.
The coordinator alone writes lifecycle, metadata and shared parent indexes;
workers return results. No concurrent `task.py create --parent` or Git mutations.

## Identity and lifecycle

Create with `task.py create "title" --slug name --description "outcome" --no-start`
(and `--parent <exact-path>` for children). Capture the printed path. The identity
is `trellis-task:<complete-MM-DD-basename>`, never the bare task.json id. Fetch
from `.trellis/tasks/<basename>` or exactly one matching directory under
`.trellis/tasks/archive/`. Zero or multiple matches is a missing/ambiguous identity.
Read task.json and the published body; verify metadata and parent references.

Use supported `task.py set-meta <task> <key> <value>` with string values:

- `matt_publication_ref=trellis-task:<basename>`
- `matt_publication_kind=spec|ticket|wayfinder-map|wayfinder-ticket`
- `matt_ready_for_agent=false`, then `true` after publication validation
- tickets: `matt_parent_ref=trellis-task:<parent-basename>`
- specs: `matt_spec_artifacts=prd.md` (legacy split specs: `prd.md,design.md`)

Readiness is complete publication, not permission or completion. Native states
remain `planning`, `in_progress`, `completed`. `start` activates authorized work;
`finish` only clears this session's pointer. Never start just to bind planning.

## Publish and fetch a specification

Reuse the task for this request; never overwrite another non-placeholder body.
Persist the **complete real `to-spec` output** in `prd.md`, preserving its sections
and decisions. No second Acceptance Criteria projection or mandatory design copy.
Fetch every artifact listed by `matt_spec_artifacts`, including legacy design files.
Implementation Decisions belongs in the canonical spec unless the existing
publication is split. Keep scope authorization and operational notes distinct
from the skill output. No ticket decomposition during specification publication.

Verify exact reference, nonempty full body and metadata before marking ready.
On failure leave false readiness and report the partial identity. Publishing an
approved spec does not itself activate code; the user's recorded delivery
scope may authorize the coordinator to proceed directly to needed next work.

## Publish and fetch tickets

When real `to-tickets` is needed, publish one child per approved ticket in blocker
order, preserving the complete body and acceptance criteria. Reuse existing
partial children instead of duplicating them. Each child's `## Blocked by` lists
one exact `trellis-task:<basename>` per blocker, or `- None`. Parentage groups
work; it is not a dependency. Record ownership separately from upstream content.

The parent `implement.md` references children and records progress; don't copy
all ticket bodies into it. Preserve spec content and pre-existing children.
Validate unique child references, bidirectional parent links, existing blockers,
no self/cyclic dependencies, complete bodies and context before setting the whole
batch ready. An incomplete batch stays false; retain recoverable partial objects.
Trellis's context validator does not validate this dependency graph.

Use `task.py add-context <child> implement|check <repo-relative-path> "reason"`
for needed spec/research sources, then `task.py validate <child>`. Resolve archived
paths before dispatch and repair moved context pointers; never inject stale paths.
No need to copy a child's own body into its manifests. Fetch child body, parent
artifacts and appropriate manifest before execution/review.

## Work the frontier

The coordinator selects ready incomplete work within the recorded authorization.
A blocker is satisfied by verified acceptance at a recorded revision in the
parent ledger, or by an archived completed task with verification evidence.
An agent's final message, readiness=true, or merely in_progress is insufficient.
This lets integration begin before the administrative archive step.

Record exact ownership before dispatch. Parallelize independent ready tickets
only when their file ownership is disjoint (or isolated). Start/record them
serially as coordinator; workers never write session pointers, parent JSON or
archive state. Wait for all required results before dependent integration.

Review passes exact child and full parent spec paths plus the saved Git base to
real `code-review`; it does not understand `trellis-task:` as a hosted issue.
Persist per-axis findings and executable evidence, allow bounded fixes, and use
workflow 2.2 for rechecking. Continue repairs within the invoked skill's scope;
if stock policy requires a new invocation, persist the next command and pause.

## Close

After verified acceptance and authorized closeout, archive children before parent:
`task.py archive <exact-task> --no-commit`. Inspect branch prerequisites first;
do not silently bypass validation. Parent archival clears links on active children,
so order matters. Stable publication references continue to resolve in archive.
Record journal with `add_session.py --no-commit`; commit only if authorized.
No automatic push, hosted issue, synthetic lifecycle, or blanket staging.

## Wayfinding

Real `wayfinder` publishes its full map into a top-level `prd.md` with kind
`wayfinder-map`; its decision children have kind `wayfinder-ticket`, a complete
`## Question`, and `matt_wayfinder_type=research|prototype|grilling|task`.
Coordinator claims with `set-meta <child> matt_claimed_by <developer>` before
work. Blockers use the same exact references. Resolve by appending `## Resolution`
and its durable context pointer to the map's Decisions so far; verify both files
and archive the decision child with `--no-commit` when authorized. Don't run
`start` for a planning decision. A delivery scope requires its own authorization.
