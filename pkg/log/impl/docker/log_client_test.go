package docker

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
)

type MockDockerAPI struct {
	OnContainerList func(options container.ListOptions) ([]types.Container, error)
	OnContainerLogs func(container string, options container.LogsOptions) (io.ReadCloser, error)
}

func (m *MockDockerAPI) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	if m.OnContainerList != nil {
		return m.OnContainerList(options)
	}
	return nil, nil
}

func (m *MockDockerAPI) ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error) {
	if m.OnContainerLogs != nil {
		return m.OnContainerLogs(container, options)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func TestLogClient_Get_ContainerID(t *testing.T) {
	mockAPI := &MockDockerAPI{
		OnContainerLogs: func(containerID string, options container.LogsOptions) (io.ReadCloser, error) {
			assert.Equal(t, "my-container-id", containerID)
			// Return a simple log line
			return io.NopCloser(strings.NewReader("2023-01-01T12:00:00Z Hello Docker\n")), nil
		},
	}

	lc := LogClient{
		apiClient: mockAPI,
	}

	search := &client.LogSearch{
		Options: ty.MI{
			"container":  "my-container-id",
			"showStdout": true,
			"showStderr": false, // Avoid stdcopy for simplicity
		},
	}

	result, err := lc.Get(context.Background(), search)
	assert.NoError(t, err)

	entries, _, err := result.GetEntries(context.Background())
	assert.NoError(t, err)
		assert.Len(t, entries, 1)
		assert.Equal(t, " Hello Docker", entries[0].Message)
	}
	
	func TestLogClient_Get_ServiceDiscovery(t *testing.T) {
		mockAPI := &MockDockerAPI{
			OnContainerList: func(options container.ListOptions) ([]types.Container, error) {
				// Verify filters?
				return []types.Container{
					{ID: "discovered-id", Labels: map[string]string{"com.docker.compose.service": "my-service"}},
				}, nil
			},
			OnContainerLogs: func(containerID string, options container.LogsOptions) (io.ReadCloser, error) {
				assert.Equal(t, "discovered-id", containerID)
				return io.NopCloser(strings.NewReader("2023-01-01T12:00:00Z Service Log\n")), nil
			},
		}
	
		lc := LogClient{apiClient: mockAPI}
	
		search := &client.LogSearch{
			Options: ty.MI{
				"service":    "my-service",
				"showStdout": true,
				"showStderr": false,
			},
		}
	
		result, err := lc.Get(context.Background(), search)
		assert.NoError(t, err)
	
		entries, _, _ := result.GetEntries(context.Background())
			assert.Len(t, entries, 1)
			assert.Equal(t, " Service Log", entries[0].Message)
		}
		
		func TestLogClient_GetFieldValues(t *testing.T) {
			mockAPI := &MockDockerAPI{
				OnContainerLogs: func(containerID string, options container.LogsOptions) (io.ReadCloser, error) {
					// JSON log line with a field
					// reader.GetLogResult handles JSON extraction if configured?
					// The default reader logic parses the line.
					// ExtractJSONFromEntry is usually called by the Printer or SearchResult.GetFieldValuesFromResult.
					// GetFieldValuesFromResult (in log/client/mod.go) calls ExtractJSONFromEntry.
					return io.NopCloser(strings.NewReader(`2023-01-01T12:00:00Z {"level":"INFO", "app":"myapp"}` + "\n")), nil
				},
			}
		
			lc := LogClient{apiClient: mockAPI}
		
				search := &client.LogSearch{
					Options: ty.MI{
						"container":  "my-container",
						"showStdout": true,
						"showStderr": false,
					},
					FieldExtraction: client.FieldExtraction{
						JSON: ty.OptWrap(true), // Enable JSON extraction
					},
				}		
			values, err := lc.GetFieldValues(context.Background(), search, []string{"level", "app"})
			assert.NoError(t, err)
			assert.Contains(t, values, "level")
			assert.Contains(t, values["level"], "INFO")
			assert.Contains(t, values, "app")
			assert.Contains(t, values["app"], "myapp")
		}
		