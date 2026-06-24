package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateOptionsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  options
		want string
	}{
		{
			name: "json and raw",
			cfg:  options{jsonOutput: true, rawOutput: true},
			want: "--json and --raw",
		},
		{
			name: "short timeout",
			cfg:  options{timeout: 500 * time.Millisecond, color: "auto"},
			want: "--timeout",
		},
		{
			name: "short watch",
			cfg:  options{timeout: 12 * time.Second, watch: 5 * time.Second, color: "auto"},
			want: "--watch",
		},
		{
			name: "bad retries",
			cfg:  options{timeout: 12 * time.Second, retries: 4, color: "auto"},
			want: "--retries",
		},
		{
			name: "narrow width",
			cfg:  options{timeout: 12 * time.Second, width: 57, color: "auto"},
			want: "--width",
		},
		{
			name: "bad color",
			cfg:  options{timeout: 12 * time.Second, color: "sometimes"},
			want: "--color",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message := validateOptions(test.cfg)
			if !strings.Contains(message, test.want) {
				t.Fatalf("validateOptions() = %q, want substring %q", message, test.want)
			}
		})
	}
}

func TestParseFlagsRejectsPositionalArguments(t *testing.T) {
	var stderr bytes.Buffer
	_, code := parseFlags([]string{"extra"}, &stderr)
	if code != 64 {
		t.Fatalf("exit code = %d, want 64", code)
	}
	if !strings.Contains(stderr.String(), "Unexpected positional argument") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunVersionDoesNotRequireCredentials(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "1.2.3", "abc123", "2026-06-25T00:00:00Z"
	defer func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	}()

	directory := t.TempDir()
	stdoutPath := filepath.Join(directory, "stdout")
	stderrPath := filepath.Join(directory, "stderr")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		t.Fatal(err)
	}

	code := run([]string{"--version"}, stdout, stderr)
	if closeErr := stdout.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if closeErr := stderr.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	output, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "codex-meter 1.2.3 (abc123, 2026-06-25T00:00:00Z") {
		t.Fatalf("stdout = %q", string(output))
	}
	errOutput, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(errOutput)) != "" {
		t.Fatalf("stderr = %q", string(errOutput))
	}
}
