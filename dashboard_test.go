package main

import (
	"testing"
	"time"
)

func makePanes(states ...PaneState) []*PaneBuffer {
	panes := make([]*PaneBuffer, len(states))
	for i, s := range states {
		panes[i] = newPaneBuffer("repo", "\033[32m", maxPaneLines)
		switch s {
		case Active:
			panes[i].start()
		case Done:
			panes[i].start()
			panes[i].complete(time.Second)
		case Failed:
			panes[i].start()
			panes[i].fail(time.Second)
			for j := 0; j < 5; j++ {
				panes[i].appendLine("error line")
			}
		}
	}
	return panes
}

func TestComputeLayoutEqualSplitActive(t *testing.T) {
	panes := makePanes(Active, Active, Active)
	layouts := computeLayout(panes, 30)
	for i, l := range layouts {
		if l.contentLines < 3 {
			t.Fatalf("pane %d contentLines=%d, want >= 3", i, l.contentLines)
		}
	}
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

func TestComputeLayoutFailedPanesHidden(t *testing.T) {
	panes := makePanes(Failed, Active)
	layouts := computeLayout(panes, 30)
	if layouts[0].contentLines != 0 {
		t.Fatalf("Failed pane contentLines=%d, want 0 (failures shown in summary)", layouts[0].contentLines)
	}
	if layouts[1].contentLines < 3 {
		t.Fatalf("Active pane contentLines=%d, want >= 3", layouts[1].contentLines)
	}
}

func TestComputeLayoutMinimumThreeLinesPerActive(t *testing.T) {
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
