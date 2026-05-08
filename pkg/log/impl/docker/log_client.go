// Package docker provides a Docker-backed log client implementation.
package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/docker/cli/cli/connhelper"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"sync"

	logclient "github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/log/reader"
)

const regexDockerTimestamp = "(([0-9]*)-([0-9]*)-([0-9]*)T([0-9]*):([0-9]*):([0-9]*).([0-9]*)Z)"
const dockerPingTimeout = 10 * time.Second

// DockerAPI defines the subset of the Docker client interface used by this package.
type DockerAPI interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
}

// LogClient implements the client.LogBackend interface for Docker.
type LogClient struct {
	apiClient DockerAPI
	host      string
}

// Get executes a search against Docker logs.
func (lc LogClient) Get(ctx context.Context, search *logclient.LogSearch) (logclient.LogSearchResult, error) {

	if !search.FieldExtraction.TimestampRegex.Set {
		search.FieldExtraction.TimestampRegex.S(regexDockerTimestamp)
	}

	// Specify the container ID or name
	containerID := search.Options.GetString("container")

	// Check if service is provided for service discovery
	if service := search.Options.GetString("service"); service != "" {
		// Use service discovery
		filterArgs := filters.NewArgs()
		filterArgs.Add("label", fmt.Sprintf("com.docker.compose.service=%s", service))

		// Optional project filter
		if project := search.Options.GetString("project"); project != "" {
			filterArgs.Add("label", fmt.Sprintf("com.docker.compose.project=%s", project))
		}

		containers, err := lc.apiClient.ContainerList(ctx, container.ListOptions{
			Filters: filterArgs,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list containers for service %s: %w", service, err)
		}

		if len(containers) == 0 {
			return nil, fmt.Errorf("no running containers found for service %s", service)
		}

		// Use MultiLogSearchResult to merge logs from all containers when multiple replicas exist
		if len(containers) > 1 {
			multiResult, err := logclient.NewMultiLogSearchResult(search)
			if err != nil {
				return nil, err
			}

			var wg sync.WaitGroup
			for _, container := range containers {
				wg.Add(1)
				go func(cID string) {
					defer wg.Done()
					// Clone the search object to prevent race conditions
					containerSearch := search.Clone()

					// Configure for specific container
					containerSearch.Options["container"] = cID
					delete(containerSearch.Options, "service")
					containerSearch.Options["__context_id__"] = cID[:12]

					// Fetch logs
					result, err := lc.Get(ctx, containerSearch)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error fetching logs for container %s: %v\n", cID[:12], err)
					}
					multiResult.Add(result, err)
				}(container.ID)
			}
			wg.Wait()
			return multiResult, nil
		}

		// Use the first matching container
		containerID = containers[0].ID
	}

	var since, until string

	if search.Range.Last.Value != "" {
		since = search.Range.Last.Value
	} else {
		if search.Range.Gte.Value != "" {
			since = search.Range.Gte.Value
		}

		if search.Range.Lte.Value != "" {
			until = search.Range.Lte.Value
		}
	}

	tail := "all"

	if search.Size.Set {
		tail = fmt.Sprintf("%d", search.Size.Value)
	}

	follow := search.Follow

	showStdout := search.Options.GetOr("showStdout", true).(bool)
	showStderr := search.Options.GetOr("showStderr", true).(bool)

	options := container.LogsOptions{
		ShowStdout: showStdout,
		ShowStderr: showStderr,
		Timestamps: search.Options.GetOr("timestamps", true).(bool),
		Details:    search.Options.GetOr("details", false).(bool),
		Since:      since,
		Until:      until,
		Follow:     follow,
		Tail:       tail,
	}
	out, err := lc.apiClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, err
	}

	// Docker uses a multiplexed stream format when both stdout and stderr are requested.
	// We need to demultiplex it using stdcopy.StdCopy to get clean log output.
	var logReader io.Reader
	var closer io.Closer
	if showStdout && showStderr {
		// Both streams - need to demultiplex
		pr, pw := io.Pipe()
		go func() {
			// StdCopy demultiplexes the stream, writing stdout and stderr to the same destination
			_, err := stdcopy.StdCopy(pw, pw, out)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error demultiplexing docker log stream: %v\n", err)
			}
			_ = pw.CloseWithError(err)
			_ = out.Close()
		}()
		logReader = pr
		closer = pr
	} else {
		// Single stream - no demultiplexing needed
		logReader = out
		closer = out
	}

	scanner := bufio.NewScanner(logReader)

	return reader.GetLogResult(search, scanner, closer)
}

// GetFieldValues retrieves distinct values for the specified fields.
func (lc LogClient) GetFieldValues(ctx context.Context, search *logclient.LogSearch, fields []string) (map[string][]string, error) {
	// For docker/text-based backends, we need to run a search and extract field values
	result, err := lc.Get(ctx, search)
	if err != nil {
		return nil, err
	}
	return logclient.GetFieldValuesFromResult(ctx, result, fields)
}

// GetLogClient returns a new Docker log client.
func GetLogClient(host string) (logclient.LogBackend, error) {
	// Prepare basic options
	opts := []client.Opt{
		client.FromEnv,
		client.WithHost(host),
	}

	// Try to get a connection helper (e.g., for ssh://)
	helper, err := connhelper.GetConnectionHelper(host)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection helper: %w", err)
	}

	// If a helper is found (SSH case), inject its DialContext
	// This allows using the system ssh binary and .ssh/config file
	if helper != nil {
		opts = append(opts, client.WithDialContext(helper.Dialer))
	}

	// Always add API version negotiation last, after all connection options are set
	opts = append(opts, client.WithAPIVersionNegotiation())

	apiClient, err := client.NewClientWithOpts(opts...)
	if err != nil {
		// It is preferable to return the error rather than to panic
		return nil, err
	}

	// Attempt to negotiate API version by pinging the server
	// This helps ensure compatibility, especially with older Docker daemons
	ctx, cancel := context.WithTimeout(context.Background(), dockerPingTimeout)
	defer cancel()

	if _, err := apiClient.Ping(ctx); err != nil {
		// For SSH connections, provide helpful diagnostic information
		if helper != nil {
			sshHost := host
			sshHost = strings.TrimPrefix(sshHost, "ssh://")
			return nil, fmt.Errorf("failed to connect to docker daemon via SSH: %w\n\nTroubleshooting:\n"+
				"1. Ensure Docker is installed on the remote host (version 18.09 or later required for SSH)\n"+
				"2. Verify SSH connection works: ssh %s docker version\n"+
				"3. Check that your user has permission to access Docker on the remote host\n"+
				"4. If using docker context, verify it's configured: docker context ls", err, sshHost)
		}
		return nil, fmt.Errorf("failed to connect to docker daemon: %w", err)
	}

	return LogClient{
		apiClient: apiClient,
		host:      host,
	}, nil
}
