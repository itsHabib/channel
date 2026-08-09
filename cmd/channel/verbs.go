package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/itsHabib/channel/internal/store"
)

// followInterval paces the read --follow poll loop. Half a second keeps a
// tail feeling live without hammering the filesystem.
const followInterval = 500 * time.Millisecond

func cmdPost(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("post", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	as := fs.String("as", os.Getenv("CHANNEL_AS"), "sender identity (defaults to $CHANNEL_AS)")

	if err := fs.Parse(args); err != nil || fs.NArg() < 2 {
		return errUsage
	}

	body, err := postBody(fs.Args()[1:], os.Stdin)
	if err != nil {
		return err
	}

	st, err := openStore()
	if err != nil {
		return err
	}

	msg, err := st.Post(fs.Arg(0), *as, body)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "posted to %s as %s at %s\n", fs.Arg(0), msg.From, msg.TS.Format(time.RFC3339))

	return nil
}

// postBody joins the positional body words; a lone "-" reads from stdin
// instead, for multi-line payloads like reports.
func postBody(words []string, stdin io.Reader) (string, error) {
	if len(words) == 1 && words[0] == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read body from stdin: %w", err)
		}

		return string(data), nil
	}

	return strings.Join(words, " "), nil
}

func cmdRead(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	since := fs.String("since", "", "opaque cursor from a prior read")
	limit := fs.Int("limit", 0, "max messages (0 = all)")
	follow := fs.Bool("follow", false, "keep polling for new messages")
	asJSON := fs.Bool("json", false, "emit {messages, cursor} JSON")

	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		return errUsage
	}

	st, err := openStore()
	if err != nil {
		return err
	}

	if *follow {
		return followChannel(st, fs.Arg(0), *since, stdout)
	}

	msgs, cursor, err := st.Read(fs.Arg(0), *since, *limit)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(stdout, map[string]any{"messages": msgs, "cursor": cursor})
	}

	printMessages(stdout, msgs)

	return nil
}

// followChannel tails a channel forever, polling from the last cursor.
func followChannel(st *store.Store, name, cursor string, stdout io.Writer) error {
	for {
		next, err := followOnce(st, name, cursor, stdout)
		if err != nil {
			return err
		}

		cursor = next

		time.Sleep(followInterval)
	}
}

// followOnce prints any messages after cursor and returns the next cursor. A
// channel that does not exist yet is waited on, not an error — the peer may
// simply not have posted first — so the cursor is returned unchanged.
func followOnce(st *store.Store, name, cursor string, stdout io.Writer) (string, error) {
	msgs, next, err := st.Read(name, cursor, 0)
	if err != nil {
		if errors.Is(err, store.ErrChannelNotFound) {
			return cursor, nil
		}

		return cursor, err
	}

	printMessages(stdout, msgs)

	return next, nil
}

func printMessages(w io.Writer, msgs []store.Message) {
	for _, m := range msgs {
		_, _ = fmt.Fprintf(w, "%s  %-12s %s\n", m.TS.Local().Format("2006-01-02 15:04:05"), m.From, m.Body)
	}
}

func cmdList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "emit channel infos as JSON")

	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errUsage
	}

	st, err := openStore()
	if err != nil {
		return err
	}

	infos, err := st.List()
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(stdout, map[string]any{"channels": infos})
	}

	for _, info := range infos {
		_, _ = fmt.Fprintf(stdout, "%s  %-24s %d bytes\n",
			info.LastActivity.Local().Format("2006-01-02 15:04:05"), info.Name, info.SizeBytes)
	}

	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}

	return nil
}
