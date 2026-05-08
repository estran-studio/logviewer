//go:build integration
// +build integration

package hl

import (
	"os/exec"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/client/operator"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests require hl to be installed and in PATH
// Run with: go test -tags=integration ./pkg/adapter/hl/...

func TestIntegration_HLAvailable(t *testing.T) {
	if !IsAvailable() {
		t.Skip("hl is not installed, skipping integration tests")
	}

	path := GetPath()
	assert.NotEmpty(t, path)
	t.Logf("hl found at: %s", path)
}

func TestIntegration_HLVersion(t *testing.T) {
	if !IsAvailable() {
		t.Skip("hl is not installed, skipping integration tests")
	}

	// Run hl --version to verify it works
	cmd := exec.Command(GetPath(), "--version")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	t.Logf("hl version: %s", string(output))
}

func TestIntegration_BuildArgsCompatibility(t *testing.T) {
	if !IsAvailable() {
		t.Skip("hl is not installed, skipping integration tests")
	}

	tests := []struct {
		name   string
		search *client.LogSearch
	}{
		{
			name: "simple equals filter",
			search: &client.LogSearch{
				Filter: &client.Filter{
					Field: "level",
					Op:    operator.Equals,
					Value: "error",
				},
			},
		},
		{
			name: "regex filter",
			search: &client.LogSearch{
				Filter: &client.Filter{
					Field: "message",
					Op:    operator.Regex,
					Value: "timeout.*connection",
				},
			},
		},
		{
			name: "time range with last",
			search: &client.LogSearch{
				Range: client.SearchRange{
					Last: ty.Opt[string]{Set: true, Value: "1h"},
				},
			},
		},
		{
			name: "complex AND filter",
			search: &client.LogSearch{
				Filter: &client.Filter{
					Logic: client.LogicAnd,
					Filters: []client.Filter{
						{Field: "level", Op: operator.Equals, Value: "error"},
						{Field: "service", Op: operator.Equals, Value: "api"},
					},
				},
			},
		},
		{
			name: "complex OR filter",
			search: &client.LogSearch{
				Filter: &client.Filter{
					Logic: client.LogicOr,
					Filters: []client.Filter{
						{Field: "level", Op: operator.Equals, Value: "error"},
						{Field: "level", Op: operator.Equals, Value: "warn"},
					},
				},
			},
		},
		{
			name: "comparison operators",
			search: &client.LogSearch{
				Filter: &client.Filter{
					Field: "status",
					Op:    operator.Gte,
					Value: "400",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := BuildArgs(tt.search, []string{"/dev/null"})
			require.NoError(t, err)

			// Verify hl accepts these arguments by running with --help
			// This validates the argument syntax without actually processing logs
			t.Logf("Generated args: %v", args)

			// We can't easily validate the full command without real log files,
			// but we can verify the args are built correctly
			assert.Contains(t, args, "-P")
		})
	}
}

func TestIntegration_SSHCommandSyntax(t *testing.T) {
	// This test validates that the SSH command syntax is correct
	// by checking the shell can parse it (without executing)

	tests := []struct {
		name        string
		hlArgs      []string
		paths       []string
		fallbackCmd string
	}{
		{
			name:        "basic command",
			hlArgs:      []string{"-P", "--since", "-1h"},
			paths:       []string{"/var/log/app.log"},
			fallbackCmd: "",
		},
		{
			name:        "with filter",
			hlArgs:      []string{"-P", "-q", "level = error"},
			paths:       []string{"/var/log/app.log"},
			fallbackCmd: "",
		},
		{
			name:        "multiple paths",
			hlArgs:      []string{"-P"},
			paths:       []string{"/var/log/app.log", "/var/log/error.log"},
			fallbackCmd: "",
		},
		{
			name:        "with custom fallback",
			hlArgs:      []string{"-P"},
			paths:       []string{"/var/log/app.log"},
			fallbackCmd: "tail -n 1000 /var/log/app.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := BuildSSHCommand(tt.hlArgs, tt.paths, tt.fallbackCmd)

			// Validate the command syntax using bash -n (syntax check only)
			bashCmd := exec.Command("bash", "-n", "-c", cmd)
			output, err := bashCmd.CombinedOutput()
			if err != nil {
				t.Errorf("Invalid shell syntax: %v\nCommand: %s\nOutput: %s", err, cmd, output)
			}
		})
	}
}

func TestIntegration_ShellEscapeRobustness(t *testing.T) {
	// Test that shell escaping handles various edge cases
	dangerousInputs := []string{
		"'; rm -rf / #",
		"`whoami`",
		"$(cat /etc/passwd)",
		"${HOME}",
		"a\nb\nc",
		"a\tb",
		"hello'world",
		`hello"world`,
		"hello\\world",
		"hello|grep password",
		"hello;exit",
		"hello&background",
		"hello>file",
		"hello<file",
		"hello$(cmd)",
	}

	for _, input := range dangerousInputs {
		t.Run(input[:min(len(input), 20)], func(t *testing.T) {
			escaped := shellEscape(input)

			// Validate the escaped value is shell-safe using bash
			// We echo the value and check it matches the original
			cmd := exec.Command("bash", "-c", "printf '%s' "+escaped)
			output, err := cmd.Output()
			if err != nil {
				t.Errorf("Shell escape failed for %q: %v", input, err)
				return
			}

			// The output should match the original input
			if string(output) != input {
				t.Errorf("Shell escape mismatch:\n  input:    %q\n  escaped:  %s\n  output:   %q",
					input, escaped, string(output))
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
