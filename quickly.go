package main

import (
	"bufio"
	"fmt"
	"io"
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
	Directory string
	Output    string
	Error     error
	Color     string
}

type Task struct {
	Directory string
	ShellCmd  string
	Color     string
}

type prefixedWriter struct {
	directory string
	writer    io.Writer
	color     string
}

var colors = []string{
	"\033[31m", // Red
	"\033[32m", // Green
	"\033[33m", // Yellow
	"\033[34m", // Blue
	"\033[35m", // Magenta
	"\033[36m", // Cyan
	// "\033[91m", // Bright Red
	// "\033[92m", // Bright Green
	// "\033[93m", // Bright Yellow
	// "\033[94m", // Bright Blue
	// "\033[95m", // Bright Magenta
	// "\033[96m", // Bright Cyan
}

const resetColor = "\033[0m"

const minLength = 25

func createDefaultConfig(configPath string) error {
	// Create empty config file
	file, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	// Get current working directory as default
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Write current directory as default
	if _, err := fmt.Fprintln(file, currentDir); err != nil {
		return fmt.Errorf("failed to write to config file: %w", err)
	}

	return nil
}

func readConfig() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".quicklyrc")

	// Try to create config if it doesn't exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := createDefaultConfig(configPath); err != nil {
			return Config{}, fmt.Errorf("failed to create default config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Created new config file at %s with current directory\n", configPath)
	}

	file, err := os.Open(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var directories []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		dir := strings.TrimSpace(scanner.Text())
		if dir != "" {
			directories = append(directories, dir)
		}
	}

	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}

	return Config{Directories: directories}, nil
}

func assignColors(directories []string) map[string]string {
	colorMap := make(map[string]string)
	for i, dir := range directories {
		colorMap[dir] = colors[i%len(colors)]
	}
	return colorMap
}

func (w *prefixedWriter) Write(p []byte) (n int, err error) {
	scanner := bufio.NewScanner(strings.NewReader(string(p)))
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(w.writer, "%s[%s]%s %s\n",
			w.color,
			w.directory,
			resetColor,
			line,
		)
	}

	return len(p), nil
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

func executeCommand(task Task) CommandOutput {
	if task.ShellCmd == "" {
		return CommandOutput{
			Directory: task.Directory,
			Error:     fmt.Errorf("no command provided"),
		}
	}

	writer := &prefixedWriter{
		directory: filepath.Base(task.Directory),
		color:     task.Color,
		writer:    os.Stdout,
	}

	// Handle the `status` command separately to give cleaner output
	if task.ShellCmd == "status" {
		cmd := exec.Command("git", "status", "--branch", "--porcelain")
		cmd.Dir = task.Directory
		output, err := cmd.CombinedOutput()

		if err != nil {
			return CommandOutput{
				Directory: task.Directory,
				Error:     err,
			}
		}
		lines := strings.Split(string(output), "\n")
		lines = filterStrings(lines)

		branchInfo := lines[0]
		lines = lines[1:]

		re, _ := regexp.Compile(`(\[.+\])`)
		behind := re.FindString(branchInfo)
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

		return CommandOutput{
			Directory: task.Directory,
			Color:     task.Color,
			Error:     nil,
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
		Directory: task.Directory,
		Error:     err,
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

	// Assign unique colors to directories
	colorMap := assignColors(config.Directories)

	numWorkers := runtime.NumCPU()
	tasks := make(chan Task, len(config.Directories))
	results := make(chan CommandOutput, len(config.Directories))
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(tasks, results, &wg)
	}

	shellCmd := strings.Join(os.Args[1:], " ")

	// Explicitly set color flags for common commands
	if !strings.Contains(shellCmd, "--color") {
		shellCmd = strings.ReplaceAll(shellCmd, "ls ", "ls --color=always ")
		shellCmd = strings.ReplaceAll(shellCmd, "grep ", "grep --color=always ")
	}
	if !strings.Contains(shellCmd, "-c color") {
		shellCmd = strings.ReplaceAll(shellCmd, "git ", "git -c color.status=always ")
	}

	// Send tasks to workers
	for _, dir := range config.Directories {
		tasks <- Task{
			Directory: dir,
			ShellCmd:  shellCmd,
			Color:     colorMap[dir],
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
			fmt.Printf("%s[%s]%s %s\n",
				result.Color,
				filepath.Base(result.Directory),
				resetColor,
				result.Error,
			)
			// fmt.Fprintf(os.Stderr, "[%s] Command failed: %v\n", filepath.Base(result.Directory), result.Error)
			hasErrors = true
		}
	}

	if hasErrors {
		os.Exit(1)
	}
}
