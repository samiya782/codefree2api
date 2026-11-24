# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

<!--
Document your project's quality standards here.

Questions to answer:
- What patterns are forbidden?
- What linting rules do you enforce?
- What are your testing requirements?
- What code review standards apply?
-->

(To be filled by the team)

---

## Forbidden Patterns

<!-- Patterns that should never be used and why -->

(To be filled by the team)

---

## Required Patterns

<!-- Patterns that must always be used -->

(To be filled by the team)

---

## Testing Requirements

Tool protocol changes in `internal/proxy` need a two-turn HTTP regression. Assert
that the streaming tool call contains the tool identity and arguments the client
uses for dispatch, then replay the tool result and check the upstream prompt.
For Responses, the completed output must match the streamed item ID, call ID,
and output index. For Messages, a tool turn must end with `stop_reason: tool_use`.

When testing codefree-o with client tools, check that its `/message` request
omits `format: json_schema`. That setting adds an upstream `StructuredOutput`
tool which can interrupt the client tool turn.

---

## Code Review Checklist

<!-- What reviewers should check -->

(To be filled by the team)
