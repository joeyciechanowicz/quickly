# Interactive PTY Dashboard — Design Spec

**Date:** 2026-04-23  
**Status:** Approved

---

## Problem

`quickly` runs commands concurrently across repos by piping `stdout`/`stderr` through a `PrefixedWriter`. Because subprocesses are not started inside a terminal (TTY), tools like `yarn install`, `npm install`, and `cargo build` detect the absence of a TTY and suppress progress bars, spinners, and interactive output entirely. Users see no progress until the command completes.

---

## Goal

Add an opt-in `--interactive` / `-i` flag that:

1. Runs each subprocess inside a pseudo-terminal (PTY) so tools behave as if in an interactive terminal.
2. Multiplexes all active subprocesses onto a single dashboard view with in-place updates.
3. Queues tasks beyond `runtime.NumCPU()` concurrent workers.

---

## Activation

```
quickly --interactive yarn install
quickly -i yarn install
```

If `--interactive` / `-i` is passed but stdout is not a TTY (e.g. piped to a file), fall back to the existing streaming mode with a warning to stderr.

The existing streaming path is **unchanged** — this flag is purely additive.

---

## Architecture

### New files

| File | Responsibility |
|---|---|
| `pty-runner.go` | Allocates PTY per task, starts process, reads output, feeds `PaneBuffer` |
| `dashboard.go` | Renders panes in-place at ~15 fps using ANSI cursor movement |
| `ansi-normaliser.go` | Strips cursor-movement escapes; turns `` rewrites into clean lines |

### Modified files

| File | Change |
|---|---|
| `quickly.go` | Parse `--interactive` / `-i`; route to `interactiveRun()` |

### Data flow

```
main()
  └─ [--interactive] → interactiveRun(tasks, workers=NumCPU)
        ├─ worker goroutines (NumCPU) pull from task channel
        │    └─ ptyRunner(task) → allocates PTY, starts process
        │         └─ reads PTY master fd → ANSI normaliser → PaneBuffer
        └─ dashboard goroutine → ticks ~15fps → redraws all panes to stdout
```

The dashboard goroutine owns stdout exclusively once started. No other goroutine writes to stdout in interactive mode.

---

## Component Design

### PTY Runner (`pty-runner.go`)

Each worker calls `ptyRunner(task Task) CommandOutput`:

1. Creates `exec.Command("bash", "-c", task.ShellCmd)` with the same environment variables as the existing path (`TERM=xterm-256color`, `FORCE_COLOR=true`, `COLORTERM=truecolor`, `CLICOLOR=1`, `CLICOLOR_FORCE=1`).
2. Starts the command inside a PTY via `pty.Start(cmd)` (returns the master fd).
3. Sets the PTY window size to 220×50 columns/rows.
4. Sets `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` so the child gets its own process group (needed for clean SIGINT forwarding).
5. Reads the master fd in a loop, passing chunks through the ANSI normaliser, appending clean lines to the task's `PaneBuffer`.
6. On process exit (master fd returns `io.EOF` or `EIO`), marks the pane as `Done` or `Failed` and records duration.

### ANSI Normaliser (`ansi-normaliser.go`)

A state-machine (no regex) that processes raw PTY output into clean `[]string` lines.

| Input | Treatment |
|---|---|
| `` (bare carriage return) | Replace current in-progress line (progress bar rewrite) |
| `
` | Emit current line, start new |
| `\033[<n>A`, `\033[<n>K`, `\033[2J` | Discard (cursor-up, erase-line, clear-screen) |
| `\033[<n>m` | **Keep** — colour/style codes passed through |
| All other ANSI escape sequences | Discard |

Output: complete lines appended to the pane's ring buffer.

### PaneBuffer

```go
type PaneState int

const (
    Active PaneState = iota
    Done
    Failed
)

type PaneBuffer struct {
    mu       sync.Mutex
    dir      string        // display name (filepath.Base of directory)
    color    string        // ANSI colour code
    lines    []string      // ring buffer, capacity = maxLines
    state    PaneState
    duration time.Duration // set on completion
}
```

`maxLines` is a package-level constant (e.g. `20`). When the ring buffer is full, the oldest line is evicted.

### Dashboard Renderer (`dashboard.go`)

**Layout algorithm** (pure function `computeLayout`):

1. Count `Failed` panes — each is allocated its full line history (up to `maxLines`) plus 1 header line.
2. Count `Done` panes — each occupies 1 summary line.
3. `R = termHeight - sum(failed allocations) - done_count - active_count` (1 header per active pane).
4. Each active pane gets `max(3, R / active_count)` lines of content.

**Render loop** (~15 fps ticker):

1. Move cursor to top of dashboard region.
2. For each pane in order — Failed → Active → Done:
   - **Failed:** header `✗ [dirname] failed in Xs` + last N lines (coloured), padded to allocated height.
   - **Active:** header `[dirname] ── running… Xs` + last N lines (coloured), padded to allocated height.
   - **Done:** single line `✓ [dirname] done in Xs`.
3. Print a blank sentinel line after all panes.
4. On all tasks complete: stop ticker, print final summary of any failures.

**Terminal hygiene:**
- Hide cursor on start (`\033[?25l`), restore on exit (`\033[?25h`).
- Restore terminal in a `defer` — runs on normal exit, panic, and signal.

**SIGWINCH handling:** Re-query terminal size on each SIGWINCH signal; layout reflows on the next tick.

**SIGINT handling:** Forward SIGINT to all child process groups (`syscall.Kill(-pgid, syscall.SIGINT)`). After all children exit, restore terminal and exit with code 1.

---

## Worker Count

`interactiveRun` uses exactly `runtime.NumCPU()` workers (no multiplier). Tasks beyond this queue in the buffered task channel. Queued tasks have no pane rendered until a worker picks them up.

---

## Non-TTY Fallback

If `isatty(os.Stdout.Fd())` returns false when `--interactive` is set, print a warning to stderr and fall back to the existing `executeCommand` streaming path unchanged.

---

## Testing Strategy

### Unit tests

**`ansi-normaliser.go`** (pure function, no I/O):
- `` mid-line replaces the current line.
- Cursor-up / erase-line sequences are stripped.
- Colour codes pass through intact.
- Mixed real-world output (e.g. a yarn-style progress line).

**`PaneBuffer`**:
- Ring buffer caps at `maxLines`; oldest lines evicted.
- State transitions: `Active → Done`, `Active → Failed`.
- Concurrent writes are race-free (run with `-race`).

**`computeLayout`** (pure function):
- Equal split across active panes.
- Failed panes get full allocation.
- Done panes take exactly 1 line.
- Minimum 3 lines per active pane is respected.
- Edge case: more active panes than available lines → all get minimum.

### Integration test

A single integration test in `quickly_test.go` runs a short command (`echo hello && sleep 0.1 && echo world`) through `ptyRunner` and asserts:
- Both lines appear in the `PaneBuffer`.
- The pane transitions to `Done`.
- No data races under `-race`.

### Unchanged

All existing tests pass without modification. The `--interactive` path is strictly additive.

---

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/creack/pty` | latest stable | PTY allocation and window sizing |
| `golang.org/x/term` | (already in stdlib ecosystem) | `IsTerminal`, `GetSize`, `SIGWINCH` |

`golang.org/x/term` may already be an indirect dependency; confirm with `go mod tidy` after adding `creack/pty`.

---

## Out of Scope

- Windows support (PTY API is entirely different; not a current target).
- Scrollback / pager for failed pane output beyond `maxLines`.
- Configuring `maxLines` or `NumCPU` cap via flags (can be added later).
