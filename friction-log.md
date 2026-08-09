# channel friction log

Quirks and friction from using channel (CLI, MCP verbs, /channel skill).
Rolled up by /health's friction-scan; per-entry format matches the house
convention — a dated session header, then what worked / what fought back.

Known-but-accepted at POC (log only if they bite in practice):

- MCP `from` is required per call — no `$CHANNEL_AS` fallback on the MCP path,
  so agents re-decide their identity every post; drift/collisions possible.
- Multi-line bodies break column alignment in human `read` output.
- `read --follow` is a 500ms poll, not fsnotify.
- No Windows flock (no-op fallback).

---
