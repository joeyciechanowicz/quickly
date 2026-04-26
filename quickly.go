package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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

type statusSummary struct {
	BranchName     string
	Behind         string
	TrackedCount   int
	UntrackedCount int
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

func parseStatusOutput(output []byte) (statusSummary, error) {
	lines := strings.Split(string(output), "\n")
	lines = filterStrings(lines)
	if len(lines) == 0 {
		return statusSummary{}, fmt.Errorf("git status returned no output")
	}

	branchInfo := lines[0]
	branchName := strings.TrimPrefix(branchInfo, "## ")
	if strings.Contains(branchName, "...") {
		branchName = strings.Split(branchName, "...")[0]
	}
	if match := branchRegexp.FindString(branchInfo); match != "" {
		branchName = strings.TrimSpace(strings.TrimSuffix(branchName, match))
	}

	trackedCount := 0
	untrackedCount := 0
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "??") {
			untrackedCount++
			continue
		}
		trackedCount++
	}

	return statusSummary{
		BranchName:     branchName,
		Behind:         branchRegexp.FindString(branchInfo),
		TrackedCount:   trackedCount,
		UntrackedCount: untrackedCount,
	}, nil
}

func statusMatchesBranchFilter(summary statusSummary, branchFilter string) bool {
	if branchFilter == "" {
		return true
	}
	return strings.Contains(summary.BranchName, branchFilter)
}

func formatStatusLine(writer *PrefixedWriter, summary statusSummary) string {
	statusParts := []string{}
	if summary.TrackedCount > 0 {
		statusParts = append(statusParts, fmt.Sprintf("\033[33m!%d%s", summary.TrackedCount, resetColor))
	}
	if summary.UntrackedCount > 0 {
		statusParts = append(statusParts, fmt.Sprintf("\033[34m?%d%s", summary.UntrackedCount, resetColor))
	}
	statusText := fmt.Sprintf("%sClean%s", "\033[32m", resetColor)
	if len(statusParts) > 0 {
		statusText = strings.Join(statusParts, " ")
	}

	return fmt.Sprintf("%s%-25s%s %-15s %-10s %s\n",
		writer.color,
		fmt.Sprintf("[%s]", writer.directory),
		resetColor,
		summary.BranchName,
		statusText,
		summary.Behind,
	)
}

func recommendedWorkerCount(repoCount, cpuCount int) int {
	if repoCount <= 1 || cpuCount <= 1 {
		return 1
	}

	workers := cpuCount * 2
	if workers > repoCount {
		workers = repoCount
	}
	if workers < 1 {
		return 1
	}
	return workers
}

func status(writer *PrefixedWriter, task Task) error {
	cmd := exec.Command("git", "status", "--branch", "--porcelain")
	cmd.Dir = task.Directory
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	summary, err := parseStatusOutput(output)
	if err != nil {
		return err
	}
	if !statusMatchesBranchFilter(summary, task.BranchFilter) {
		return nil
	}

	fmt.Print(formatStatusLine(writer, summary))
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

	branchFilter := ""
	interactive := false
	concurrency := 0

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
		case "--concurrency", "-c":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "quickly: --concurrency requires a positive integer")
				os.Exit(1)
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				fmt.Fprintf(os.Stderr, "quickly: invalid --concurrency value %q (must be a positive integer)\n", args[i+1])
				os.Exit(1)
			}
			concurrency = n
			i++
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

	colorMap := assignColors(config.Directories)

	if interactive {
		hasErrors := interactiveRun(config.Directories, shellCmd, branchFilter, colorMap, concurrency)
		if hasErrors {
			os.Exit(1)
		}
		return
	}

	numWorkers := concurrency
	if numWorkers == 0 {
		numWorkers = recommendedWorkerCount(len(config.Directories), runtime.NumCPU())
	}
	if numWorkers > len(config.Directories) {
		numWorkers = len(config.Directories)
	}
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
