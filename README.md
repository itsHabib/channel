# channel

An append-only message bus for agents sharing a machine. One channel is one
JSONL file; posting is one locked `write(2)`; reading is a byte-offset cursor.
No background service or channel lifecycle — a channel exists because someone
posted to it. Sender names are self-declared, not authenticated.

**A shared mailbox, not an agent runner.** Posting does not wake another agent
or prove that it read the message. Both agents must use the same store and
agree to read the channel. `--follow` displays new messages; it does not make
an agent respond, transfer a lease, or authorize work.

channel is the nimble successor to [huddle](https://github.com/itsHabib/huddle):
same core need (agents working the same problem need an attributed shared
channel, and the operator wants to watch), minus the Slack app, seat keys,
roles, and create/close ceremony that made huddle heavy. ~500 lines instead of
~3,600.

## Verbs

```sh
channel post [--as <sender>] <channel> <body...|->    # append (channel created on first post)
channel read [--since <cursor>] [--limit n] [--follow] [--json] <channel>
channel list [--json]                                 # channels, most recently active first
channel mcp                                           # serve channel.post/read/list over stdio MCP
```

`--as` falls back to `$CHANNEL_AS`, so an agent wrapper can set identity once.
A body of `-` reads stdin, for multi-line reports. `read --follow` tails a
channel and will happily wait on one that doesn't exist yet.

## Install

```sh
go install github.com/itsHabib/channel/cmd/channel@latest   # or @v0.1.0
```

Or clone and `make install` (builds `cmd/channel` → `$GOBIN/channel`).

## Quickstart: two agents and an observer

After installing, have both agents use the same channel name and store.
Introduce the channel through your agent harness's direct messaging feature,
if it has one, or include it in each agent's starting prompt.

Agent A posts a scoped request:

```sh
channel post --as agent-a pair-debug "Please review commit abc123 in the store layer. Reply here when you have read it; do not edit files."
```

Agent B reads and acknowledges:

```sh
channel read pair-debug
channel post --as agent-b pair-debug "Read your request for abc123. Reviewing the store layer; no edits."
```

The operator can watch in a terminal:

```sh
channel read --follow pair-debug
# Ctrl+C stops watching. It does not delete messages or stop either agent.
```

A successful post means **recorded**. A peer reply means **acknowledged**.
A review result with evidence means **completed**. Keep these separate.

### Multiline reports and incremental reads

```sh
channel post --as agent-b pair-debug - <<'REPORT'
Reviewed abc123.
Finding: the reader must retain its cursor before an incomplete final line.
Evidence: include the test command and result here.
REPORT

channel read --json --limit 10 pair-debug
# Pass the returned cursor unchanged on the next read:
channel read --json --since '<returned-cursor>' pair-debug
```

Save the returned cursor in your agent's task state. Do not calculate it from
message counts or timestamps. After an uncertain post, read recent messages
before retrying to avoid duplicate requests.

## Agent integration

Channel exposes `channel.post`, `channel.read`, and `channel.list` over stdio
MCP. Configure an MCP-capable client to launch `channel` with argument `mcp`.
Make sure the executable is on that client's PATH and its store matches the
other agent's. For Claude Code:

```sh
claude mcp add --scope user channel -- channel mcp
```

CLI access is sufficient when your client does not support MCP. Neither mode
subscribes an agent automatically: arrange reads at work boundaries or use your
harness's separately configured scheduling mechanism.

### Optional skill

[skills/channel/SKILL.md](skills/channel/SKILL.md) teaches discovery, direct
peer coordination, acknowledgment, and safe cursor handling. It is portable
between Claude and Codex; the MCP tools also work without installing a skill.

From this repository checkout, copy the folder into the home for your agent
(or use your skill manager). Inspect an existing installation before replacing it:

```sh
mkdir -p ~/.claude/skills ~/.codex/skills
cp -R skills/channel ~/.claude/skills/
cp -R skills/channel ~/.codex/skills/
```

Start a new agent session if your harness discovers skills only at startup.
This repository is the canonical source of the public channel skill. Catalogs
that distribute it should vendor a pinned revision, record that revision, and
submit improvements here before refreshing their copies. Installing this skill
does not install the Channel executable or configure MCP.

[AGENTS.md](AGENTS.md) and [CLAUDE.md](CLAUDE.md) are instructions for agents
**contributing to this repository**, not prerequisites for using the bus.

## Design

- **Storage:** `$CHANNEL_DIR` (default `~/.channel`), one `<name>.jsonl` per
  channel, one message per line: `{"ts":…,"from":…,"body":…}`.
- **Atomicity:** appends go through a single `write(2)` on an `O_APPEND`
  descriptor under an exclusive `flock`. Concurrent writers — goroutines or
  separate processes — never interleave within a line. Readers take no lock.
- **Cursors:** `read` returns an opaque cursor (today: a byte offset). Pass it
  back as `--since`/`since` to get only new messages. Never compute one.
- **Identity:** `from` is self-declared. Everything runs as you, on your
  machine; impersonation is not in the threat model. If that ever changes,
  that's the moment to reach for something heavier — not to add keys here.
- **Torn lines:** a reader that catches a writer mid-append excludes the
  partial line and parks the cursor before it; the next read picks it up.

## Multiple machines and copied logs

Channel has no built-in remote transport. Different machines normally have
separate stores. Copying or Git-syncing JSONL files is not live delivery, and
concurrent merges can reorder records, duplicate content, or change the byte
positions behind cursors. A conflict-free merge does not prove correct message
history. Do not reuse a cursor after its underlying file has been replaced or
rewritten; re-read and reconcile the history explicitly.

For occasional exchange, transfer reports into the destination through `post`
and retain their original attribution in the body. Treat shared/network
filesystems as a separate deployment to validate: local filesystem locking
assumptions do not establish cross-machine behavior.

## Non-goals (deliberately)

- No server or network protocol. The `post`/`read --since` verb contract is
  the stable seam; if remote agents ever need this, a ~100-line HTTP backend
  can satisfy the same contract behind `CHANNEL_URL`. It stays unwritten until
  then.
- No channel lifecycle, keys, roles, or Slack. See huddle for how that goes.
- No delivery guarantees beyond the file. Watching, mirroring, and notifying
  are layers on top of the JSONL, never in the write path.

## Development

```sh
make check   # vet + golangci-lint + race tests + build
```

Go 1.26, no runtime dependencies beyond the MCP SDK. CI runs tests (race),
golangci-lint, and govulncheck. See [CONTRIBUTING.md](CONTRIBUTING.md) for the
design invariants a change must respect.

## License

MIT — see [LICENSE](LICENSE).
