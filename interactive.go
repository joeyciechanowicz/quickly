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
// and a live dashboard. Blocks until all tasks complete.
func interactiveRun(directories []string, shellCmd, branchFilter string, colorMap map[string]string) bool {
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

	panes := make([]*PaneBuffer, len(directories))
	for i, dir := range directories {
		panes[i] = newPaneBuffer(filepath.Base(dir), colorMap[dir], maxPaneLines)
	}

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

	sigint := make(chan os.Signal, 1)
	signal.Notify(sigint, syscall.SIGINT)

	dash := newDashboard(panes)
	dash.start()
	defer dash.stop()

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
