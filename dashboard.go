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
	contentLines int
}

// computeLayout calculates how many content lines each pane gets given
// the available terminal height.
//
// Failed panes keep their full line history.
// Done panes get 0 content lines (rendered as a single summary line).
// Active panes share remaining height equally, minimum 3 lines each.
func computeLayout(panes []*PaneBuffer, termHeight int) []paneLayout {
	layouts := make([]paneLayout, len(panes))

	failedLines := 0
	doneCount := 0
	activeCount := 0

	for i, pb := range panes {
		switch pb.getState() {
		case Failed:
			n := len(pb.snapshot())
			layouts[i].contentLines = n
			failedLines += n + 1
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

// dashboard manages the in-place terminal UI for interactive mode.
// It owns stdout exclusively while running.
type dashboard struct {
	panes      []*PaneBuffer
	totalLines int
	done       chan struct{}
}

func newDashboard(panes []*PaneBuffer) *dashboard {
	return &dashboard{
		panes: panes,
		done:  make(chan struct{}),
	}
}

func (d *dashboard) start() {
	fmt.Print("\033[?25l")
	go d.loop()
}

func (d *dashboard) stop() {
	close(d.done)
	time.Sleep(50 * time.Millisecond)
	d.render()
	fmt.Print("\033[?25h")
	fmt.Println()
}

func (d *dashboard) loop() {
	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()

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
			start := 0
			if len(lines) > layout.contentLines {
				start = len(lines) - layout.contentLines
			}
			shown := lines[start:]
			for _, l := range shown {
				fmt.Fprintln(&sb, truncate(pb.color+l+resetColor, termWidth))
				lineCount++
			}
			for j := len(shown); j < layout.contentLines; j++ {
				fmt.Fprintln(&sb, "\033[2K")
				lineCount++
			}
		}
	}

	fmt.Fprintln(&sb, "\033[2K")
	lineCount++

	fmt.Print(sb.String())
	d.totalLines = lineCount
}

// truncate trims s to at most maxCols visible characters (approximate).
func truncate(s string, maxCols int) string {
	if len(s) <= maxCols {
		return s
	}
	return s[:maxCols]
}
