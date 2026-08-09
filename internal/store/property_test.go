package store_test

import (
	"os"
	"strings"
	"testing"

	"github.com/itsHabib/channel/internal/store"

	"pgregory.net/rapid"
)

// The store has exactly two invariants worth fuzzing over a large input space;
// the rest of channel (CLI, MCP, name validation) is too thin to earn a
// property. These match gate's precedent of property-testing the load-bearing
// logic, not the plumbing.
//
//  1. Round-trip fidelity — the JSONL "one message = one line" invariant must
//     survive bodies full of newlines, quotes, backslashes, and control chars.
//     It holds because Post JSON-encodes each message, but that's exactly the
//     kind of claim to prove rather than assume.
//  2. Cursor split-consistency — reading in arbitrarily-sized chunks via the
//     opaque cursor must equal one full read: no loss, no duplication, no
//     reorder. This fuzzes the byte-offset arithmetic, the subtlest code here.

// tricky is an adversarial rune alphabet aimed squarely at the JSONL delimiter:
// every char here either threatens line-splitting or exercises multibyte
// encoding. All valid UTF-8 — invalid UTF-8 is a documented boundary
// (encoding/json substitutes U+FFFD), not part of the round-trip law.
var tricky = []rune{
	'a', 'Z', '9', ' ', '\t', '\n', '\r', '"', '\\', '/', '{', '}', ':',
	'\x00', '\x1b', 'é', '世', '🚀',
}

func nonBlank(s string) bool { return strings.TrimSpace(s) != "" }

// fieldGen draws a non-blank string over the tricky alphabet, bounded to
// maxRunes so `from` stays within the store's length limit.
func fieldGen(maxRunes int) *rapid.Generator[string] {
	return rapid.StringOfN(rapid.SampledFrom(tricky), 1, maxRunes, -1).Filter(nonBlank)
}

var (
	fromGen = fieldGen(20)
	bodyGen = fieldGen(60)
)

func propStore(t *rapid.T) *store.Store {
	dir, err := os.MkdirTemp("", "channel-prop")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	st, err := store.New(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	return st
}

func TestPropRoundTripPreservesOrderAndContent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		st := propStore(t)

		n := rapid.IntRange(1, 20).Draw(t, "n")

		type post struct{ from, body string }
		posts := make([]post, n)

		for i := range n {
			posts[i] = post{from: fromGen.Draw(t, "from"), body: bodyGen.Draw(t, "body")}

			if _, err := st.Post("prop", posts[i].from, posts[i].body); err != nil {
				t.Fatalf("post %d: %v", i, err)
			}
		}

		got, _, err := st.Read("prop", "", 0)
		if err != nil {
			t.Fatalf("read: %v", err)
		}

		if len(got) != n {
			t.Fatalf("read %d messages, posted %d", len(got), n)
		}

		for i, p := range posts {
			// Post normalizes `from` (trims surrounding space) but stores
			// `body` verbatim — the property encodes that exact contract.
			wantFrom := strings.TrimSpace(p.from)
			if got[i].From != wantFrom || got[i].Body != p.body {
				t.Fatalf("message %d: got (%q, %q), want (%q, %q)", i, got[i].From, got[i].Body, wantFrom, p.body)
			}
		}
	})
}

func TestPropCursorSplitIsLossless(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		st := propStore(t)

		n := rapid.IntRange(1, 25).Draw(t, "n")

		want := make([]string, n)
		for i := range n {
			want[i] = bodyGen.Draw(t, "body")

			if _, err := st.Post("c", "sender", want[i]); err != nil {
				t.Fatalf("post %d: %v", i, err)
			}
		}

		// Drain the channel in arbitrarily-sized chunks; the concatenation must
		// reproduce the full read exactly. limit 0 means "all remaining".
		var got []string

		cursor := ""
		for len(got) < n {
			limit := rapid.IntRange(0, 5).Draw(t, "limit")

			msgs, next, err := st.Read("c", cursor, limit)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			for _, m := range msgs {
				got = append(got, m.Body)
			}

			if next == cursor {
				break // no progress — drained
			}

			cursor = next
		}

		if len(got) != n {
			t.Fatalf("chunked read got %d, want %d", len(got), n)
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("message %d: got %q, want %q", i, got[i], want[i])
			}
		}
	})
}
