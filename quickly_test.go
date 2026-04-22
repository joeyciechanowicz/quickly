package main

import (
	"strings"
	"testing"
)

func TestParseStatusOutput(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		wantBranch    string
		wantBehind    string
		wantTracked   int
		wantUntracked int
	}{
		{
			name:          "clean branch with upstream",
			output:        "## main...origin/main\n",
			wantBranch:    "main",
			wantBehind:    "",
			wantTracked:   0,
			wantUntracked: 0,
		},
		{
			name:          "branch behind upstream with tracked modifications and untracked files",
			output:        "## feature/test...origin/feature/test [behind 2]\n M quickly.go\n?? quickly_test.go\n",
			wantBranch:    "feature/test",
			wantBehind:    "[behind 2]",
			wantTracked:   1,
			wantUntracked: 1,
		},
		{
			name:          "unborn branch",
			output:        "## No commits yet on main\n",
			wantBranch:    "No commits yet on main",
			wantBehind:    "",
			wantTracked:   0,
			wantUntracked: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseStatusOutput([]byte(tt.output))
			if err != nil {
				t.Fatalf("parseStatusOutput returned error: %v", err)
			}

			if parsed.BranchName != tt.wantBranch {
				t.Fatalf("BranchName = %q, want %q", parsed.BranchName, tt.wantBranch)
			}
			if parsed.Behind != tt.wantBehind {
				t.Fatalf("Behind = %q, want %q", parsed.Behind, tt.wantBehind)
			}
			if parsed.TrackedCount != tt.wantTracked {
				t.Fatalf("TrackedCount = %d, want %d", parsed.TrackedCount, tt.wantTracked)
			}
			if parsed.UntrackedCount != tt.wantUntracked {
				t.Fatalf("UntrackedCount = %d, want %d", parsed.UntrackedCount, tt.wantUntracked)
			}
		})
	}
}

func TestParseStatusOutputReturnsErrorForEmptyOutput(t *testing.T) {
	_, err := parseStatusOutput(nil)
	if err == nil {
		t.Fatal("parseStatusOutput() error = nil, want error")
	}
}

func TestParseStatusOutputCountsTrackedAndUntrackedSeparately(t *testing.T) {
	parsed, err := parseStatusOutput([]byte("## main...origin/main\n M tracked.go\n?? untracked.go\n?? docs.md\n"))
	if err != nil {
		t.Fatalf("parseStatusOutput returned error: %v", err)
	}
	if parsed.TrackedCount != 1 {
		t.Fatalf("TrackedCount = %d, want 1", parsed.TrackedCount)
	}
	if parsed.UntrackedCount != 2 {
		t.Fatalf("UntrackedCount = %d, want 2", parsed.UntrackedCount)
	}
}

func TestStatusMatchesBranchFilter(t *testing.T) {
	parsed := statusSummary{BranchName: "feature/test"}

	if !statusMatchesBranchFilter(parsed, "feature") {
		t.Fatal("statusMatchesBranchFilter() = false, want true")
	}
	if statusMatchesBranchFilter(parsed, "main") {
		t.Fatal("statusMatchesBranchFilter() = true, want false")
	}
	if !statusMatchesBranchFilter(parsed, "") {
		t.Fatal("statusMatchesBranchFilter() = false for empty filter, want true")
	}
}

func TestRecommendedWorkerCount(t *testing.T) {
	tests := []struct {
		name       string
		repos      int
		cpuCount   int
		wantWorker int
	}{
		{name: "no repos", repos: 0, cpuCount: 4, wantWorker: 1},
		{name: "single repo", repos: 1, cpuCount: 4, wantWorker: 1},
		{name: "few repos", repos: 3, cpuCount: 4, wantWorker: 3},
		{name: "io bound cap scales above cpu", repos: 20, cpuCount: 4, wantWorker: 8},
		{name: "never exceeds repo count", repos: 6, cpuCount: 8, wantWorker: 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recommendedWorkerCount(tt.repos, tt.cpuCount)
			if got != tt.wantWorker {
				t.Fatalf("recommendedWorkerCount(%d, %d) = %d, want %d", tt.repos, tt.cpuCount, got, tt.wantWorker)
			}
		})
	}
}

func TestFormatStatusLineUsesPromptStyleCounts(t *testing.T) {
	tests := []struct {
		name    string
		summary statusSummary
		wants   []string
		avoids  []string
	}{
		{
			name: "clean repo",
			summary: statusSummary{
				BranchName: "main",
				Behind:     "[behind 1]",
			},
			wants:  []string{"[repo]", "main", "Clean", "[behind 1]"},
			avoids: []string{"!", "?"},
		},
		{
			name: "tracked changes only",
			summary: statusSummary{
				BranchName:   "main",
				TrackedCount: 2,
			},
			wants:  []string{"[repo]", "main", "!2"},
			avoids: []string{"?"},
		},
		{
			name: "tracked and untracked changes",
			summary: statusSummary{
				BranchName:     "main",
				TrackedCount:   1,
				UntrackedCount: 2,
			},
			wants: []string{"[repo]", "main", "!1", "?2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := formatStatusLine(&PrefixedWriter{directory: "repo", color: ""}, tt.summary)

			for _, want := range tt.wants {
				if !strings.Contains(line, want) {
					t.Fatalf("formatStatusLine() = %q, want substring %q", line, want)
				}
			}

			for _, avoid := range tt.avoids {
				if strings.Contains(line, avoid) {
					t.Fatalf("formatStatusLine() = %q, want to avoid substring %q", line, avoid)
				}
			}
		})
	}
}

func TestExecuteCommandStatusBranchFilterReturnsWithoutBranchLookup(t *testing.T) {
	task := Task{
		Directory:    "/definitely/missing/repo",
		ShellCmd:     "status",
		BranchFilter: "feature",
	}

	result := executeCommand(task)
	if result.Error == nil {
		t.Fatal("executeCommand() error = nil, want git status error from missing repo")
	}
	if strings.Contains(result.Error.Error(), "failed to get branch name") {
		t.Fatalf("executeCommand() error = %q, want status path without branch lookup", result.Error)
	}
}
