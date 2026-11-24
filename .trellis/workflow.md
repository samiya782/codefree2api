# Development Workflow

## Phase Index

Normal Trellis is the default. Use Matt only when the user explicitly invokes a
Matt skill (e.g. `grill-with-docs`, `grill-me`, `wayfinder`, `implement`) or resumes
a recorded Matt task; then load `docs/agents/matt-flow.md`. On `grill-with-docs`,
bootstrap workflow 1.0 (`task.py create … --no-start`) and load `get_context`
before the first interview question; write into that task as you grill.
Publication uses `docs/agents/issue-tracker.md`.
That selection governs its work item: settled requirements, small size or general
implementation authorization never switch it to the normal route; only the user
can. Unrelated later work defaults to normal Trellis.

Continue within the user's scope and runtime permissions. At a user-only skill
boundary, persist context, give the exact next skill command with its task path
(or agreed scope for same-session small work), and pause. Otherwise ask only for
unresolved material decisions, scope changes, or missing permissions.
Authorization persists across stages and sessions when recorded with its scope.
This workflow replaces generic Trellis examples requiring repeated approvals or
fixed stages/artifacts; stock skill invocation controls still apply.

- Small settled normal-route work: edit, run relevant checks, report. No task
  ceremony.
- Reported bug symptom (wrong output, error, failure, slowness) without a stated
  fix: invoke stock `diagnosing-bugs` before reading code for a theory; it scales
  its own phases. A user-stated fix or typo stays small work. No task unless durable.
- Durable work: one task with contract, authorization, progress and evidence.
- Read applicable `.trellis/spec/` before coding. Implement and fix in scope;
  verify the combined result before closeout. Keep research and logs on demand.
- Resume from exact task artifacts and current files, not lifecycle status alone.

Details: `python3 .trellis/scripts/get_context.py --mode phase --step <X.Y>`.
Steps below are responsibilities to select as needed, not mandatory ceremonies.

## Phase 1: Plan

#### 1.0 Create task

Identify existing work with `task.py current --source`. Authorized work includes
local task bookkeeping. When useful, create with `task.py create "title" --slug
name --description "scope" --no-start`. Resolve ambiguous identity before lifecycle
changes; never change another session's pointer.

#### 1.1 Discovery and specification

Use `trellis-brainstorm` for unresolved normal-route requirements; Matt follows
its entry guide. Record scope, acceptance, decisions and authorization in `prd.md`.
Add artifacts only for useful information. Never invent an
answer or treat silence as approval.

#### 1.2 Research

Inspect code and primary sources before factual questions. Use independent
research agents when worthwhile and save concise evidence on demand.

#### 1.3 Tickets and context

Decompose only when separate deliverables help; record blockers and file ownership.
Curate necessary spec/research pointers in implement/check JSONL, then run
`task.py validate <task>`. Loaders already append task artifacts; don't duplicate
them or include placeholder guidelines. When no additional context is useful,
`start --allow-empty-context` is supported; empty-manifest validation still fails.

#### 1.4 Activate

Once material decisions and implementation authorization are recorded, the
coordinator runs `task.py start <task>`. Readiness metadata is not authorization;
unchanged scope needs no repeat approval. Workers never change shared lifecycle.

#### 1.5 Ready to execute

The contract, acceptance, authorization and next action must be recoverable.
Artifact count does not establish readiness; missing optional files is valid.

## Phase 2: Implement

#### 2.1 Implement

Normal work uses `trellis-implement` when delegation helps, otherwise
`trellis-before-dev` and inline work. Matt follows its entry guide. Parallelize
independent ready tickets with disjoint ownership or isolated worktrees. Give
workers explicit task paths, blockers and spec pointers; they load missing context
themselves. Inherited chat is not proof of injection. The coordinator alone owns
task lifecycle, shared indexes and Git. Join all results before integration;
report sequential execution honestly when concurrency is unavailable.

#### 2.2 Verify and repair

Run relevant executable checks and contract/standards review. Reuse evidence for
unchanged revisions and scope. Checkers may fix bounded defects; recheck affected
behavior and the integrated result. Stop after three unsuccessful repair cycles,
or earlier for a decision or permission boundary; persist evidence and next action.
Never count skipped, empty-diff or failed checks as PASS. Matt review details stay
in its entry guide; avoid duplicate reviews or suites for a stage name.

#### 2.3 Recover

Resolve exact active/archive paths, then read contract, authorization and progress.
Resume the first unfinished responsibility, retaining verified work and partial
publications. Honor blockers and pending skill commands. Clarify missing identity,
decisions or permissions; contract changes need renewed authorization.

## Phase 3: Finish

#### 3.2 Retrospective when useful

Use Matt `retro` only when requested or warranted, following its entry guide and
stock availability/policy. Repeated debugging may warrant `trellis-break-loop`.
No actionable lesson is a valid outcome.

#### 3.3 Preserve a lesson when warranted

Use `trellis-update-spec` only for a reusable repository contract or convention.
It is independent of retro. Prefer fixing faulty rules/checks over adding prose;
no-change is valid.

#### 3.4 Commit when authorized

Inspect changes and authorization; stage only owned paths and never push by
default. Follow the installed review's actual diff contract. Without commit
permission, leave any review/commit that requires it pending.

#### 3.5 Close out

After verified acceptance and authorized closeout, use `trellis-finish-work` to
reconcile prior commits, archives and journal entries. Archive children before
parent with `task.py archive <task> --no-commit`; journal with
`add_session.py --no-commit`. Respect branch preconditions, unrelated tasks and
any boundary requiring changes to remain for user review.

## Runtime breadcrumbs

[workflow-state:no_task]
Read the Phase Index. Normal-route small work proceeds directly; a reported bug symptom invokes diagnosing-bugs first. A Matt selection for this work item keeps its route: docs/agents/matt-flow.md. Honor scope and stock invocation boundaries.
[/workflow-state:no_task]

[workflow-state:planning]
Read the exact task contract, authorization and next action. Resolve decisions without repeating approvals. Matt: docs/agents/matt-flow.md; honor stock invocation boundaries.
[/workflow-state:planning]

[workflow-state:planning-inline]
Read the exact task contract and authorization; continue in scope. Matt: docs/agents/matt-flow.md; preserve context and pause at user-only skill boundaries.
[/workflow-state:planning-inline]

[workflow-state:in_progress]
Recover task context; implement, fix and recheck in scope. Join workers; coordinator owns lifecycle/Git. Matt: docs/agents/matt-flow.md; honor stock invocation boundaries.
[/workflow-state:in_progress]

[workflow-state:in_progress-inline]
Recover task context and specs; implement, fix and recheck in scope. Matt: docs/agents/matt-flow.md; honor stock invocation boundaries.
[/workflow-state:in_progress-inline]

[workflow-state:completed]
Verify evidence and reconcile authorized archive/journal work. Preserve pending boundaries and unrelated work; don't restart completed stages.
[/workflow-state:completed]
