// Package main is the channel CLI and MCP server: an append-only JSONL
// message bus for agents sharing a machine. Verbs: post, read, list, mcp.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/itsHabib/channel/internal/server"
	"github.com/itsHabib/channel/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "v0.0.1"

const usage = `channel — append-only message bus for agents sharing a machine

Usage:
  channel post [--as <sender>] <channel> <body...|->   append a message (- reads body from stdin)
  channel read [--since <cursor>] [--limit <n>] [--follow] [--json] <channel>
  channel list [--json]                                channels, most recently active first
  channel mcp                                          serve channel.post/read/list over stdio MCP
  channel --version

Channels live under $CHANNEL_DIR (default ~/.channel), one JSONL file each.
--as falls back to $CHANNEL_AS.
`

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		_, _ = fmt.Fprint(stderr, usage)

		return 2
	}

	switch args[1] {
	case "--version", "-version", "version":
		_, _ = fmt.Fprintf(stdout, "channel %s\n", version)

		return 0
	case "post":
		return fail(stderr, cmdPost(args[2:], stdout))
	case "read":
		return fail(stderr, cmdRead(args[2:], stdout))
	case "list":
		return fail(stderr, cmdList(args[2:], stdout))
	case "mcp":
		return fail(stderr, cmdMCP(stderr))
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[1], usage)

		return 2
	}
}

// fail is the single error-to-exit-code seam: nil is success, a usage error
// is 2, anything else prints and returns 1.
func fail(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}

	if errors.Is(err, errUsage) {
		_, _ = fmt.Fprint(stderr, usage)

		return 2
	}

	_, _ = fmt.Fprintf(stderr, "channel: %v\n", err)

	return 1
}

var errUsage = errors.New("usage")

// openStore resolves the store dir: $CHANNEL_DIR, else ~/.channel.
func openStore() (*store.Store, error) {
	dir := os.Getenv("CHANNEL_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}

		dir = filepath.Join(home, ".channel")
	}

	return store.New(dir)
}

func cmdMCP(stderr io.Writer) error {
	st, err := openStore()
	if err != nil {
		return err
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "channel", Version: version}, nil)
	server.Register(srv, st)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	_, _ = fmt.Fprintf(stderr, "channel MCP listening on stdio (dir %s)\n", st.Dir())

	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("mcp server: %w", err)
	}

	return nil
}
