package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itsHabib/channel/internal/store"

	"github.com/stretchr/testify/require"
)

func TestPostBodyJoinsWords(t *testing.T) {
	body, err := postBody([]string{"hello", "there", "world"}, strings.NewReader("IGNORED"))
	require.NoError(t, err)
	require.Equal(t, "hello there world", body)
}

func TestPostBodyReadsStdinOnDash(t *testing.T) {
	body, err := postBody([]string{"-"}, strings.NewReader("multi\nline\nreport\n"))
	require.NoError(t, err)
	require.Equal(t, "multi\nline\nreport\n", body)
}

func TestFollowOnceToleratesMissingChannel(t *testing.T) {
	st, err := store.New(t.TempDir())
	require.NoError(t, err)

	var out strings.Builder
	cursor, err := followOnce(st, "ghost", "", &out)
	require.NoError(t, err)
	require.Equal(t, "", cursor)
	require.Empty(t, out.String())
}

func TestFollowOncePrintsAndAdvances(t *testing.T) {
	st, err := store.New(t.TempDir())
	require.NoError(t, err)

	_, err = st.Post("dev", "alice", "first")
	require.NoError(t, err)

	var out strings.Builder
	cursor, err := followOnce(st, "dev", "", &out)
	require.NoError(t, err)
	require.NotEqual(t, "", cursor)
	require.Contains(t, out.String(), "first")

	// A second call from the returned cursor prints nothing new.
	out.Reset()
	next, err := followOnce(st, "dev", cursor, &out)
	require.NoError(t, err)
	require.Equal(t, cursor, next)
	require.Empty(t, out.String())
}

func TestOpenStoreUsesEnvDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANNEL_DIR", dir)

	st, err := openStore()
	require.NoError(t, err)
	require.Equal(t, dir, st.Dir())
}

func TestOpenStoreDefaultsToHomeDotChannel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CHANNEL_DIR", "")
	t.Setenv("HOME", home)

	st, err := openStore()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".channel"), st.Dir())
}

func TestFailMapsErrorsToExitCodes(t *testing.T) {
	require.Equal(t, 0, fail(&strings.Builder{}, nil))
	require.Equal(t, 2, fail(&strings.Builder{}, errUsage))
	require.Equal(t, 1, fail(&strings.Builder{}, errors.New("boom")))
}

func TestCmdListJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANNEL_DIR", dir)

	st, err := store.New(dir)
	require.NoError(t, err)

	_, err = st.Post("alpha", "a", "x")
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, cmdList([]string{"--json"}, &out))
	require.Contains(t, out.String(), `"channels"`)
	require.Contains(t, out.String(), "alpha")
}

// errWriter fails every write, to exercise writeJSON's encode-error path.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteJSONSurfacesEncodeError(t *testing.T) {
	err := writeJSON(errWriter{}, map[string]any{"k": "v"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "encode json")
}

// TestReadFollowStopsOnError confirms cmdRead's --follow path returns the
// underlying error rather than looping — a bad cursor is a clean bail.
func TestReadFollowStopsOnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CHANNEL_DIR", dir)

	_, err := os.Create(filepath.Join(dir, "dev.jsonl"))
	require.NoError(t, err)

	code := fail(&strings.Builder{}, cmdRead([]string{"--follow", "--since", "not-a-cursor", "dev"}, &strings.Builder{}))
	require.Equal(t, 1, code)
}
