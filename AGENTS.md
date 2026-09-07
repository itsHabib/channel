# Contributing with an agent

Read [CONTRIBUTING.md](CONTRIBUTING.md) for design invariants and code style.
The CLI and MCP usage examples live in [README.md](README.md); the optional
consumer skill lives in [skills/channel/SKILL.md](skills/channel/SKILL.md).

## Commands

```sh
make build      # build cmd/channel
make test       # go test -race ./...
make lint       # go vet and golangci-lint
make check      # lint, race tests, build
```

CI also runs govulncheck; local make check does not include that scan.
Use a temporary CHANNEL_DIR for tests and examples so they do not write into
an operator's live message store. Never clean or rewrite existing channel logs.

## Architecture and boundaries

- internal/store owns append-only JSONL storage, locked appends, opaque cursors,
  partial-line handling, and channel-name validation.
- internal/server exposes channel.post/read/list over stdio MCP.
- cmd/channel owns CLI parsing and follow behavior.

Keep coordination policy outside the storage mechanism. A successful post is
not delivery acknowledgment, a wake-up, a lease transfer, or authorization.
Sender names are self-declared. Preserve the small local-tool scope; do not
introduce a scheduler, network transport, roles, or credentials incidentally.

Update the README and public skill when user-visible behavior changes. This
repository owns the skill source; downstream catalogs should pin their copies.
Do not assume contributors have any particular private toolchain, agent home,
portfolio directory, or merge-grant system beyond their own repository policies.
