// Package ssh implements a LogClient backed by remote SSH access. It contains
// utilities to build remote commands, establish SSH connections and stream
// logs back to the caller.
package ssh

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/estran-studio/logviewer/pkg/adapter/hl"
	mylog "github.com/estran-studio/logviewer/pkg/log"
	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/reader"
	sshc "golang.org/x/crypto/ssh"
	"k8s.io/client-go/util/homedir"
)

const (
	// OptionsCmd is the key for the command to execute.
	OptionsCmd = "cmd"
	// OptionsPaths specifies file paths to read logs from on the remote host.
	// When paths are provided, a hybrid command will be used that checks for hl on the remote host.
	OptionsPaths = "paths"
	// OptionsPreferNativeDriver when set to true, disables hl usage and forces the native command.
	OptionsPreferNativeDriver = "preferNativeDriver"
)

// LogClientOptions defines configuration for the SSH client.
type LogClientOptions struct {
	User string `json:"user"`
	Addr string `json:"addr"`

	PrivateKey string `json:"privateKey"`
	DisablePTY bool   `json:"disablePTY"`
}

type sshLogClient struct {
	conn    *sshc.Client
	options LogClientOptions
}

func getCommand(search *client.LogSearch) (string, error) {
	cmdTplStr := search.Options.GetString(OptionsCmd)

	if cmdTplStr == "" {
		return "", errors.New("cmd is missing for sshLogClient")
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

func (lc sshLogClient) Get(_ context.Context, search *client.LogSearch) (client.LogSearchResult, error) {
	// Check if we should use hl with paths
	paths, hasPaths := search.Options.GetListOfStringsOk(OptionsPaths)
	preferNative := search.Options.GetBool(OptionsPreferNativeDriver)

	var cmd string
	var useHybridHL bool

	if hasPaths && len(paths) > 0 && !preferNative {
		// Build hybrid command that checks for hl on remote host
		hybridCmd, err := lc.buildHybridHLCommand(search, paths)
		if err != nil {
			mylog.Warn("failed to build hybrid hl command, falling back to native: %v", err)
		} else {
			cmd = hybridCmd
			useHybridHL = true
			mylog.Debug("using hybrid hl command for SSH: %s", cmd)
		}
	}

	// Fall back to native command if hybrid didn't work
	if cmd == "" {
		var err error
		cmd, err = getCommand(search)
		if err != nil {
			if err.Error() == "cmd is missing for sshLogClient" {
				// Check if we have paths but failed to build hybrid command
				if hasPaths && len(paths) > 0 {
					return nil, errors.New("failed to build hl command and no fallback 'cmd' is configured")
				}
				return nil, fmt.Errorf("configuration error: %w", err)
			}
			return nil, err
		}
		mylog.Debug("using native command for SSH: %s", cmd)
	}

	session, err := lc.conn.NewSession()
	if err != nil {
		return nil, err
	}

	modes := sshc.TerminalModes{
		sshc.ECHO:          0,     // disable echoing
		sshc.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		sshc.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	// Determine whether to disable PTY, with search options overriding client options.
	disablePTY := lc.options.DisablePTY
	if searchDisable, ok := search.Options.GetBoolOk("disablePTY"); ok {
		disablePTY = searchDisable
	}

	// Only request a PTY if it's not disabled.
	if !disablePTY {
		err = session.RequestPty("xterm", 80, 40, modes)
		if err != nil {
			return nil, err
		}
	}

	_, err = session.StdinPipe()
	if err != nil {
		return nil, err
	}

	errOut, err := session.StderrPipe()
	if err != nil {
		return nil, err
	}

	out, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := session.Start(cmd); err != nil {
		return nil, fmt.Errorf("failed to start ssh command: %w", err)
	}

	// Track which engine was used (for hybrid mode)
	var engineUsed string
	errChan := make(chan error, 1)
	go func() {
		defer close(errChan)
		// Read stderr to detect engine marker and capture errors
		stderrScanner := bufio.NewScanner(errOut)
		var stderrOutput bytes.Buffer
		for stderrScanner.Scan() {
			line := stderrScanner.Text()
			// Check for engine marker
			if strings.HasPrefix(line, "HL_ENGINE=") {
				engineUsed = strings.TrimPrefix(line, "HL_ENGINE=")
				mylog.Debug("remote engine detected: %s", engineUsed)
				continue
			}
			stderrOutput.WriteString(line)
			stderrOutput.WriteString("\n")
		}
		if err := session.Wait(); err != nil {
			if stderrOutput.Len() > 0 {
				errChan <- fmt.Errorf("ssh command failed: %w (remote output: %s)", err, stderrOutput.String())
			} else {
				errChan <- fmt.Errorf("ssh command failed: %w", err)
			}
		}
	}()

	scanner := bufio.NewScanner(out)

	// For hybrid mode, mark it for debugging/metrics purposes.
	// Note: We do NOT skip client-side filtering based on this flag because
	// we can't know if hl actually ran on the remote until after all output is read.
	// The reader will always apply filtering for SSH hybrid mode to ensure correctness.
	searchToUse := search
	if useHybridHL {
		preFilteredSearch := *search
		if preFilteredSearch.Options == nil {
			preFilteredSearch.Options = make(map[string]interface{})
		}
		// Mark as hybrid mode for debugging (not used to skip filtering)
		preFilteredSearch.Options["__hybridHL__"] = true
		searchToUse = &preFilteredSearch
	}

	result, err := reader.GetLogResult(searchToUse, scanner, session)
	if err != nil {
		return nil, err
	}
	result.ErrChan = errChan

	// Store engine info for debugging/metrics
	_ = engineUsed // Available for future use (metrics, logging)

	return result, nil
}

// buildHybridHLCommand creates a shell command that:
// 1. Checks if hl is available on the remote host
// 2. Uses hl with filters if available (server-side filtering)
// 3. Falls back to cat/tail if hl is not available (client-side filtering)
func (lc sshLogClient) buildHybridHLCommand(search *client.LogSearch, paths []string) (string, error) {
	// Build hl arguments (paths are passed separately to BuildSSHCommand, not included here)
	hlArgs, err := hl.BuildArgs(search, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build hl arguments: %w", err)
	}

	// Build fallback command
	var fallbackCmd string
	if fallback, err := getCommand(search); err == nil && fallback != "" {
		fallbackCmd = fallback
	} else if search.Follow {
		// For follow mode, use tail -f as fallback
		fallbackCmd = "" // hl.BuildFollowSSHCommand will handle this
	}

	// Extract size limit
	sizeLimit := 0
	if search.Size.Set && search.Size.Value > 0 {
		sizeLimit = search.Size.Value
	}

	// Use the SSH builder with marker for engine detection
	var cmd string
	if search.Follow {
		cmd = hl.BuildSSHCommandWithMarker(hlArgs, paths, fallbackCmd, sizeLimit)
	} else {
		if fallbackCmd == "" {
			// Default fallback: cat the files
			var catParts []string
			catParts = append(catParts, "cat")
			for _, p := range paths {
				catParts = append(catParts, hl.ArgsToString([]string{p}))
			}
			fallbackCmd = strings.Join(catParts, " ")
		}
		cmd = hl.BuildSSHCommandWithMarker(hlArgs, paths, fallbackCmd, sizeLimit)
	}

	return cmd, nil
}

func (lc sshLogClient) GetFieldValues(ctx context.Context, search *client.LogSearch, fields []string) (map[string][]string, error) {
	// For SSH/text-based backends, we need to run a search and extract field values
	result, err := lc.Get(ctx, search)
	if err != nil {
		return nil, err
	}
	return client.GetFieldValuesFromResult(ctx, result, fields)
}

// GetLogClient returns a new SSH log client.
func GetLogClient(options LogClientOptions) (client.LogBackend, error) {

	if options.Addr == "" {
		return nil, errors.New("ssh address (addr) is empty")
	}
	if options.User == "" {
		return nil, errors.New("ssh user (user) is empty")
	}

	var privateKeyFile string
	if options.PrivateKey != "" {
		privateKeyFile = options.PrivateKey
	} else {
		privateKeyFile = filepath.Join(homedir.HomeDir(), ".ssh", "id_rsa")
	}

	key, err := os.ReadFile(privateKeyFile) //nolint:gosec
	if err != nil {
		return nil, err
	}
	signer, err := sshc.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}

	sshConfig := &sshc.ClientConfig{
		User: options.User,
		Auth: []sshc.AuthMethod{
			sshc.PublicKeys(signer),
		},
		HostKeyCallback: sshc.HostKeyCallback(
			func(_ string, _ net.Addr, _ sshc.PublicKey) error {
				return nil
			}),
	}

	conn, err := sshc.Dial("tcp", options.Addr, sshConfig)
	if err != nil {
		return nil, err
	}

	return sshLogClient{conn: conn, options: options}, nil
}
