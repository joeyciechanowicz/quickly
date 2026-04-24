package main

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
