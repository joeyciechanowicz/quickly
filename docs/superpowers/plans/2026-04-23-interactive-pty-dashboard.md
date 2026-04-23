# Interactive PTY Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--interactive` / `-i` flag that runs each subprocess in a PTY and multiplexes their output onto an adaptive in-place dashboard.

**Architecture:** Each task gets a `PaneBuffer` (ring buffer of lines). A pool of `runtime.NumCPU()` workers allocates a PTY per task via `creack/pty`, reads raw output through an ANSI normaliser, and appends clean lines to the buffer. A dashboard goroutine redraws all panes at ~15 fps using ANSI cursor movement.

**Tech Stack:** Go 1.22, `github.com/creack/pty`, `golang.org/x/term`

---

## File Map

| Action | Path | Responsibility |
|---|---|---|
| Create | `ansi-normaliser.go` | State-machine that strips cursor-movement escapes and turns `` rewrites into clean lines |
| Create | `ansi-normaliser_test.go` | Unit tests for the normaliser |
| Create | `pane-buffer.go` | `PaneBuffer` ring buffer + `PaneState` type |
| Create | `pane-buffer_test.go` | Unit tests for `PaneBuffer` |
| Create | `dashboard.go` | `computeLayout` + render loop + terminal hygiene |
| Create | `dashboard_test.go` | Unit tests for `computeLayout` |
| Create | `pty-runner.go` | `ptyRunner(task, pane)` — PTY allocation, process start, read loop |
| Create | `interactive.go` | `interactiveRun(dirs, shellCmd, branchFilter)` — entry point, wires everything together |
| Modify | `quickly.go` | Parse `--interactive` / `-i`; route to `interactiveRun` |
| Modify | `quickly_test.go` | Add PTY integration test |
| Modify | `go.mod` / `go.sum` | Add `creack/pty` and `golang.org/x/term` |

---

## Task 1: Add dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add `creack/pty` and `golang.org/x/term`**

```bash
cd /path/to/quickly
go get github.com/creack/pty@latest
go get golang.org/x/term@latest
go mod tidy
```

Expected output: `go.mod` and `go.sum` updated, no errors.

- [ ] **Step 2: Verify the build still compiles**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 3: Verify existing tests still pass**

```bash
go test -race ./...
```

Expected: all existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add creack/pty and golang.org/x/term"
```

---

## Task 2: ANSI Normaliser

**Files:**
- Create: `ansi-normaliser.go`
- Create: `ansi-normaliser_test.go`

The normaliser is a pure function — it takes a `[]byte` chunk and a carry-over in-progress line string, and returns completed lines plus the new in-progress line.

- [ ] **Step 1: Write the failing tests in `ansi-normaliser_test.go`**

```go
package main

import (
	"testing"
)

func TestNormalisePassesThroughPlainText(t *testing.T) {
	lines, carry := normaliseChunk([]byte("hello
world
"), "")
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("got lines=%v carry=%q, want [hello world] carry=''", lines, carry)
	}
	if carry != "" {
		t.Fatalf("carry=%q, want ''", carry)
	}
}

func TestNormaliseCarryRetainsPartialLine(t *testing.T) {
	lines, carry := normaliseChunk([]byte("hel"), "")
	if len(lines) != 0 {
		t.Fatalf("got lines=%v, want []", lines)
	}
	if carry != "hel" {
		t.Fatalf("carry=%q, want 'hel'", carry)
	}
}

func TestNormaliseCRReplacesCurrentLine(t *testing.T) {
	//  mid-line should replace the in-progress content (progress bar rewrite)
	lines, carry := normaliseChunk([]byte("loading...Done
"), "")
	if len(lines) != 1 || lines[0] != "Done" {
		t.Fatalf("got lines=%v carry=%q, want [Done] carry=''", lines, carry)
	}
}

func TestNormaliseCROnlyEmitsOnNewline(t *testing.T) {
	//  without following 
 should stay in carry
	lines, carry := normaliseChunk([]byte("loading...Done"), "")
	if len(lines) != 0 {
		t.Fatalf("got lines=%v, want []", lines)
	}
	if carry != "Done" {
		t.Fatalf("carry=%q, want 'Done'", carry)
	}
}

func TestNormaliseStripsEraseLine(t *testing.T) {
	// ESC[2K (erase line) should be stripped; text before it should survive in carry
	lines, carry := normaliseChunk([]byte("text\033[2K"), "")
	if len(lines) != 0 {
		t.Fatalf("got lines=%v, want []", lines)
	}
	if carry != "text" {
		t.Fatalf("carry=%q, want 'text'", carry)
	}
}

func TestNormaliseStripsCursorUp(t *testing.T) {
	// ESC[1A (cursor up 1) should be discarded entirely
	lines, carry := normaliseChunk([]byte("line\033[1Amore
"), "")
	if len(lines) != 1 || lines[0] != "linemore" {
		t.Fatalf("got lines=%v carry=%q, want [linemore]", lines, carry)
	}
}

func TestNormaliseKeepsColourCodes(t *testing.T) {
	// ESC[32m (green) must be preserved
	lines, carry := normaliseChunk([]byte("\033[32mgreen\033[0m
"), "")
	if len(lines) != 1 || lines[0] != "\033[32mgreen\033[0m" {
		t.Fatalf("got lines=%v carry=%q, want colour codes preserved", lines, carry)
	}
}

func TestNormaliseStripsOtherCursorMovement(t *testing.T) {
	// ESC[H (cursor home), ESC[2J (clear screen) discarded
	lines, carry := normaliseChunk([]byte("\033[H\033[2Jhello
"), "")
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("got lines=%v carry=%q, want [hello]", lines, carry)
	}
}

func TestNormaliseCarryPrependedToNextChunk(t *testing.T) {
	_, carry := normaliseChunk([]byte("hel"), "")
	lines, carry2 := normaliseChunk([]byte("lo
"), carry)
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("got lines=%v carry=%q, want [hello]", lines, carry2)
	}
}

func TestNormaliseYarnStyleProgressLine(t *testing.T) {
	// Yarn emits: "progress 10%progress 20%progress 100%
"
	input := []byte("progress 10%progress 20%progress 100%
")
	lines, carry := normaliseChunk(input, "")
	if len(lines) != 1 || lines[0] != "progress 100%" {
		t.Fatalf("got lines=%v carry=%q, want [progress 100%%]", lines, carry)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestNormalise ./...
```

Expected: compile error — `normaliseChunk` undefined.

- [ ] **Step 3: Implement `ansi-normaliser.go`**

```go
package main

// normaliseChunk processes a raw PTY output chunk into complete lines.
// carry is any incomplete line from a previous chunk; it is prepended to
// the first token in p. Returns completed lines and the new carry.
//
// Rules:
//   - 
  → emit current line, reset carry
//   -   → reset current line (progress-bar rewrite), keep building
//   - ESC [ <params> m  → colour/style code: KEEP (pass through)
//   - ESC [ <params> A/K  → cursor-up / erase-line: DISCARD
//   - ESC [ <params> J/H → clear / cursor-home: DISCARD
//   - All other ESC sequences → DISCARD
func normaliseChunk(p []byte, carry string) (lines []string, newCarry string) {
	cur := carry
	i := 0
	for i < len(p) {
		b := p[i]
		switch {
		case b == '
':
			lines = append(lines, cur)
			cur = ""
			i++
		case b == '':
			cur = ""
			i++
		case b == '\033' && i+1 < len(p) && p[i+1] == '[':
			// CSI sequence: ESC [ <params> <final>
			seq, n := parseCSI(p, i)
			final := seq[len(seq)-1]
			switch final {
			case 'm':
				// colour/style — keep
				cur += string(seq)
			default:
				// cursor movement, erase, etc — discard
				_ = final
			}
			i += n
		case b == '\033':
			// Non-CSI escape — skip ESC + next byte
			i += 2
			if i > len(p) {
				i = len(p)
			}
		default:
			cur += string(b)
			i++
		}
	}
	return lines, cur
}

// parseCSI parses a CSI (ESC [) sequence starting at p[start].
// Returns the full sequence bytes and how many bytes were consumed.
// If the sequence is incomplete (runs off the end of p), returns
// just ESC as a 1-byte sequence so the caller can skip it.
func parseCSI(p []byte, start int) (seq []byte, n int) {
	// p[start] == ESC, p[start+1] == '['
	if start+1 >= len(p) {
		return p[start : start+1], 1
	}
	j := start + 2 // skip ESC [
	for j < len(p) {
		c := p[j]
		j++
		// Final byte: 0x40–0x7E
		if c >= 0x40 && c <= 0x7E {
			return p[start:j], j - start
		}
	}
	// Incomplete sequence — skip ESC
	return p[start : start+1], 1
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race -run TestNormalise ./...
```

Expected: all `TestNormalise*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add ansi-normaliser.go ansi-normaliser_test.go
git commit -m "feat: add ANSI normaliser for PTY output"
```

---

## Task 3: PaneBuffer

**Files:**
- Create: `pane-buffer.go`
- Create: `pane-buffer_test.go`

- [ ] **Step 1: Write the failing tests in `pane-buffer_test.go`**

```go
package main

import (
	"sync"
	"testing"
	"time"
)

func TestPaneBufferAppendsLines(t *testing.T) {
	pb := newPaneBuffer("myrepo", "\033[32m", 5)
	pb.appendLine("hello")
	pb.appendLine("world")
	lines := pb.snapshot()
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("snapshot()=%v, want [hello world]", lines)
	}
}

func TestPaneBufferRingEvictsOldest(t *testing.T) {
	pb := newPaneBuffer("myrepo", "\033[32m", 3)
	pb.appendLine("a")
	pb.appendLine("b")
	pb.appendLine("c")
	pb.appendLine("d") // evicts "a"
	lines := pb.snapshot()
	if len(lines) != 3 || lines[0] != "b" || lines[1] != "c" || lines[2] != "d" {
		t.Fatalf("snapshot()=%v, want [b c d]", lines)
	}
}

func TestPaneBufferStateTransitionsToDone(t *testing.T) {
	pb := newPaneBuffer("myrepo", "\033[32m", 5)
	if pb.getState() != Active {
		t.Fatal("initial state should be Active")
	}
	pb.complete(12 * time.Second)
	if pb.getState() != Done {
		t.Fatal("state should be Done after complete()")
	}
	if pb.getDuration() != 12*time.Second {
		t.Fatalf("duration=%v, want 12s", pb.getDuration())
	}
}

func TestPaneBufferStateTransitionsToFailed(t *testing.T) {
	pb := newPaneBuffer("myrepo", "\033[32m", 5)
	pb.fail(3 * time.Second)
	if pb.getState() != Failed {
		t.Fatal("state should be Failed after fail()")
	}
}

func TestPaneBufferConcurrentWritesSafe(t *testing.T) {
	pb := newPaneBuffer("myrepo", "\033[32m", 100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pb.appendLine("line")
		}()
	}
	wg.Wait()
	lines := pb.snapshot()
	if len(lines) != 50 {
		t.Fatalf("len(snapshot())=%d, want 50", len(lines))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestPaneBuffer ./...
```

Expected: compile error — `newPaneBuffer`, `PaneBuffer`, `Active`, `Done`, `Failed` undefined.

- [ ] **Step 3: Implement `pane-buffer.go`**

```go
package main

import (
	"sync"
	"time"
)

// PaneState represents the lifecycle state of a running task's pane.
type PaneState int

const (
	Active PaneState = iota
	Done
	Failed
)

// maxPaneLines is the maximum number of lines retained per pane.
const maxPaneLines = 20

// PaneBuffer holds the display state for a single running task.
// It is safe for concurrent use.
type PaneBuffer struct {
	mu       sync.Mutex
	dir      string    // display name (e.g. filepath.Base of directory)
	color    string    // ANSI colour escape for this pane
	lines    []string  // ring buffer of recent output lines
	head     int       // index of the oldest line in the ring
	count    int       // number of lines currently stored
	cap      int       // ring buffer capacity (== maxPaneLines)
	state    PaneState
	duration time.Duration
	startedAt time.Time
}

func newPaneBuffer(dir, color string, capacity int) *PaneBuffer {
	return &PaneBuffer{
		dir:       dir,
		color:     color,
		lines:     make([]string, capacity),
		cap:       capacity,
		startedAt: time.Now(),
	}
}

// appendLine adds a line to the ring buffer, evicting the oldest if full.
func (pb *PaneBuffer) appendLine(line string) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if pb.count < pb.cap {
		pb.lines[(pb.head+pb.count)%pb.cap] = line
		pb.count++
	} else {
		// overwrite oldest
		pb.lines[pb.head] = line
		pb.head = (pb.head + 1) % pb.cap
	}
}

// snapshot returns a copy of lines in order from oldest to newest.
func (pb *PaneBuffer) snapshot() []string {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	out := make([]string, pb.count)
	for i := 0; i < pb.count; i++ {
		out[i] = pb.lines[(pb.head+i)%pb.cap]
	}
	return out
}

// complete transitions the pane to Done state.
func (pb *PaneBuffer) complete(d time.Duration) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.state = Done
	pb.duration = d
}

// fail transitions the pane to Failed state.
func (pb *PaneBuffer) fail(d time.Duration) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.state = Failed
	pb.duration = d
}

// getState returns the current pane state.
func (pb *PaneBuffer) getState() PaneState {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.state
}

// getDuration returns the recorded task duration.
func (pb *PaneBuffer) getDuration() time.Duration {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.duration
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race -run TestPaneBuffer ./...
```

Expected: all `TestPaneBuffer*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pane-buffer.go pane-buffer_test.go
git commit -m "feat: add PaneBuffer ring buffer with state transitions"
```

---

## Task 4: Dashboard layout algorithm

**Files:**
- Create: `dashboard.go` (layout function only for now — renderer added in Task 6)
- Create: `dashboard_test.go`

- [ ] **Step 1: Write the failing tests in `dashboard_test.go`**

```go
package main

import (
	"testing"
	"time"
)

// helper: build a slice of PaneBuffers with the given states
func makePanes(states ...PaneState) []*PaneBuffer {
	panes := make([]*PaneBuffer, len(states))
	for i, s := range states {
		panes[i] = newPaneBuffer("repo", "\033[32m", maxPaneLines)
		switch s {
		case Done:
			panes[i].complete(time.Second)
		case Failed:
			panes[i].fail(time.Second)
			// fill with lines so failed pane has content height
			for j := 0; j < 5; j++ {
				panes[i].appendLine("error line")
			}
		}
	}
	return panes
}

// Note: paneLayout is defined in dashboard.go — do not redefine it here.

func TestComputeLayoutEqualSplitActive(t *testing.T) {
	panes := makePanes(Active, Active, Active)
	layouts := computeLayout(panes, 30)
	for i, l := range layouts {
		if l.contentLines < 3 {
			t.Fatalf("pane %d contentLines=%d, want >= 3", i, l.contentLines)
		}
	}
	// each should be equal
	if layouts[0].contentLines != layouts[1].contentLines || layouts[1].contentLines != layouts[2].contentLines {
		t.Fatalf("unequal splits: %v", layouts)
	}
}

func TestComputeLayoutDonePanesGetOneLine(t *testing.T) {
	panes := makePanes(Done, Done, Active)
	layouts := computeLayout(panes, 30)
	if layouts[0].contentLines != 0 {
		t.Fatalf("Done pane[0] contentLines=%d, want 0", layouts[0].contentLines)
	}
	if layouts[1].contentLines != 0 {
		t.Fatalf("Done pane[1] contentLines=%d, want 0", layouts[1].contentLines)
	}
	if layouts[2].contentLines < 3 {
		t.Fatalf("Active pane[2] contentLines=%d, want >= 3", layouts[2].contentLines)
	}
}

func TestComputeLayoutFailedPanesGetFullHistory(t *testing.T) {
	panes := makePanes(Failed, Active)
	layouts := computeLayout(panes, 30)
	// Failed pane should get its actual line count (5 lines added in makePanes)
	if layouts[0].contentLines != 5 {
		t.Fatalf("Failed pane contentLines=%d, want 5", layouts[0].contentLines)
	}
}

func TestComputeLayoutMinimumThreeLinesPerActive(t *testing.T) {
	// 10 active panes, terminal height 15 — not enough for equal split
	panes := makePanes(Active, Active, Active, Active, Active, Active, Active, Active, Active, Active)
	layouts := computeLayout(panes, 15)
	for i, l := range layouts {
		if l.contentLines < 3 {
			t.Fatalf("pane %d contentLines=%d, want >= 3 (minimum)", i, l.contentLines)
		}
	}
}

func TestComputeLayoutAllDone(t *testing.T) {
	panes := makePanes(Done, Done, Done)
	layouts := computeLayout(panes, 30)
	for i, l := range layouts {
		if l.contentLines != 0 {
			t.Fatalf("Done pane %d contentLines=%d, want 0", i, l.contentLines)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestComputeLayout ./...
```

Expected: compile error — `computeLayout` undefined (and `paneLayout` undefined until `dashboard.go` is written in Step 3).

- [ ] **Step 3: Implement `computeLayout` in `dashboard.go`**

```go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// paneLayout holds the computed display allocation for one pane.
type paneLayout struct {
	contentLines int // number of content lines allocated (0 for Done panes)
}

// computeLayout calculates how many content lines each pane gets given
// the available terminal height. Called on every render tick.
//
// Ordering contract: Failed panes keep their full line history.
// Done panes get 0 content lines (rendered as a single summary line).
// Active panes share remaining height equally, minimum 3 lines each.
func computeLayout(panes []*PaneBuffer, termHeight int) []paneLayout {
	layouts := make([]paneLayout, len(panes))

	// Tally allocations for non-active panes first.
	failedLines := 0
	doneCount := 0
	activeCount := 0

	for i, pb := range panes {
		switch pb.getState() {
		case Failed:
			n := len(pb.snapshot())
			layouts[i].contentLines = n
			failedLines += n + 1 // +1 for header line
		case Done:
			layouts[i].contentLines = 0
			doneCount++
		case Active:
			activeCount++
		}
	}

	if activeCount == 0 {
		return layouts
	}

	// Remaining height after failed and done panes.
	// Each active pane needs 1 header line + contentLines.
	remaining := termHeight - failedLines - doneCount - activeCount
	if remaining < 0 {
		remaining = 0
	}

	perActive := remaining / activeCount
	if perActive < 3 {
		perActive = 3
	}

	for i, pb := range panes {
		if pb.getState() == Active {
			layouts[i].contentLines = perActive
		}
	}

	return layouts
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -race -run TestComputeLayout ./...
```

Expected: all `TestComputeLayout*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add dashboard.go dashboard_test.go
git commit -m "feat: add dashboard layout algorithm"
```

---

## Task 5: PTY Runner

**Files:**
- Create: `pty-runner.go`

- [ ] **Step 1: Implement `pty-runner.go`**

There is no unit test for this file — it requires a real PTY and is covered by the integration test in Task 7. Write the implementation directly.

```go
package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// ptyRunner runs a task inside a pseudo-terminal, feeding normalised output
// lines into pane. It blocks until the subprocess exits.
func ptyRunner(task Task, pane *PaneBuffer) CommandOutput {
	cmd := exec.Command("bash", "-c", task.ShellCmd)
	cmd.Dir = task.Directory
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Mirror the env vars used by executeCommand.
	env := os.Environ()
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"FORCE_COLOR=true",
		"CLICOLOR=1",
		"CLICOLOR_FORCE=1",
	)
	cmd.Env = env

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return CommandOutput{Error: err}
	}
	defer ptmx.Close()

	// Set a generous window size so tools don't wrap aggressively.
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 50, Cols: 220})

	start := time.Now()
	carry := ""
	buf := make([]byte, 4096)

	for {
		n, readErr := ptmx.Read(buf)
		if n > 0 {
			lines, newCarry := normaliseChunk(buf[:n], carry)
			carry = newCarry
			for _, line := range lines {
				pane.appendLine(line)
			}
		}
		if readErr != nil {
			// io.EOF and EIO (Linux) both mean the child has exited.
			if errors.Is(readErr, io.EOF) || isEIO(readErr) {
				break
			}
			// Unexpected read error — record it but don't crash.
			break
		}
	}

	// Flush any remaining carry as a final partial line.
	if carry != "" {
		pane.appendLine(carry)
	}

	// Wait for process to fully exit and get exit code.
	waitErr := cmd.Wait()
	elapsed := time.Since(start)

	if waitErr != nil {
		pane.fail(elapsed)
		return CommandOutput{Error: waitErr}
	}
	pane.complete(elapsed)
	return CommandOutput{}
}

// isEIO reports whether err is an EIO syscall error.
// On Linux, reading from a PTY master after the child exits returns EIO.
func isEIO(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EIO
	}
	return false
}

// dirName returns the base name of a directory path for display purposes.
func dirName(dir string) string {
	return filepath.Base(strings.TrimRight(dir, string(os.PathSeparator)))
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add pty-runner.go
git commit -m "feat: add PTY runner for interactive subprocesses"
```

---

## Task 6: Dashboard renderer

**Files:**
- Modify: `dashboard.go` (add renderer, signal handling, terminal hygiene to the file created in Task 4)

- [ ] **Step 1: Append the renderer to `dashboard.go`**

Add the following to the end of `dashboard.go` (after `computeLayout`):

```go
// dashboard manages the in-place terminal UI for interactive mode.
// It owns stdout exclusively while running.
type dashboard struct {
	panes      []*PaneBuffer
	totalLines int // how many lines the last render occupied
	done       chan struct{}
}

func newDashboard(panes []*PaneBuffer) *dashboard {
	return &dashboard{
		panes: panes,
		done:  make(chan struct{}),
	}
}

// start begins the render loop and returns immediately.
// Call stop() when all tasks are complete.
func (d *dashboard) start() {
	// Hide cursor.
	fmt.Print("\033[?25l")

	go d.loop()
}

// stop halts the render loop, does a final render, and restores the terminal.
func (d *dashboard) stop() {
	close(d.done)
	time.Sleep(50 * time.Millisecond) // let loop drain
	d.render()
	// Show cursor.
	fmt.Print("\033[?25h")
	// Move past the rendered area.
	fmt.Println()
}

func (d *dashboard) loop() {
	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()

	// Listen for terminal resize.
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, syscall.SIGWINCH)
	defer signal.Stop(sigwinch)

	for {
		select {
		case <-d.done:
			return
		case <-sigwinch:
			d.render()
		case <-ticker.C:
			d.render()
		}
	}
}

func (d *dashboard) render() {
	termWidth, termHeight, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		termWidth, termHeight = 120, 40
	}

	layouts := computeLayout(d.panes, termHeight)

	var sb strings.Builder

	// Move cursor up to top of previously rendered region.
	if d.totalLines > 0 {
		fmt.Fprintf(&sb, "\033[%dA", d.totalLines)
	}

	lineCount := 0

	for i, pb := range d.panes {
		layout := layouts[i]
		state := pb.getState()
		dur := pb.getDuration()
		if state == Active {
			dur = time.Since(pb.startedAt).Round(time.Second)
		}
		dirLabel := pb.dir

		switch state {
		case Done:
			line := fmt.Sprintf("✓ %s[%s]%s done in %s",
				pb.color, dirLabel, resetColor, dur.Round(time.Second))
			fmt.Fprintln(&sb, truncate(line, termWidth))
			lineCount++

		case Failed:
			header := fmt.Sprintf("✗ %s[%s]%s failed in %s",
				pb.color, dirLabel, resetColor, dur.Round(time.Second))
			fmt.Fprintln(&sb, truncate(header, termWidth))
			lineCount++
			lines := pb.snapshot()
			for _, l := range lines {
				fmt.Fprintln(&sb, truncate(pb.color+l+resetColor, termWidth))
				lineCount++
			}

		case Active:
			header := fmt.Sprintf("%s[%s]%s ── running… %s",
				pb.color, dirLabel, resetColor, dur)
			fmt.Fprintln(&sb, truncate(header, termWidth))
			lineCount++
			lines := pb.snapshot()
			// Show last layout.contentLines lines.
			start := 0
			if len(lines) > layout.contentLines {
				start = len(lines) - layout.contentLines
			}
			shown := lines[start:]
			for _, l := range shown {
				fmt.Fprintln(&sb, truncate(pb.color+l+resetColor, termWidth))
				lineCount++
			}
			// Pad to fill allocated height.
			for j := len(shown); j < layout.contentLines; j++ {
				fmt.Fprintln(&sb, "\033[2K") // erase line
				lineCount++
			}
		}
	}

	// Sentinel blank line.
	fmt.Fprintln(&sb, "\033[2K")
	lineCount++

	fmt.Print(sb.String())
	d.totalLines = lineCount
}

// truncate trims s to at most maxCols visible characters (approximate —
// does not account for ANSI escape widths, but avoids hard wrapping).
func truncate(s string, maxCols int) string {
	if len(s) <= maxCols {
		return s
	}
	return s[:maxCols]
}
```

- [ ] **Step 2: Verify the file compiles**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 3: Run all tests to check nothing broke**

```bash
go test -race ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add dashboard.go
git commit -m "feat: add dashboard renderer with adaptive pane layout"
```

---

## Task 7: `interactiveRun` entry point

**Files:**
- Create: `interactive.go`

- [ ] **Step 1: Implement `interactive.go`**

```go
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	"golang.org/x/term"
)

// interactiveRun executes shellCmd across all directories using PTY runners
// and a live dashboard. It blocks until all tasks complete.
func interactiveRun(directories []string, shellCmd, branchFilter string, colorMap map[string]string) bool {
	// Non-TTY fallback.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "quickly: --interactive requires a TTY; falling back to streaming mode")
		hasErrors := false
		for _, dir := range directories {
			result := executeCommand(Task{
				Directory:    dir,
				ShellCmd:     shellCmd,
				BranchFilter: branchFilter,
				Color:        colorMap[dir],
			})
			if result.Error != nil {
				hasErrors = true
			}
		}
		return hasErrors
	}

	numWorkers := runtime.NumCPU()
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Build panes — one per directory.
	panes := make([]*PaneBuffer, len(directories))
	for i, dir := range directories {
		panes[i] = newPaneBuffer(filepath.Base(dir), colorMap[dir], maxPaneLines)
	}

	// Task channel: buffered so we can enqueue all tasks upfront.
	type indexedTask struct {
		index int
		task  Task
	}
	taskCh := make(chan indexedTask, len(directories))
	for i, dir := range directories {
		taskCh <- indexedTask{
			index: i,
			task: Task{
				Directory:    dir,
				ShellCmd:     shellCmd,
				BranchFilter: branchFilter,
				Color:        colorMap[dir],
			},
		}
	}
	close(taskCh)

	// Handle SIGINT: forward to all child process groups then exit.
	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, syscall.SIGINT)

	dash := newDashboard(panes)
	dash.start()
	defer dash.stop()

	// Restore cursor on SIGINT.
	go func() {
		<-sigint
		signal.Stop(sigint)
		dash.stop()
		os.Exit(1)
	}()

	results := make(chan CommandOutput, len(directories))
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range taskCh {
				results <- ptyRunner(it.task, panes[it.index])
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	hasErrors := false
	for result := range results {
		if result.Error != nil {
			hasErrors = true
		}
	}

	return hasErrors
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add interactive.go
git commit -m "feat: add interactiveRun entry point"
```

---

## Task 8: Wire `--interactive` / `-i` into `main`

**Files:**
- Modify: `quickly.go`

- [ ] **Step 1: Update flag parsing in `main()` in `quickly.go`**

Locate the existing argument parsing block (around `os.Args[1] == "--if-branch"`). Replace the entire block from `branchFilter := ""` to `close(tasks)` with the version below. The worker pool, results drain, and `os.Exit` remain unchanged — only the routing before them changes.

```go
	// Filter out --if-branch / -b and --interactive / -i from the command.
	branchFilter := ""
	interactive := false

	args := os.Args[1:]
	var remaining []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--if-branch", "-b":
			if i+1 < len(args) {
				branchFilter = args[i+1]
				i++
			}
		case "--interactive", "-i":
			interactive = true
		default:
			remaining = append(remaining, args[i])
		}
	}
	shellCmd := strings.Join(remaining, " ")

	if shellCmd != "status" {
		if !strings.Contains(shellCmd, "--color") {
			shellCmd = strings.ReplaceAll(shellCmd, "ls ", "ls --color=always ")
			shellCmd = strings.ReplaceAll(shellCmd, "grep ", "grep --color=always ")
		}
		if !strings.Contains(shellCmd, "-c color") {
			shellCmd = strings.ReplaceAll(shellCmd, "git ", "git -c color.status=always ")
		}
	}

	// Assign unique colours to directories.
	colorMap := assignColors(config.Directories)

	if interactive {
		hasErrors := interactiveRun(config.Directories, shellCmd, branchFilter, colorMap)
		if hasErrors {
			os.Exit(1)
		}
		return
	}

	numWorkers := recommendedWorkerCount(len(config.Directories), runtime.NumCPU())
	tasks := make(chan Task, len(config.Directories))
	results := make(chan CommandOutput, len(config.Directories))
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go worker(tasks, results, &wg)
	}

	for _, dir := range config.Directories {
		tasks <- Task{
			Directory:    dir,
			ShellCmd:     shellCmd,
			Color:        colorMap[dir],
			BranchFilter: branchFilter,
		}
	}
	close(tasks)
```

Note: remove the now-redundant `colorMap := assignColors(config.Directories)` line that previously existed just before the worker loop (since it's now declared earlier).

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 3: Run all tests**

```bash
go test -race ./...
```

Expected: all existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add quickly.go
git commit -m "feat: wire --interactive/-i flag into main"
```

---

## Task 9: Integration test for PTY runner

**Files:**
- Modify: `quickly_test.go`

- [ ] **Step 1: Add the integration test to `quickly_test.go`**

Append the following to the existing test file:

```go
func TestPtyRunnerEmitsLines(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("PTY integration test skipped in CI (no TTY)")
	}

	dir := t.TempDir()
	pb := newPaneBuffer("test", "\033[32m", maxPaneLines)

	result := ptyRunner(Task{
		Directory: dir,
		ShellCmd:  "echo hello && sleep 0.05 && echo world",
		Color:     "\033[32m",
	}, pb)

	if result.Error != nil {
		t.Fatalf("ptyRunner() error = %v", result.Error)
	}
	if pb.getState() != Done {
		t.Fatalf("pane state = %v, want Done", pb.getState())
	}

	lines := pb.snapshot()
	found := map[string]bool{}
	for _, l := range lines {
		found[l] = true
	}
	if !found["hello"] {
		t.Fatalf("lines=%v, want 'hello' present", lines)
	}
	if !found["world"] {
		t.Fatalf("lines=%v, want 'world' present", lines)
	}
}
```

Also add `"os"` to the import block in `quickly_test.go` if it is not already present.

- [ ] **Step 2: Run just this test**

```bash
go test -race -run TestPtyRunnerEmitsLines ./...
```

Expected: PASS (or SKIP in CI).

- [ ] **Step 3: Run the full test suite**

```bash
go test -race ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add quickly_test.go
git commit -m "test: add PTY runner integration test"
```

---

## Task 10: Smoke test and final verification

- [ ] **Step 1: Build the binary**

```bash
go build -o quickly .
```

Expected: `quickly` binary created, no errors.

- [ ] **Step 2: Run the full test suite with race detector**

```bash
go test -race ./...
```

Expected: all tests pass, no race conditions reported.

- [ ] **Step 3: Manual smoke test**

Add a temporary directory to `~/.quicklyrc` (or use existing entries) and run:

```bash
./quickly -i echo "hello from interactive mode"
```

Expected: dashboard renders with one pane per repo, each showing `hello from interactive mode`, then collapses to `✓ [repo] done in 0s`.

- [ ] **Step 4: Verify streaming mode is unaffected**

```bash
./quickly echo "hello from streaming mode"
```

Expected: prefixed streaming output as before, no dashboard.

- [ ] **Step 5: Final commit if any stray changes remain**

```bash
go mod tidy
git add -A
git status  # should be clean, or commit any go.sum drift
```
