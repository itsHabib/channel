package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/itsHabib/channel/internal/server"
	"github.com/itsHabib/channel/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// newSession stands up a real MCP client/server pair over in-memory
// transports with all three channel tools registered against a temp store.
func newSession(t *testing.T) *mcp.ClientSession {
	t.Helper()

	st, err := store.New(t.TempDir())
	require.NoError(t, err)

	srv := mcp.NewServer(&mcp.Implementation{Name: t.Name(), Version: "v0-test"}, nil)
	server.Register(srv, st)

	ctx := context.Background()
	ct, srvT := mcp.NewInMemoryTransports()

	_, err = srv.Connect(ctx, srvT, nil)
	require.NoError(t, err)

	cl := mcp.NewClient(&mcp.Implementation{Name: t.Name() + "-cli", Version: "v0-test"}, nil)

	cs, err := cl.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	require.NoError(t, err)

	if out == nil || res.IsError {
		return res
	}

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, out))

	return res
}

func TestPostThenReadOverMCP(t *testing.T) {
	cs := newSession(t)

	var posted server.PostResult
	res := call(t, cs, "channel.post", map[string]any{
		"channel": "pair-debug",
		"from":    "fable",
		"body":    "took the store layer",
	}, &posted)
	require.False(t, res.IsError)
	require.Equal(t, "fable", posted.Posted.From)

	var read server.ReadResult
	res = call(t, cs, "channel.read", map[string]any{"channel": "pair-debug"}, &read)
	require.False(t, res.IsError)
	require.Len(t, read.Messages, 1)
	require.Equal(t, "took the store layer", read.Messages[0].Body)
	require.NotEmpty(t, read.Cursor)

	// Resuming from the returned cursor yields nothing new.
	res = call(t, cs, "channel.read", map[string]any{"channel": "pair-debug", "since": read.Cursor}, &read)
	require.False(t, res.IsError)
	require.Empty(t, read.Messages)
}

func TestListOverMCP(t *testing.T) {
	cs := newSession(t)

	call(t, cs, "channel.post", map[string]any{"channel": "one", "from": "a", "body": "x"}, nil)
	call(t, cs, "channel.post", map[string]any{"channel": "two", "from": "a", "body": "y"}, nil)

	var listed server.ListResult
	res := call(t, cs, "channel.list", map[string]any{}, &listed)
	require.False(t, res.IsError)
	require.Len(t, listed.Channels, 2)
}

// failStore implements server.Store and fails every call with a non-sentinel
// error, to exercise the internal-error mapping in mcpError plus each
// handler's error return.
type failStore struct{}

func (failStore) Post(_, _, _ string) (store.Message, error) {
	return store.Message{}, errors.New("boom")
}

func (failStore) Read(_, _ string, _ int) ([]store.Message, string, error) {
	return nil, "", errors.New("boom")
}

func (failStore) List() ([]store.Info, error) { return nil, errors.New("boom") }

func TestInternalErrorsSurfaceAsToolErrors(t *testing.T) {
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: t.Name(), Version: "v0-test"}, nil)
	server.Register(srv, failStore{})

	ct, srvT := mcp.NewInMemoryTransports()
	_, err := srv.Connect(ctx, srvT, nil)
	require.NoError(t, err)

	cl := mcp.NewClient(&mcp.Implementation{Name: t.Name() + "-cli", Version: "v0-test"}, nil)
	cs, err := cl.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"channel.post", map[string]any{"channel": "dev", "from": "a", "body": "x"}},
		{"channel.read", map[string]any{"channel": "dev"}},
		{"channel.list", map[string]any{}},
	} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		// A returned jsonrpc.Error surfaces either as a protocol-level error
		// or as a tool result with IsError — both are acceptable failures.
		if err != nil {
			continue
		}

		require.True(t, res.IsError, "%s should surface an internal error", tc.tool)
	}
}

func TestInvalidArgsSurfaceAsToolErrors(t *testing.T) {
	cs := newSession(t)

	for name, args := range map[string]map[string]any{
		"missing from": {"channel": "dev", "from": "", "body": "x"},
		"bad name":     {"channel": "../evil", "from": "a", "body": "x"},
		"empty body":   {"channel": "dev", "from": "a", "body": " "},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "channel.post", Arguments: args})
		if err != nil {
			continue // protocol-level rejection is equally acceptable
		}

		require.True(t, res.IsError, "case %q should fail", name)
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "channel.read",
		Arguments: map[string]any{"channel": "ghost"},
	})
	if err == nil {
		require.True(t, res.IsError, "reading a missing channel should fail")
	}
}
