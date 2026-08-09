// Package server exposes the store over MCP: three stdio tools, no state of
// its own. The tool descriptions are agent-facing product surface — they are
// what makes an agent reach for a channel without being told — so they say
// when to use each verb, not just what it does.
package server

import (
	"context"
	"errors"

	"github.com/itsHabib/channel/internal/store"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Store is the narrow store surface the MCP tools need.
type Store interface {
	Post(name, from, body string) (store.Message, error)
	Read(name, cursor string, limit int) ([]store.Message, string, error)
	List() ([]store.Info, error)
}

// PostArgs are the inputs to channel.post.
type PostArgs struct {
	Channel string `json:"channel" jsonschema:"channel name; created on first post"`
	From    string `json:"from" jsonschema:"your identity as shown to readers, e.g. an agent or session name"`
	Body    string `json:"body" jsonschema:"the message"`
}

// PostResult is the output of channel.post.
type PostResult struct {
	Posted store.Message `json:"posted"`
}

// ReadArgs are the inputs to channel.read.
type ReadArgs struct {
	Channel string `json:"channel" jsonschema:"channel name"`
	Since   string `json:"since,omitempty" jsonschema:"opaque cursor from a prior read; omit to read from the start"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max messages to return; 0 or omitted = all"`
}

// ReadResult is the output of channel.read.
type ReadResult struct {
	Messages []store.Message `json:"messages"`
	Cursor   string          `json:"cursor"`
}

// ListArgs are the (empty) inputs to channel.list.
type ListArgs struct{}

// ListResult is the output of channel.list.
type ListResult struct {
	Channels []store.Info `json:"channels"`
}

// Register wires the three channel tools onto s.
func Register(s *mcp.Server, st Store) {
	registerPost(s, st)
	registerRead(s, st)
	registerList(s, st)
}

func registerPost(s *mcp.Server, st Store) {
	const description = `Post a message to a named channel on this machine's shared agent message bus. ` +
		`Use it to coordinate with other local agents or leave word for the operator: announce a status ` +
		`("PR 42 is up"), ask a peer a question, hand off a finding, or report a result. The channel is ` +
		`created on first post — no setup verb exists. Pick a short kebab-case channel name scoped to the ` +
		`shared effort (e.g. "pair-debug-auth"), and post proactively when another agent may be waiting on you.`

	mcp.AddTool(s, &mcp.Tool{Name: "channel.post", Description: description},
		func(_ context.Context, _ *mcp.CallToolRequest, args PostArgs) (*mcp.CallToolResult, PostResult, error) {
			msg, err := st.Post(args.Channel, args.From, args.Body)
			if err != nil {
				return nil, PostResult{}, mcpError(err)
			}

			return nil, PostResult{Posted: msg}, nil
		})
}

func registerRead(s *mcp.Server, st Store) {
	const description = `Read messages from a named channel on this machine's shared agent message bus. ` +
		`Use it to catch up on what peer agents or the operator have posted — poll it between work steps when ` +
		`you are coordinating with someone. Pass the cursor returned by your previous read as "since" to get ` +
		`only new messages; treat the cursor as an opaque token.`

	mcp.AddTool(s, &mcp.Tool{Name: "channel.read", Description: description},
		func(_ context.Context, _ *mcp.CallToolRequest, args ReadArgs) (*mcp.CallToolResult, ReadResult, error) {
			msgs, cursor, err := st.Read(args.Channel, args.Since, args.Limit)
			if err != nil {
				return nil, ReadResult{}, mcpError(err)
			}

			return nil, ReadResult{Messages: msgs, Cursor: cursor}, nil
		})
}

func registerList(s *mcp.Server, st Store) {
	const description = `List every channel on this machine's shared agent message bus, most recently active ` +
		`first. Use it to discover where coordination is already happening before posting to a new name.`

	mcp.AddTool(s, &mcp.Tool{Name: "channel.list", Description: description},
		func(_ context.Context, _ *mcp.CallToolRequest, _ ListArgs) (*mcp.CallToolResult, ListResult, error) {
			infos, err := st.List()
			if err != nil {
				return nil, ListResult{}, mcpError(err)
			}

			return nil, ListResult{Channels: infos}, nil
		})
}

// mcpError maps store sentinels to invalid-params so agents see actionable
// messages; anything else is an internal error.
func mcpError(err error) error {
	switch {
	case errors.Is(err, store.ErrBadName),
		errors.Is(err, store.ErrChannelNotFound),
		errors.Is(err, store.ErrBadCursor),
		errors.Is(err, store.ErrEmptyFrom),
		errors.Is(err, store.ErrEmptyBody):
		return jsonrpcError(jsonrpc.CodeInvalidParams, err)
	default:
		return jsonrpcError(jsonrpc.CodeInternalError, err)
	}
}

func jsonrpcError(code int64, err error) error {
	return &jsonrpc.Error{Code: code, Message: err.Error()}
}
