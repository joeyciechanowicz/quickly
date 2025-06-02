package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

type Config struct {
	Directories []string
}

type CommandOutput struct {
	Output string
	Error  error
	Color  string
}

type Task struct {
	BranchFilter string
	Color        string
	Directory    string
	ShellCmd     string
}

func filterStrings(input []string) []string {
	var filtered []string
	for _, str := range input {
		if strings.TrimSpace(str) != "" {
			filtered = append(filtered, str)
		}
	}
	return filtered
}

var branchRegexp = regexp.MustCompile(`(\[.+\])`)

func status(writer *PrefixedWriter, task Task) error {
	cmd := exec.Command("git", "status", "--branch", "--porcelain")
	cmd.Dir = task.Directory
	output, err := cmd.CombinedOutput()

	if err != nil {
		return err
	}

	lines := strings.Split(string(output), "\n")
	lines = filterStrings(lines)

	branchInfo := lines[0]
	lines = lines[1:]

	behind := branchRegexp.FindString(branchInfo)
	modified := fmt.Sprintf("%sClean%s", "\033[32m", resetColor)
	if len(lines) > 0 {
		modified = fmt.Sprintf("%s%d modified%s", "\033[31m", len(lines), resetColor)
	}

	branchName := strings.Split(branchInfo[3:], "...")[0]

	fmt.Printf("%s%-25s%s %-15s %-10s %s\n",
		writer.color,
		fmt.Sprintf("[%s]", writer.directory),
		resetColor,
		branchName,
		modified,
		behind,
	)

	return nil
}

func branchName(task Task) (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = task.Directory
	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("failed to get branch name: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func executeCommand(task Task) CommandOutput {
	if task.ShellCmd == "" {
		return CommandOutput{
			Error: fmt.Errorf("no command provided"),
		}
	}

	writer := &PrefixedWriter{
		directory: filepath.Base(task.Directory),
		color:     task.Color,
		writer:    os.Stdout,
	}

	if task.BranchFilter != "" {
		currentBranch, err := branchName(task)
		if err != nil {
			return CommandOutput{
				Error: err,
			}
		}

		if !strings.Contains(currentBranch, task.BranchFilter) {
			return CommandOutput{}
		}
	}

	// Handle the `status` command separately to give cleaner output
	if task.ShellCmd == "status" {
		err := status(writer, task)
		if err != nil {
			return CommandOutput{
				Error: err,
			}
		}
		return CommandOutput{}
	}

	cmd := exec.Command("bash", "-c", task.ShellCmd) // Changed to bash for better color support
	cmd.Dir = task.Directory

	// Set environment variables for color output
	env := os.Environ()
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"FORCE_COLOR=true",
		"CLICOLOR=1",
		"CLICOLOR_FORCE=1",
	)
	cmd.Env = env

	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()
	return CommandOutput{
		Error: err,
	}
}

func worker(tasks <-chan Task, results chan<- CommandOutput, wg *sync.WaitGroup) {
	defer wg.Done()
	for task := range tasks {
		results <- executeCommand(task)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: quickly <command> [args...]")
		os.Exit(1)
	}

	config, err := readConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
		os.Exit(1)
	}

	// Filter out --if-branch some-string from the command
	branchFilter := ""

	var shellCmd string
	if os.Args[1] == "--if-branch" || os.Args[1] == "-b" {
		branchFilter = os.Args[2]
		shellCmd = strings.Join(os.Args[3:], " ")
	} else {
		shellCmd = strings.Join(os.Args[1:], " ")
	}

	// Explicitly set color flags for common commands
	if !strings.Contains(shellCmd, "--color") {
		shellCmd = strings.ReplaceAll(shellCmd, "ls ", "ls --color=always ")
		shellCmd = strings.ReplaceAll(shellCmd, "grep ", "grep --color=always ")
	}
	if !strings.Contains(shellCmd, "-c color") {
		shellCmd = strings.ReplaceAll(shellCmd, "git ", "git -c color.status=always ")
	}

	numWorkers := runtime.NumCPU()
	tasks := make(chan Task, len(config.Directories))
	results := make(chan CommandOutput, len(config.Directories))
	var wg sync.WaitGroup

	// Start worker pool
	for range numWorkers {
		wg.Add(1)
		go worker(tasks, results, &wg)
	}

	// Assign unique colors to directories
	colorMap := assignColors(config.Directories)

	// Send tasks to workers
	for _, dir := range config.Directories {
		tasks <- Task{
			Directory:    dir,
			ShellCmd:     shellCmd,
			Color:        colorMap[dir],
			BranchFilter: branchFilter,
		}
	}
	close(tasks)

	// Start a goroutine to close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Handle only errors from results, since output is streamed directly
	hasErrors := false
	for result := range results {
		if result.Error != nil {
			hasErrors = true
		}
	}

	if hasErrors {
		os.Exit(1)
	}
}
