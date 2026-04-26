package main

import (
	"sync"
	"time"
)

type PaneState int

const (
	Queued PaneState = iota
	Active
	Done
	Failed
)

const maxPaneLines = 20

type PaneBuffer struct {
	mu        sync.Mutex
	dir       string
	color     string
	lines     []string
	head      int
	count     int
	cap       int
	state     PaneState
	duration  time.Duration
	startedAt time.Time
}

func newPaneBuffer(dir, color string, capacity int) *PaneBuffer {
	return &PaneBuffer{
		dir:   dir,
		color: color,
		lines: make([]string, capacity),
		cap:   capacity,
		state: Queued,
	}
}

func (pb *PaneBuffer) start() {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.state = Active
	pb.startedAt = time.Now()
}

func (pb *PaneBuffer) appendLine(line string) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	if pb.count < pb.cap {
		pb.lines[(pb.head+pb.count)%pb.cap] = line
		pb.count++
		return
	}
	pb.lines[pb.head] = line
	pb.head = (pb.head + 1) % pb.cap
}

func (pb *PaneBuffer) snapshot() []string {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	out := make([]string, pb.count)
	for i := 0; i < pb.count; i++ {
		out[i] = pb.lines[(pb.head+i)%pb.cap]
	}
	return out
}

func (pb *PaneBuffer) complete(d time.Duration) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.state = Done
	pb.duration = d
}

func (pb *PaneBuffer) fail(d time.Duration) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.state = Failed
	pb.duration = d
}

func (pb *PaneBuffer) getState() PaneState {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.state
}

func (pb *PaneBuffer) getDuration() time.Duration {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.duration
}
