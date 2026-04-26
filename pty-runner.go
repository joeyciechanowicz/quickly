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
	pane.start()
	cmd := exec.Command("bash", "-c", task.ShellCmd)
	cmd.Dir = task.Directory

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
			if errors.Is(readErr, io.EOF) || isEIO(readErr) {
				break
			}
			break
		}
	}

	if carry != "" {
		pane.appendLine(carry)
	}

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
