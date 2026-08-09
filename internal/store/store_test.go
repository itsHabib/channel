package store_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/itsHabib/channel/internal/store"

	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()

	st, err := store.New(t.TempDir())
	require.NoError(t, err)

	return st
}

func TestPostReadRoundtrip(t *testing.T) {
	st := newStore(t)

	posted, err := st.Post("dev", "fable", "hello world")
	require.NoError(t, err)
	require.Equal(t, "fable", posted.From)
	require.False(t, posted.TS.IsZero())

	msgs, cursor, err := st.Read("dev", "", 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "hello world", msgs[0].Body)
	require.NotEmpty(t, cursor)
}

func TestReadSinceCursorReturnsOnlyNew(t *testing.T) {
	st := newStore(t)

	_, err := st.Post("dev", "a", "first")
	require.NoError(t, err)

	_, cursor, err := st.Read("dev", "", 0)
	require.NoError(t, err)

	msgs, same, err := st.Read("dev", cursor, 0)
	require.NoError(t, err)
	require.Empty(t, msgs)
	require.Equal(t, cursor, same)

	_, err = st.Post("dev", "b", "second")
	require.NoError(t, err)

	msgs, next, err := st.Read("dev", cursor, 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "second", msgs[0].Body)
	require.NotEqual(t, cursor, next)
}

func TestReadLimit(t *testing.T) {
	st := newStore(t)

	for i := range 5 {
		_, err := st.Post("dev", "a", fmt.Sprintf("msg %d", i))
		require.NoError(t, err)
	}

	msgs, cursor, err := st.Read("dev", "", 2)
	require.NoError(t, err)
	require.Len(t, msgs, 2)

	rest, _, err := st.Read("dev", cursor, 0)
	require.NoError(t, err)
	require.Len(t, rest, 3)
	require.Equal(t, "msg 2", rest[0].Body)
}

func TestReadUnknownChannel(t *testing.T) {
	st := newStore(t)

	_, _, err := st.Read("nope", "", 0)
	require.ErrorIs(t, err, store.ErrChannelNotFound)
}

func TestBadCursor(t *testing.T) {
	st := newStore(t)

	_, err := st.Post("dev", "a", "x")
	require.NoError(t, err)

	for _, cursor := range []string{"not-a-number", "-4", "999999"} {
		_, _, err := st.Read("dev", cursor, 0)
		require.ErrorIs(t, err, store.ErrBadCursor, "cursor %q", cursor)
	}
}

func TestNameValidation(t *testing.T) {
	st := newStore(t)

	for _, name := range []string{"", "../evil", "a/b", ".hidden", "way" + string(make([]byte, 100))} {
		_, err := st.Post(name, "a", "x")
		require.ErrorIs(t, err, store.ErrBadName, "name %q", name)
	}

	_, err := st.Post("ok-name_1.log", "a", "x")
	require.NoError(t, err)
}

func TestEmptyFromAndBody(t *testing.T) {
	st := newStore(t)

	_, err := st.Post("dev", "  ", "x")
	require.ErrorIs(t, err, store.ErrEmptyFrom)

	_, err = st.Post("dev", "a", "   ")
	require.ErrorIs(t, err, store.ErrEmptyBody)
}

// TestTornFinalLine simulates a writer caught mid-append: the partial line is
// excluded, the cursor stops before it, and once the line completes a resumed
// read picks it up.
func TestTornFinalLine(t *testing.T) {
	dir := t.TempDir()

	st, err := store.New(dir)
	require.NoError(t, err)

	_, err = st.Post("dev", "a", "whole")
	require.NoError(t, err)

	path := filepath.Join(dir, "dev.jsonl")
	half1 := `{"ts":"2026-01-01T00:00:00Z","from":"b","bo`
	half2 := `dy":"late"}` + "\n"

	appendRaw(t, path, half1)

	msgs, cursor, err := st.Read("dev", "", 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	appendRaw(t, path, half2)

	msgs, _, err = st.Read("dev", cursor, 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "late", msgs[0].Body)
}

func appendRaw(t *testing.T, path, chunk string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)

	_, err = f.WriteString(chunk)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestDirReportsResolvedPath(t *testing.T) {
	dir := t.TempDir()

	st, err := store.New(dir)
	require.NoError(t, err)
	require.Equal(t, dir, st.Dir())
}

func TestNewFailsWhenDirPathIsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o644))

	// MkdirAll under a regular file must fail.
	_, err := store.New(filepath.Join(f, "sub"))
	require.Error(t, err)
}

func TestList(t *testing.T) {
	st := newStore(t)

	_, err := st.Post("alpha", "a", "x")
	require.NoError(t, err)
	_, err = st.Post("beta", "a", "y")
	require.NoError(t, err)

	infos, err := st.List()
	require.NoError(t, err)
	require.Len(t, infos, 2)

	names := []string{infos[0].Name, infos[1].Name}
	require.ElementsMatch(t, []string{"alpha", "beta"}, names)
	require.Positive(t, infos[0].SizeBytes)
	require.False(t, infos[0].LastActivity.Before(infos[1].LastActivity))
}

func TestListSkipsNonChannelEntries(t *testing.T) {
	dir := t.TempDir()

	st, err := store.New(dir)
	require.NoError(t, err)

	_, err = st.Post("real", "a", "x")
	require.NoError(t, err)

	// A non-.jsonl file and a directory that happens to end in .jsonl must
	// both be ignored.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "weird.jsonl"), 0o755))

	infos, err := st.List()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Equal(t, "real", infos[0].Name)
}

func TestReadRejectsCorruptLine(t *testing.T) {
	dir := t.TempDir()

	st, err := store.New(dir)
	require.NoError(t, err)

	_, err = st.Post("dev", "a", "ok")
	require.NoError(t, err)

	appendRaw(t, filepath.Join(dir, "dev.jsonl"), "this is not json\n")

	_, _, err = st.Read("dev", "", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "corrupt line")
}

// TestConcurrentAppends is the core atomicity claim: many writers, one file,
// zero torn or interleaved lines. Run under -race (make test always does).
func TestConcurrentAppends(t *testing.T) {
	const writers, perWriter = 20, 50

	st := newStore(t)

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range perWriter {
				_, err := st.Post("busy", fmt.Sprintf("w%d", w), fmt.Sprintf("writer %d message %d", w, i))
				if err != nil {
					t.Error(err)
				}
			}
		}()
	}

	wg.Wait()

	msgs, _, err := st.Read("busy", "", 0)
	require.NoError(t, err)
	require.Len(t, msgs, writers*perWriter)

	for _, m := range msgs {
		require.NotEmpty(t, m.From)
		require.NotEmpty(t, m.Body)
	}
}
