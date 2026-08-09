# Mutation audit — internal/store

A one-time, by-hand mutation audit of the store's correctness-critical lines.
**Not wired into CI** — for a tool this small the property + unit + e2e tests
are the standing guard, and this is a periodic sanity check that those tests
actually *kill* bugs rather than just execute code. Re-run by hand if the store
changes shape.

- **Last run:** 2026-08-09, store at base `5be8f5f`, on darwin/arm64 (APFS).
- **Method:** apply one source mutation, run the tests that should catch it,
  confirm they go red, revert. A surviving mutant is a gap (or defense-in-depth
  the tests can't reach).

| # | Mutation | Tests exercised | Result |
|---|----------|-----------------|--------|
| M1 | cursor advance `next += nl + 1` → `next += nl` (off-by-one) | `TestReadSinceCursor…`, `TestPropCursorSplitIsLossless` | killed |
| M2 | drop the appended `'\n'` delimiter in `Post` | `TestPostReadRoundtrip`, `TestPropRoundTrip…` | killed |
| M4 | drop the `limit > 0` short-circuit in `decodeLines` | `TestReadLimit`, `TestPostReadRoundtrip` | killed |
| M3 | `flock` → `return nil` (no lock) | `TestConcurrentAppends` (in-proc), `TestE2ECrossProcessAtomicity` (cross-proc, 40 KB bodies) | **survived** |

## The surviving mutant: flock (M3)

Removing the `flock` is caught by no current test on darwin/APFS. Why: Go issues
each append as a single `write(2)`, and both Linux and APFS make a single
`O_APPEND` write atomic with respect to the file offset — so for the message
sizes these tests produce (even the 40 KB cross-process bodies) appends never
interleave, lock or no lock.

The `flock` is therefore **defense-in-depth, not dead weight**: it covers what
single-syscall atomicity doesn't — writes large enough that the kernel
short-writes (Go then loops, and the follow-up `write` is a separate,
unprotected syscall), and filesystems without `O_APPEND` atomicity guarantees
(some network mounts). It stays deliberately. Forcing a portable short-write to
kill M3 would cost more test complexity than it's worth for this tool, so the
audit simply records the gap.

## Deliberately not covered

Automated mutation (e.g. [gremlins](https://github.com/go-gremlins/gremlins))
isn't set up: it doesn't build cleanly on Go 1.26 yet, and a standing mutation
job is disproportionate for ~500 lines. This hand audit is the proportional
substitute — the property tests (`internal/store/property_test.go`) are where
the broad-input-space confidence actually comes from.
