package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
