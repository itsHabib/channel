# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Dev Commands

```bash
make build      # build ./cmd/channel → ./channel
make install    # build + copy to $GOBIN
make test       # go test -race ./...
make lint       # go vet + golangci-lint (Dave Cheney config)
make check      # lint + test + build (full validation)
make run        # go run ./cmd/channel
```

## Architecture

channel is an append-only message bus for agents sharing a machine. One channel = one JSONL file under `$CHANNEL_DIR` (default `~/.channel`); a message is one JSON line `{ts, from, body}`.

- **`internal/store/`** — the mechanism: appends are a single write(2) on an O_APPEND descriptor under an exclusive flock (`lock_unix.go`); reads resume from an opaque byte-offset cursor and stop cleanly at a torn final line; `List` scans the dir. Name validation rejects path traversal.
- **`internal/server/`** — the MCP surface: `channel.post` / `channel.read` / `channel.list` over stdio via the official go-sdk. Tool descriptions are agent-facing product surface — they say *when* to reach for a channel, not just what the verbs do. Keep them that way.
- **`cmd/channel/`** — CLI (`post` / `read --since --limit --follow --json` / `list`) and the `mcp` subcommand; `--as` falls back to `$CHANNEL_AS`, a body of `-` reads stdin.

Invariants: the log is append-only (never edit/delete lines); cursors are opaque tokens (never computed by callers); `from` is self-declared — impersonation is out of the threat model, and adding keys/roles here is how huddle got heavy. The `post`/`read --since` contract is the seam a future HTTP backend would satisfy; it stays unwritten until remote agents actually need it.

<!-- BEGIN dev-workbench (managed by /dev-workbench skill — re-run to refresh; hand-edits inside this block will be overwritten) -->
## Dev workbench

These MCPs, planes, and skills are available in any agent session on this machine; the harness injects each tool's signature, so this is the *map* — how they compose — not the per-verb manual. **This is channel — the agent message bus itself** — so the channel verbs are the most directly relevant here. When the signal matches, call the verb; don't ask permission. Stuck on a *knowledge* question about another portfolio repo → `/consult` its steward; only *authority* questions (direction, spend, irreversible calls) go to the operator.

**MCPs (in-session):**
- **dossier** — durable project memory: projects → phases → tasks → artifacts (markdown-on-disk).
- **ship** — the driver engine: dispatch a task to a cloud/local agent and persist the run (dispatch→poll→judgment→land→record); inspect/cancel/replay.
- **channel** — *optional* agent message bus (append-only JSONL, `channel.post/read/list`); post/read to coordinate with peer agents or leave word for the operator; off the normal PR path.
- **playwright** — browser automation when a task needs a real DOM.

**Planes (workbench tenants — CLIs composed via exit codes + JSONL, not MCPs; `itsHabib/workbench` `cmd/<tool>`):**
- **gate** — the flagship: authorization. Evaluates the *exact* PR head against an operator-minted grant + the escalate-only verifier ladder; hash-chained audit log; exit 0 pass / 1 blocked / 2 parked / 3 refused / 4 error. Findings ≠ authorization; gate is the merge boundary. State + keys stay `~/pers/gate`.
- **flare** — notification: best-effort escalation sink over authoritative receipts → its own Slack app/channel. Pure sink; never gates; not built on huddle.
- **console** — read-only local web view of gate's inbox (parked runs + grant ledger); shells the gate binary, owns no authoritative state.
- **escalate** — the agent→human→agent back-channel: ingests the human's decision for a parked escalation and drives `gate resolve`.

**Skills:**
- **/work-driver** [+ **/work-driver-prep**] — drive agent-led impl end-to-end; prep builds the specs + conflict-batched plan.
- **/pr-risk** — size how much review a PR needs (deterministic floor + agent advisory); upstream of the reviewers — it decides *how much*, they *do* it.
- **/review-coordinator** [+ **/review-digest**] — consolidate the AI PR reviewers into one verdict (the judge over the finders); digest pre-triages the bot pile locally.
- **/shipped** · **/status** · **/wip** — retrospective recap · in-flight update · cross-store live board.
- **/consult** — summon a sibling repo's steward for a same-turn answer; knowledge → peer, authority → operator.
- **/worktree-*** — add · list · remove · transfer · where, over `git worktree`.

### The loop

```
dossier task → /worktree-add → spec → ship driver (cloud-first: dispatch→poll→judgment→land→record)
   → PR + CI → /pr-risk tiers it → reviewers fire → /review-coordinator → one verdict
   → gate evaluates the exact head → 0: governed-path authorization → merge
   → authoritative receipts → dossier close-out → /worktree-remove
        ↘ 2: gate PARKS → console / gate next surface it → human decides → escalate → gate resolve → re-judge
        ↘ any attention/terminal receipt → best-effort flare sweep → Slack   (independent; never gates)
```

`/work-driver` coordinates dispatch→poll→land and runs its own review triage inline. `/pr-risk` and `/review-coordinator` are steps you *invoke* — the driver→pr-risk / driver→coordinator wiring is planned, not built, so nothing here auto-delegates.

### Why this shape

Each layer owns one responsibility and is swappable without rippling: dossier owns *what needs doing*; worktree skills own *where work happens*; ship owns *drive an agent + persist the run*; pr-risk owns *how much review*; review-coordinator owns *consolidate the finders* (the bots are swappable under it); **gate owns *authorization* — is this exact head allowed to merge — which is not the reviewers' findings**; **escalate owns *resolution* — closing the agent→human→agent loop a park opens, without ever deciding for the human**; **console owns the *read-only view* of gate's inbox — it explains, never decides**; **flare owns *notification* — a best-effort sink on authoritative receipts, its own Slack app, never blocking the driver, never depending on huddle**; consult owns the stuck path; channel owns optional agent-to-agent messaging (superseding huddle); playwright owns browser. The workbench is a menu, not a checklist — skip what a flow doesn't need.

### The shape underneath

These tools instantiate the redesign's five contract planes — coupled only by typed artifacts (`evidence → verdict → action`), never call stacks:

- **State** (remembers) — dossier + gate's hash-chained log + run/verdict/grant/receipt artifacts; the append-only substrate.
- **Execution** (does) — ship's driver; emits evidence, never judges itself.
- **Verification** (judges) — the escalate-only ladder (deterministic floor → local → premium), monotone `worst`/`max`: gate's reducer, review-coordinator, triage/tracelens.
- **Capability** (bounds) — scoped/timed grants; every effectful verb needs a live grant + a supporting verdict.
- **Observability** (explains) — read-only, storeless views from State: flare, console, /wip, /shipped, /status.

This section is the sixth — **Composition**: the agent + thin policy choosing which planes a task needs. gate is the flagship — the one tool spanning Verification + Capability, holding the merge boundary. The boundaries above *are* the plane laws, not conventions.
<!-- END dev-workbench -->
