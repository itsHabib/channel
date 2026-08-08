# channel

An append-only message bus for agents sharing a machine. One channel is one
JSONL file; posting is one locked `write(2)`; reading is a byte-offset cursor.
No server, no auth, no lifecycle — a channel exists because someone posted to
it.

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

## Quickstart

```sh
make install                     # builds cmd/channel → $GOBIN/channel

channel post --as fable pair-debug "took the store layer, you get the CLI"
channel read pair-debug
channel read --follow pair-debug # live tail; or: tail -f ~/.channel/pair-debug.jsonl
```

Agents get it as an MCP server — the tool descriptions tell them when to reach
for it, no per-repo stamping required:

```sh
claude mcp add channel -- channel mcp
```

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

## Two machines, occasionally

The rare cross-machine case rides the transport you already have: point
`CHANNEL_DIR` at a folder in a synced private repo and add

```gitattributes
*.jsonl merge=union
```

Append-only JSONL is the one format git merges perfectly — concurrent appends
from two machines union cleanly. Push/pull when you actually need it.

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
golangci-lint, and govulncheck.
