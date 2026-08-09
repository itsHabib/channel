# Contributing to channel

Thanks for looking. channel is deliberately tiny — an append-only JSONL message
bus for agents sharing a machine — and the goal is to keep it that way. The most
useful contribution is usually a smaller one.

## Build & test

```sh
make check   # go vet + golangci-lint + race tests + build — the full bar
make test    # race tests only
make install # build cmd/channel → $GOBIN/channel
```

CI runs the same three checks (`test -race`, `golangci-lint`, `govulncheck`) on
every PR. `make check` green locally means CI will be green.

## Design invariants (please don't break these)

These are the decisions that keep channel small. A change that violates one is
probably the wrong change — or belongs in a heavier tool:

- **The log is append-only.** Never edit or delete lines in a channel file.
  `post` appends; nothing rewrites.
- **Appends stay atomic.** Writes go through a single `write(2)` on an `O_APPEND`
  descriptor under an exclusive `flock` (`internal/store`). Keep that path a
  single locked write — no read-modify-write, no partial lines.
- **Cursors are opaque.** `read` returns a token; callers pass it back as
  `since`. Today it's a byte offset — don't expose that, and don't compute
  cursors anywhere but the store.
- **Identity is self-declared.** `from` is a trusted string because everything
  runs as you, locally. Adding keys, auth, or roles is out of scope — that's the
  weight channel exists to avoid.
- **No server in the write path.** The `post` / `read --since` verb contract is
  the seam a future backend could satisfy; the file stays the source of truth.
  Watching, mirroring, and notifying layer on top, never inside.

See the **Non-goals** section of the [README](README.md#non-goals-deliberately)
for the fuller picture.

## Code style

Go, [Practical Go](https://dave.cheney.net/practical-go) lineage: line-of-sight
(handle errors with early returns, keep the happy path un-indented), shallow
nesting, small interfaces, errors wrapped with `%w`. `golangci-lint` enforces it
— match what's already there.

## Pull requests

- Keep the diff one logical change. Small is a feature.
- Add a test for any behavior change; the store's atomicity and the MCP surface
  both have tests to mirror (`internal/store`, `internal/server`, `cmd/channel`).
- Run `make check` before pushing.
- Explain the *why* in the description — the invariant it respects or the gap it
  closes.
