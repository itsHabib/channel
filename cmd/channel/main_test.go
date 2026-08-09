package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := run(append([]string{"channel"}, args...), &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI(t, "--version")
	require.Equal(t, 0, code)
	require.Contains(t, out, "channel v")
}

func TestNoArgsShowsUsage(t *testing.T) {
	code, _, errOut := runCLI(t)
	require.Equal(t, 2, code)
	require.Contains(t, errOut, "Usage:")
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := runCLI(t, "frobnicate")
	require.Equal(t, 2, code)
	require.Contains(t, errOut, "unknown command")
}

func TestPostReadListRoundtrip(t *testing.T) {
	t.Setenv("CHANNEL_DIR", t.TempDir())

	code, out, errOut := runCLI(t, "post", "--as", "fable", "dev", "hello", "from", "the", "cli")
	require.Equal(t, 0, code, errOut)
	require.Contains(t, out, "posted to dev as fable")

	code, out, errOut = runCLI(t, "read", "--json", "dev")
	require.Equal(t, 0, code, errOut)

	var res struct {
		Messages []struct {
			From string `json:"from"`
			Body string `json:"body"`
		} `json:"messages"`
		Cursor string `json:"cursor"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	require.Len(t, res.Messages, 1)
	require.Equal(t, "hello from the cli", res.Messages[0].Body)
	require.NotEmpty(t, res.Cursor)

	code, out, errOut = runCLI(t, "read", "--since", res.Cursor, "--json", "dev")
	require.Equal(t, 0, code, errOut)

	var again struct {
		Messages []any `json:"messages"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &again))
	require.Empty(t, again.Messages)

	code, out, errOut = runCLI(t, "list")
	require.Equal(t, 0, code, errOut)
	require.Contains(t, out, "dev")
}

func TestPostSenderFromEnv(t *testing.T) {
	t.Setenv("CHANNEL_DIR", t.TempDir())
	t.Setenv("CHANNEL_AS", "env-agent")

	code, out, errOut := runCLI(t, "post", "dev", "hi")
	require.Equal(t, 0, code, errOut)
	require.Contains(t, out, "as env-agent")
}

func TestPostWithoutSenderFails(t *testing.T) {
	t.Setenv("CHANNEL_DIR", t.TempDir())
	t.Setenv("CHANNEL_AS", "")

	code, _, errOut := runCLI(t, "post", "dev", "hi")
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "from is required")
}

func TestReadMissingChannel(t *testing.T) {
	t.Setenv("CHANNEL_DIR", t.TempDir())

	code, _, errOut := runCLI(t, "read", "ghost")
	require.Equal(t, 1, code)
	require.Contains(t, errOut, "channel not found")
}

func TestHumanReadOutput(t *testing.T) {
	t.Setenv("CHANNEL_DIR", t.TempDir())

	code, _, _ := runCLI(t, "post", "--as", "a", "dev", "line one")
	require.Equal(t, 0, code)

	code, out, _ := runCLI(t, "read", "dev")
	require.Equal(t, 0, code)
	require.True(t, strings.Contains(out, "a") && strings.Contains(out, "line one"))
}
