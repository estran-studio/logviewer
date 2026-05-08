package local

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"text/template"

	"github.com/estran-studio/logviewer/pkg/adapter/hl"
	mylog "github.com/estran-studio/logviewer/pkg/log"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/reader"
)

const (
	// OptionsCmd is the key for the command to execute.
	OptionsCmd = "cmd"
	// OptionsShell is the key for the shell to use.
	OptionsShell = "shell"
	// OptionsPaths specifies file paths to read logs from.
	// When paths are provided and hl is available, hl will be used for high-performance filtering.
	OptionsPaths = "paths"
	// OptionsPreferNativeDriver when set to true, disables hl usage and forces the native Go engine.
	OptionsPreferNativeDriver = "preferNativeDriver"

	defaultShellWindows    = "powershell"
	defaultShellArgWindows = "-Command"
	defaultShellUnix       = "sh"
	defaultShellArgUnix    = "-c"
)

type localLogClient struct{}

func getCommand(search *client.LogSearch) (string, error) {
	cmdTplStr := search.Options.GetString(OptionsCmd)

	if cmdTplStr == "" {
		return "", errors.New("cmd is missing for localLogClient")
	}

	tmpl, err := template.New("cmd").Parse(cmdTplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse command template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, search); err != nil {
		return "", fmt.Errorf("failed to execute command template: %w", err)
	}
	return buf.String(), nil
}

func (lc localLogClient) Get(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	// Check if we should use hl (high-performance log viewer)
	paths, hasPaths := search.Options.GetListOfStringsOk(OptionsPaths)
	preferNative := search.Options.GetBool(OptionsPreferNativeDriver)

	if hasPaths && len(paths) > 0 && !preferNative && hl.IsAvailable() {
		return lc.getWithHL(ctx, search, paths)
	}

	// Fall back to native command execution
	return lc.getWithNativeCmd(ctx, search)
}

// getWithHL executes the query using the hl binary for high-performance filtering.
func (lc localLogClient) getWithHL(ctx context.Context, search *client.LogSearch, paths []string) (client.LogSearchResult, error) {
	mylog.Debug("using hl engine for local log query, paths=%v", paths)

	// Build hl arguments from the search
	args, err := hl.BuildArgs(search, paths)
	if err != nil {
		mylog.Warn("failed to build hl arguments, falling back to native engine: %v", err)
		return lc.getWithNativeCmd(ctx, search)
	}

	hlPath := hl.GetPath()
	mylog.Debug("executing hl command: %s %v", hlPath, args)

	ecmd := exec.CommandContext(ctx, hlPath, args...) //nolint:gosec

	stdout, err := ecmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("hl stdout pipe: %w", err)
	}

	stderr, err := ecmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("hl stderr pipe: %w", err)
	}

	if err = ecmd.Start(); err != nil {
		mylog.Warn("failed to start hl, falling back to native engine: %v", err)
		return lc.getWithNativeCmd(ctx, search)
	}

	// Collect stderr in background (don't call Wait() here - it closes stdout!)
	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteString("\n")
		}
	}()

	// Create error channel that will be populated after stdout is consumed
	errChan := make(chan error, 1)

	scanner := bufio.NewScanner(stdout)

	// Create a modified search that marks results as pre-filtered
	// This prevents double-filtering in the reader
	preFilteredSearch := *search
	if preFilteredSearch.Options == nil {
		preFilteredSearch.Options = make(map[string]interface{})
	} else {
		// Deep copy the Options map to avoid modifying the original
		newOptions := make(map[string]interface{})
		for k, v := range preFilteredSearch.Options {
			newOptions[k] = v
		}
		preFilteredSearch.Options = newOptions
	}
	preFilteredSearch.Options["__preFiltered__"] = true

	mylog.Debug("hl preFiltered search created, preFiltered=%v", preFilteredSearch.Options.GetBool("__preFiltered__"))

	// Create a custom closer that waits for the command after stdout is consumed
	closer := &hlCloser{
		stdout:     stdout,
		cmd:        ecmd,
		stderrDone: stderrDone,
		stderrBuf:  &stderrBuf,
		errChan:    errChan,
	}

	result, err := reader.GetLogResult(&preFilteredSearch, scanner, closer)
	if err != nil {
		return nil, err
	}
	result.ErrChan = errChan
	return result, nil
}

// hlCloser wraps stdout and ensures proper cleanup order:
// 1. Close stdout (done by reader)
// 2. Wait for stderr goroutine to finish
// 3. Call Wait() on the command (safe now that stdout is consumed)
type hlCloser struct {
	stdout     interface{ Close() error }
	cmd        *exec.Cmd
	stderrDone <-chan struct{}
	stderrBuf  *bytes.Buffer
	errChan    chan error
}

func (c *hlCloser) Close() error {
	// First close stdout
	if err := c.stdout.Close(); err != nil {
		return err
	}

	// Wait for stderr to be fully read
	<-c.stderrDone

	// Now it's safe to call Wait() - stdout is consumed
	if err := c.cmd.Wait(); err != nil {
		if c.stderrBuf.Len() > 0 {
			c.errChan <- fmt.Errorf("hl command failed: %w (stderr: %s)", err, c.stderrBuf.String())
		} else {
			c.errChan <- fmt.Errorf("hl command failed: %w", err)
		}
	}
	close(c.errChan)
	return nil
}

// getWithNativeCmd executes the query using the native shell command approach.
func (lc localLogClient) getWithNativeCmd(ctx context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	cmdContent, err := getCommand(search)
	if err != nil {
		if err.Error() == "cmd is missing for localLogClient" {
			// Check if we have paths but no hl - provide helpful error
			if paths, ok := search.Options.GetListOfStringsOk(OptionsPaths); ok && len(paths) > 0 {
				return nil, errors.New("hl is not available and no fallback 'cmd' is configured; install hl or provide a cmd option")
			}
			panic(err)
		}
		return nil, err
	}

	mylog.Debug("using native engine for local log query, cmd=%s", cmdContent)

	var shellName string
	var shellArgs []string

	if customShell, ok := search.Options.GetListOfStringsOk(OptionsShell); ok && len(customShell) > 0 {
		shellName = customShell[0]
		shellArgs = customShell[1:]
	} else {
		if runtime.GOOS == "windows" {
			shellName = defaultShellWindows
			shellArgs = []string{defaultShellArgWindows}
		} else {
			shellName = defaultShellUnix
			shellArgs = []string{defaultShellArgUnix}
		}
	}

	shellArgs = append(shellArgs, cmdContent)

	ecmd := exec.CommandContext(ctx, shellName, shellArgs...) //nolint:gosec

	stdout, err := ecmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = ecmd.Start(); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)

	return reader.GetLogResult(search, scanner, stdout)
}

func (lc localLogClient) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	// For local/text-based backends, we need to run a search and extract field values
	result, err := lc.Get(ctx, search)
	if err != nil {
		return nil, err
	}
	return client.GetFieldValuesFromResult(ctx, result, fields)
}

// GetLogClient returns a new local log client.
func GetLogClient() (client.LogBackend, error) {
	return localLogClient{}, nil
}
