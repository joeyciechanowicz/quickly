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
	pb.appendLine("d")
	lines := pb.snapshot()
	if len(lines) != 3 || lines[0] != "b" || lines[1] != "c" || lines[2] != "d" {
		t.Fatalf("snapshot()=%v, want [b c d]", lines)
	}
}

func TestPaneBufferStateTransitionsToDone(t *testing.T) {
	pb := newPaneBuffer("myrepo", "\033[32m", 5)
	if pb.getState() != Queued {
		t.Fatal("initial state should be Queued")
	}
	pb.start()
	if pb.getState() != Active {
		t.Fatal("state should be Active after start()")
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
