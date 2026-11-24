# Journal - ycg (Part 1)

> AI development session journal
> Started: 2026-11-18

---



## Session 1: CodeFree proxy tool compatibility and model checks
<!-- trellis-session: v=2 fp=08de20a7d79b3f98 -->

**Date**: 2026-11-23
**Task**: CodeFree proxy tool compatibility and model checks
**Branch**: `main`

### Summary

Fixed the Codex and Claude tool round trips and deployed the proxy locally. Tested the five exposed models and verified their advertised context sizes and observed input limits.

### Main Changes

- Removed the upstream StructuredOutput request for client tool turns and preserved the tool identity and call results across both APIs.

### Git Commits

| Hash | Message |
|------|---------|
| `50afbc4` | Fix tool calls through the CodeFree proxy |

### Testing

- [OK] go test ./... and go vet ./... passed.
- [OK] Codex executed a read-only shell command and Claude Code used Read through the local proxy.
- [OK] All 40 reasoning requests returned HTTP 200. The CodeFree model catalog declared no selectable variants.

### Status

[OK] **Completed**
