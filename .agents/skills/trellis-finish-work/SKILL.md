---
name: trellis-finish-work
description: "Finish authorized work and reconcile task archives, journal and Git after direct, interrupted or repeated closeout. Use when done coding or asked to finish the current session."
---

# Finish Work

Complete the authorized work and reconcile closeout, including direct calls and
interrupted or repeated attempts. Load `get_context.py --mode phase` and steps
3.4/3.5; the project workflow owns authorization and commit policy.

## 1. Reconcile before writing

```bash
python3 ./.trellis/scripts/get_context.py --mode record
```

Resolve the exact task from the current session or the user's reference, including
its stable identity under `.trellis/tasks/archive/` if already moved. Read its
contract, recorded authorization and progress, then inspect current Git status,
relevant commits and the developer journal/index. Lifecycle status alone proves
neither permission nor completion. Limit closeout to this scope and its children.

Continue the first unfinished authorized responsibility within stock skill and
runtime limits. For a pending Matt skill, follow `docs/agents/matt-flow.md` and
pause with the exact next command if user invocation is required.
Reuse checks only for unchanged revisions and scope; run missing checks or
fix verified failures within the recorded repair budget. Preserve planning-only,
no-commit and review boundaries. If acceptance or permission is still missing,
record the precise pending boundary; keep incomplete tasks active.

## 2. Resolve commits within scope

Classify all dirty paths, including task/workspace files, against the task's scope.
When commits are authorized, complete workflow 3.4 here: inspect changes, stage
only exact owned paths and commit with an explicit pathspec so unrelated staged
work stays out. Reconcile existing commits before retrying. If permission excludes
commits, leave changes for review and report that boundary; this does not prevent
other authorized work. A finish request never overrides an explicit no-commit rule.

## 3. Archive accepted work

After acceptance is verified and closeout is authorized, inspect command help and
branch preconditions. Archive children before their parent:

```bash
python3 ./.trellis/scripts/task.py archive <task-path> --no-commit
```

Resolve each stable task identity first. Skip an already archived child or parent;
resume the remaining archives. Leave unrelated tasks alone. Retain the resolved
archive path for journaling even after the active pointer is cleared. Report a
failed precondition; use a documented exception only when its conditions apply.

## 4. Record or resume the journal

Find any existing entry for this closeout in both journal and index before writing.
Use the same inputs and stable retry key for the same logical closeout, including
a retry after its journal was committed. Recover those inputs from task progress
or the existing entry; don't create a second entry for a repeated finish. Inspect
`add_session.py --help` for supported arguments.

```bash
python3 ./.trellis/scripts/add_session.py \
  --title "Session Title" \
  --commit "hash1,hash2" \
  --summary "Verified outcome and remaining boundaries" \
  --idempotency-key "task-slug-closeout-1" \
  --no-commit
```

Use actual work-commit hashes, or `--commit "-"` when none were made. The retry key
identifies one closeout operation (1–64 letters, digits, dots, underscores or
hyphens); choose a new key for distinct later work. If the journal append succeeded
but its index update failed, rerun the identical command to repair the index.
An already complete entry needs no append.

Both lifecycle commands always use `--no-commit`. If authorization includes
committing closeout records, inspect their final diff and commit only their exact
owned paths explicitly under step 3.4. Check Git, archive identities and the
journal/index again before reporting completion, evidence and any pending boundary.
