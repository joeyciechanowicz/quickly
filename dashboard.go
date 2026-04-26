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

	activeCount := 0
	for i, pb := range panes {
		layouts[i].contentLines = 0
		if pb.getState() == Active {
			activeCount++
		}
	}

	if activeCount == 0 {
		return layouts
	}

	dividers := 0
	if activeCount > 1 {
		dividers = activeCount - 1
	}
	// One line reserved for the footer status.
	footer := 1
	remaining := termHeight - activeCount - dividers - footer
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
	d.printFailureSummary()
	fmt.Println(d.statusFooter())
}

func (d *dashboard) statusFooter() string {
	var queued, done, failed, active int
	for _, pb := range d.panes {
		switch pb.getState() {
		case Queued:
			queued++
		case Done:
			done++
		case Failed:
			failed++
		case Active:
			active++
		}
	}
	return fmt.Sprintf(
		"\033[34m%d queued\033[0m  \033[32m%d completed\033[0m  \033[31m%d errored\033[0m  \033[2m%d running\033[0m",
		queued, done, failed, active,
	)
}

func (d *dashboard) printFailureSummary() {
	var failed []*PaneBuffer
	for _, pb := range d.panes {
		if pb.getState() == Failed {
			failed = append(failed, pb)
		}
	}
	if len(failed) == 0 {
		return
	}
	fmt.Printf("\n\033[31m%d command(s) failed:\033[0m\n", len(failed))
	for _, pb := range failed {
		dur := pb.getDuration().Round(time.Second)
		fmt.Printf("\n\033[31m✗\033[0m %s[%s]%s failed in %s\n",
			pb.color, pb.dir, resetColor, dur)
		for _, l := range pb.snapshot() {
			fmt.Println(pb.color + l + resetColor)
		}
	}
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

	rendered := 0
	queued := 0
	doneCount := 0
	failedCount := 0
	activeCount := 0
	for i, pb := range d.panes {
		state := pb.getState()
		switch state {
		case Queued:
			queued++
			continue
		case Done:
			doneCount++
			continue
		case Failed:
			failedCount++
			continue
		case Active:
			activeCount++
		}
		if rendered > 0 {
			writeLine(&sb, strings.Repeat("─", termWidth))
			lineCount++
		}
		rendered++
		layout := layouts[i]
		dur := time.Since(pb.startedAt).Round(time.Second)
		dirLabel := pb.dir

		switch state {
		case Active:
			header := fmt.Sprintf("%s[%s]%s ── running… %s",
				pb.color, dirLabel, resetColor, dur)
			writeLine(&sb, truncate(header, termWidth))
			lineCount++
			lines := pb.snapshot()
			start := 0
			if len(lines) > layout.contentLines {
				start = len(lines) - layout.contentLines
			}
			shown := lines[start:]
			for _, l := range shown {
				writeLine(&sb, truncate(pb.color+l+resetColor, termWidth))
				lineCount++
			}
			for j := len(shown); j < layout.contentLines; j++ {
				writeLine(&sb, "")
				lineCount++
			}
		}
	}

	footer := fmt.Sprintf(
		"\033[34m%d queued\033[0m  \033[32m%d completed\033[0m  \033[31m%d errored\033[0m  \033[2m%d running\033[0m",
		queued, doneCount, failedCount, activeCount,
	)
	writeLine(&sb, truncate(footer, termWidth))
	lineCount++

	fmt.Print(sb.String())
	d.totalLines = lineCount
}

// writeLine emits s followed by an erase-to-end-of-line and a newline.
// The EL ensures any trailing characters from a previous frame's longer
// line at this row are cleared, preventing bleed-through.
func writeLine(b *strings.Builder, s string) {
	b.WriteString(s)
	b.WriteString("\033[K\n")
}

// truncate trims s to at most maxCols visible columns, preserving ANSI SGR
// sequences whole (never slicing inside an escape) and ensuring the result
// ends with a reset so color cannot bleed into following output.
func truncate(s string, maxCols int) string {
	if maxCols <= 0 {
		return resetColor
	}
	var b strings.Builder
	b.Grow(len(s) + len(resetColor))
	visible := 0
	hadSGR := false
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				cc := s[j]
				j++
				if cc >= 0x40 && cc <= 0x7e {
					break
				}
			}
			b.WriteString(s[i:j])
			hadSGR = true
			i = j
			continue
		}
		if visible >= maxCols {
			break
		}
		b.WriteByte(c)
		visible++
		i++
	}
	if hadSGR {
		b.WriteString(resetColor)
	}
	return b.String()
}
