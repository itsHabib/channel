// Package store is the mechanism layer: append-only JSONL channels on disk.
// One channel is one file under the store dir; a message is one JSON line.
// Appends are a single write(2) on an O_APPEND descriptor under an exclusive
// flock, so concurrent writers never interleave within a line. Readers take
// no lock at all — they resume from an opaque cursor (a byte offset) and stop
// cleanly at a torn final line.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Message is one line of a channel: who said what, when.
type Message struct {
	TS   time.Time `json:"ts"`
	From string    `json:"from"`
	Body string    `json:"body"`
}

// Info summarizes one channel for listings.
type Info struct {
	Name         string    `json:"name"`
	LastActivity time.Time `json:"lastActivity"`
	SizeBytes    int64     `json:"sizeBytes"`
}

// Store reads and appends channels under a single directory.
type Store struct {
	dir string
}

// Sentinel errors callers branch on. Everything else is an internal failure.
var (
	ErrBadName         = errors.New("invalid channel name: use letters, digits, dot, dash, underscore (max 64)")
	ErrChannelNotFound = errors.New("channel not found")
	ErrBadCursor       = errors.New("invalid cursor")
	ErrEmptyFrom       = errors.New("from is required")
	ErrEmptyBody       = errors.New("body is required")
)

const (
	maxNameLen = 64
	maxFromLen = 128
	ext        = ".jsonl"
)

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// New opens (creating if needed) a store rooted at dir.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}

	return &Store{dir: dir}, nil
}

// Dir reports the resolved store directory.
func (s *Store) Dir() string {
	return s.dir
}

// Post appends one message to the named channel, creating the channel on
// first post. It returns the stored message with its server-side timestamp.
func (s *Store) Post(name, from, body string) (Message, error) {
	if err := validateName(name); err != nil {
		return Message{}, err
	}

	from = strings.TrimSpace(from)
	if from == "" || len(from) > maxFromLen {
		return Message{}, ErrEmptyFrom
	}

	if strings.TrimSpace(body) == "" {
		return Message{}, ErrEmptyBody
	}

	msg := Message{TS: time.Now().UTC(), From: from, Body: body}

	line, err := json.Marshal(msg)
	if err != nil {
		return Message{}, fmt.Errorf("encode message: %w", err)
	}

	line = append(line, '\n')
	if err := s.appendLine(name, line); err != nil {
		return Message{}, err
	}

	return msg, nil
}

// appendLine writes one complete line to the channel file as a single write
// under an exclusive flock. The lock covers oversized messages; for lines
// under the pipe buffer O_APPEND alone is already atomic.
func (s *Store) appendLine(name string, line []byte) error {
	f, err := os.OpenFile(s.path(name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := lock(f); err != nil {
		return fmt.Errorf("lock channel: %w", err)
	}
	defer unlock(f)

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("append message: %w", err)
	}

	return nil
}

// Read returns messages after cursor (empty cursor = from the start) and the
// cursor to resume from. limit <= 0 means no limit. A torn final line — a
// writer mid-append — is excluded, and the returned cursor stops before it.
func (s *Store) Read(name, cursor string, limit int) ([]Message, string, error) {
	if err := validateName(name); err != nil {
		return nil, "", err
	}

	offset, err := parseCursor(cursor)
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrChannelNotFound
	}

	if err != nil {
		return nil, "", fmt.Errorf("read channel: %w", err)
	}

	if offset > int64(len(data)) {
		return nil, "", ErrBadCursor
	}

	msgs, next, err := decodeLines(data, offset, limit)
	if err != nil {
		return nil, "", err
	}

	return msgs, strconv.FormatInt(next, 10), nil
}

// decodeLines walks complete lines in data starting at offset, returning at
// most limit messages (limit <= 0 = all) and the offset after the last
// consumed line.
func decodeLines(data []byte, offset int64, limit int) ([]Message, int64, error) {
	msgs := []Message{}
	next := offset

	for next < int64(len(data)) {
		if limit > 0 && len(msgs) == limit {
			break
		}

		rest := data[next:]

		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			break // torn final line; a writer is mid-append
		}

		var msg Message
		if err := json.Unmarshal(rest[:nl], &msg); err != nil {
			return nil, 0, fmt.Errorf("corrupt line at offset %d: %w", next, err)
		}

		msgs = append(msgs, msg)
		next += int64(nl) + 1
	}

	return msgs, next, nil
}

// List returns every channel in the store, most recently active first.
func (s *Store) List() ([]Info, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list store dir: %w", err)
	}

	infos := []Info{}

	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ext)
		if !ok || e.IsDir() {
			continue
		}

		fi, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("stat channel %s: %w", name, err)
		}

		infos = append(infos, Info{Name: name, LastActivity: fi.ModTime().UTC(), SizeBytes: fi.Size()})
	}

	sort.Slice(infos, func(i, j int) bool { return infos[i].LastActivity.After(infos[j].LastActivity) })

	return infos, nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name+ext)
}

func validateName(name string) error {
	if name == "" || len(name) > maxNameLen || !nameRe.MatchString(name) {
		return ErrBadName
	}

	return nil
}

// parseCursor decodes the opaque cursor. Today it is a base-10 byte offset;
// callers must treat it as a token, never compute one.
func parseCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}

	offset, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || offset < 0 {
		return 0, ErrBadCursor
	}

	return offset, nil
}
