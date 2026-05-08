package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"

	logclient "github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/estran-studio/logviewer/pkg/ty"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockDockerClient struct {
	mock.Mock
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	args := m.Called(ctx, options)
	return args.Get(0).([]types.Container), args.Error(1)
}

func (m *MockDockerClient) ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error) {
	args := m.Called(ctx, container, options)
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockDockerClient) Ping(ctx context.Context) (types.Ping, error) {
	args := m.Called(ctx)
	return args.Get(0).(types.Ping), args.Error(1)
}

func makeLogFrame(msg string) []byte {
	header := make([]byte, 8)
	header[0] = 1 // stdout
	binary.BigEndian.PutUint32(header[4:], uint32(len(msg)))
	return append(header, []byte(msg)...)
}

func TestServiceLogs(t *testing.T) {
	mockClient := new(MockDockerClient)
	lc := LogClient{
		apiClient: mockClient,
		host:      "local",
	}

	ctx := context.Background()
	search := &logclient.LogSearch{
		Options: ty.MI{
			"service": "web-app",
		},
		FieldExtraction: logclient.FieldExtraction{
			TimestampRegex: ty.Opt[string]{},
		},
	}

	// 1. Expect ContainerList call
	mockClient.On("ContainerList", ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
		// Verify filters
		return opts.Filters.ExactMatch("label", "com.docker.compose.service=web-app")
	})).Return([]types.Container{
		{ID: "container_id_1_long", Names: []string{"/web-app-1"}},
		{ID: "container_id_2_long", Names: []string{"/web-app-2"}},
	}, nil)

	// 2. Expect ContainerLogs calls for both containers
	logContent1 := makeLogFrame("2024-01-01T00:00:01.000000000Z log from c1\n")
	logContent2 := makeLogFrame("2024-01-01T00:00:02.000000000Z log from c2\n")

	mockClient.On("ContainerLogs", ctx, "container_id_1_long", mock.Anything).Return(io.NopCloser(bytes.NewReader(logContent1)), nil)
	mockClient.On("ContainerLogs", ctx, "container_id_2_long", mock.Anything).Return(io.NopCloser(bytes.NewReader(logContent2)), nil)

	// Execute
	result, err := lc.Get(ctx, search)
	assert.NoError(t, err)

	// Verify we got a MultiLogSearchResult
	// We can check if it implements specific interface or just check behavior
	
	// Get entries
	entries, _, err := result.GetEntries(ctx)
	assert.NoError(t, err)

	// We should get 2 entries
	assert.Len(t, entries, 2)

	// Sort by timestamp is handled by MultiLogSearchResult
	// Note: MultiLogSearchResult sorts by timestamp.
	// 00:00:01 comes before 00:00:02
	assert.Equal(t, " log from c1", entries[0].Message)
	assert.Equal(t, " log from c2", entries[1].Message)

	// Verify ContextID is set correctly
	// The ContextID is set to the first 12 characters of the container ID
	assert.Equal(t, "container_id", entries[0].ContextID)
	assert.Equal(t, "container_id", entries[1].ContextID)

	mockClient.AssertExpectations(t)
}

func TestServiceLogs_SingleContainer(t *testing.T) {
	mockClient := new(MockDockerClient)
	lc := LogClient{
		apiClient: mockClient,
		host:      "local",
	}

	ctx := context.Background()
	search := &logclient.LogSearch{
		Options: ty.MI{
			"service": "web-app",
		},
		FieldExtraction: logclient.FieldExtraction{
			TimestampRegex: ty.Opt[string]{},
		},
	}

	// 1. Expect ContainerList call returning 1 container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "c1", Names: []string{"/web-app-1"}},
	}, nil)

	// 2. Expect ContainerLogs call for the single container
	logContent := makeLogFrame("2024-01-01T00:00:01.000000000Z single log\n")
	mockClient.On("ContainerLogs", ctx, "c1", mock.Anything).Return(io.NopCloser(bytes.NewReader(logContent)), nil)

	// Execute
	result, err := lc.Get(ctx, search)
	assert.NoError(t, err)

	entries, _, err := result.GetEntries(ctx)
	assert.NoError(t, err)

	assert.Len(t, entries, 1)
	assert.Equal(t, " single log", entries[0].Message)

	mockClient.AssertExpectations(t)
}

func TestServiceLogs_ProjectFilter(t *testing.T) {
	mockClient := new(MockDockerClient)
	lc := LogClient{
		apiClient: mockClient,
		host:      "local",
	}

	ctx := context.Background()
	search := &logclient.LogSearch{
		Options: ty.MI{
			"service": "web-app",
			"project": "my-project",
		},
	}

	mockClient.On("ContainerList", ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
		return opts.Filters.ExactMatch("label", "com.docker.compose.service=web-app") &&
			opts.Filters.ExactMatch("label", "com.docker.compose.project=my-project")
	})).Return([]types.Container{
		{ID: "c1", Names: []string{"/web-app-1"}},
	}, nil)

	logContent := makeLogFrame("log\n")
	mockClient.On("ContainerLogs", ctx, "c1", mock.Anything).Return(io.NopCloser(bytes.NewReader(logContent)), nil)

	_, err := lc.Get(ctx, search)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestServiceLogs_ListError(t *testing.T) {
	mockClient := new(MockDockerClient)
	lc := LogClient{
		apiClient: mockClient,
		host:      "local",
	}

	ctx := context.Background()
	search := &logclient.LogSearch{
		Options: ty.MI{
			"service": "web-app",
		},
	}

	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, assert.AnError)

	_, err := lc.Get(ctx, search)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list containers")
	mockClient.AssertExpectations(t)
}

func TestServiceLogs_NoContainers(t *testing.T) {
	mockClient := new(MockDockerClient)
	lc := LogClient{
		apiClient: mockClient,
		host:      "local",
	}

	ctx := context.Background()
	search := &logclient.LogSearch{
		Options: ty.MI{
			"service": "web-app",
		},
	}

	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, nil)

	_, err := lc.Get(ctx, search)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running containers found")
	mockClient.AssertExpectations(t)
}

func TestServiceLogs_PartialFailure(t *testing.T) {
	mockClient := new(MockDockerClient)
	lc := LogClient{
		apiClient: mockClient,
		host:      "local",
	}

	ctx := context.Background()
	search := &logclient.LogSearch{
		Options: ty.MI{
			"service": "web-app",
		},
		FieldExtraction: logclient.FieldExtraction{
			TimestampRegex: ty.Opt[string]{},
		},
	}

	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "container_id_1", Names: []string{"/web-app-1"}},
		{ID: "container_id_2", Names: []string{"/web-app-2"}},
	}, nil)

	// c1 succeeds
	logContent1 := makeLogFrame("2024-01-01T00:00:01.000000000Z log from c1\n")
	mockClient.On("ContainerLogs", ctx, "container_id_1", mock.Anything).Return(io.NopCloser(bytes.NewReader(logContent1)), nil)
	
	// c2 fails
	mockClient.On("ContainerLogs", ctx, "container_id_2", mock.Anything).Return(io.NopCloser(bytes.NewReader(nil)), assert.AnError)

	result, err := lc.Get(ctx, search)
	assert.NoError(t, err) // Should still succeed partially

	entries, _, err := result.GetEntries(ctx)
	assert.NoError(t, err)

	// Should have logs from c1
	assert.Len(t, entries, 1)
	assert.Equal(t, " log from c1", entries[0].Message)

	mockClient.AssertExpectations(t)
}
