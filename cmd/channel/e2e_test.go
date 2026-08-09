package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/itsHabib/channel/internal/server"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// channelBin is the compiled binary shared by the e2e tests. Built once in
// TestMain so these tests exercise the real argv parsing and true process
// boundaries — the one thing the in-process run() tests can't reach.
var channelBin string

func TestMain(m *testing.M) {
	os.Exit(buildAndRun(m))
}

func buildAndRun(m *testing.M) int {
	dir, err := os.MkdirTemp("", "channel-e2e")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	channelBin = filepath.Join(dir, "channel")

	build := exec.CommandContext(context.Background(), "go", "build", "-o", channelBin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build channel binary: " + err.Error())
	}

	return m.Run()
}

// runBin invokes the built binary with CHANNEL_DIR=dir and returns
// stdout, stderr, and the exit code.
func runBin(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), channelBin, args...)
	cmd.Env = append(os.Environ(), "CHANNEL_DIR="+dir)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run %v: %v", args, err)
		}

		code = ee.ExitCode()
	}

	return stdout.String(), stderr.String(), code
}

func TestE2ECLIRoundtrip(t *testing.T) {
	dir := t.TempDir()

	_, _, code := runBin(t, dir, "post", "--as", "alice", "standup", "shipping the bus")
	require.Equal(t, 0, code)

	out, _, code := runBin(t, dir, "read", "--json", "standup")
	require.Equal(t, 0, code)

	var res struct {
		Messages []struct {
			From string `json:"from"`
			Body string `json:"body"`
		} `json:"messages"`
		Cursor string `json:"cursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Messages, 1)
	require.Equal(t, "alice", res.Messages[0].From)
	require.Equal(t, "shipping the bus", res.Messages[0].Body)

	listOut, _, code := runBin(t, dir, "list")
	require.Equal(t, 0, code)
	require.Contains(t, listOut, "standup")

	sinceOut, _, code := runBin(t, dir, "read", "--since", res.Cursor, "--json", "standup")
	require.Equal(t, 0, code)

	var since struct {
		Messages []json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(sinceOut), &since))
	require.Empty(t, since.Messages)
}

func TestE2EExitCodes(t *testing.T) {
	dir := t.TempDir()

	_, _, code := runBin(t, dir, "--version")
	require.Equal(t, 0, code)

	_, _, code = runBin(t, dir, "frobnicate")
	require.Equal(t, 2, code)

	_, _, code = runBin(t, dir)
	require.Equal(t, 2, code)

	_, stderr, code := runBin(t, dir, "read", "ghost")
	require.Equal(t, 1, code)
	require.Contains(t, stderr, "channel not found")
}

func TestE2EStdinBody(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.CommandContext(t.Context(), channelBin, "post", "--as", "reporter", "reports", "-")
	cmd.Env = append(os.Environ(), "CHANNEL_DIR="+dir)
	cmd.Stdin = strings.NewReader("line one\nline two\n")
	require.NoError(t, cmd.Run())

	out, _, code := runBin(t, dir, "read", "--json", "reports")
	require.Equal(t, 0, code)
	require.Contains(t, out, `line one\nline two`)
}

// TestE2ECrossProcessAtomicity is the load-bearing test: many separate OS
// processes appending large bodies to one channel must never produce a torn
// or interleaved line. Large bodies force Go's write loop to make multiple
// syscalls, so this genuinely exercises the flock — not just single-syscall
// O_APPEND atomicity that would hold for small lines regardless.
func TestE2ECrossProcessAtomicity(t *testing.T) {
	dir := t.TempDir()

	const writers, perWriter = 6, 10

	body := strings.Repeat("x", 40000)

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)

		go func(w int) {
			defer wg.Done()

			for i := range perWriter {
				_, _, code := runBin(t, dir, "post", "--as", fmt.Sprintf("w%d", w), "busy",
					fmt.Sprintf("%d-%d-%s", w, i, body))
				if code != 0 {
					t.Errorf("writer %d post %d exited %d", w, i, code)
				}
			}
		}(w)
	}

	wg.Wait()

	out, _, code := runBin(t, dir, "read", "--json", "busy")
	require.Equal(t, 0, code)

	var res struct {
		Messages []json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Messages, writers*perWriter)

	// Every stored line must be a complete, valid message — a torn or
	// interleaved append would fail this unmarshal or lose from/body.
	for _, raw := range res.Messages {
		var msg struct {
			From string `json:"from"`
			Body string `json:"body"`
		}
		require.NoError(t, json.Unmarshal(raw, &msg))
		require.NotEmpty(t, msg.From)
		require.Len(t, msg.Body, len(body)+4) // "w-i-" prefix + body
	}
}

// TestE2EMCPStdio drives the MCP server as a real subprocess over stdio via
// the SDK's CommandTransport — the actual transport agents use, not the
// in-memory one — and confirms both surfaces share one store.
func TestE2EMCPStdio(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	cmd := exec.CommandContext(ctx, channelBin, "mcp")
	cmd.Env = append(os.Environ(), "CHANNEL_DIR="+dir)

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "test"}, nil)

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	_, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "channel.post",
		Arguments: map[string]any{"channel": "mcp-e2e", "from": "prod", "body": "over real stdio"},
	})
	require.NoError(t, err)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "channel.read",
		Arguments: map[string]any{"channel": "mcp-e2e"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out server.ReadResult
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Len(t, out.Messages, 1)
	require.Equal(t, "over real stdio", out.Messages[0].Body)

	// The CLI reads the same file the MCP server just wrote.
	cliOut, _, code := runBin(t, dir, "read", "--json", "mcp-e2e")
	require.Equal(t, 0, code)
	require.Contains(t, cliOut, "over real stdio")
}
