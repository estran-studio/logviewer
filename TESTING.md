# Testing Strategy

This project follows a strict testing philosophy designed to ensure high reliability, speed, and maintainability. We prioritize **behavioral unit testing** using **Test Doubles** (Mocks/Fakes) to isolate core logic from external dependencies.

## Core Principles

1.  **Behavior Over Implementation**
    *   Tests should verify *what* the code does, not *how* it does it.
    *   Always use `package [name]_test` (external tests) to force interaction only via the public API.
    *   If you can't test it from the outside, the API might need better design.

2.  **No Network/IO in Unit Tests**
    *   Unit tests must be fast, deterministic, and runnable in isolation (e.g., without Docker or AWS creds).
    *   Any test that touches the network, file system, or a running container is an **Integration Test**.

3.  **Dependency Isolation (Ports & Adapters)**
    *   Our core logic (CLI, TUI, Query Parser) interacts with "Ports" (Interfaces), not concrete implementations.
    *   We use **Test Doubles** to simulate the "World" (Splunk, K8s, Docker) during testing.

4.  **Table-Driven Tests**
    *   Use Go's idiomatic table-driven approach to cover edge cases efficiently.

---

## Architecture: Ports and Adapters

To facilitate this strategy, the logging subsystem is split into two layers:

### 1. The Port: `LogClient`
*   **Location:** `pkg/log/client/interface.go`
*   **Purpose:** The high-level behavioral contract used by the application (CLI, TUI).
*   **Characteristics:** Simple, synchronous, data-focused (returns `[]LogEntry`).
*   **Testing:** We use `client.MockLogClient` to test components that depend on this interface.

### 2. The Driver: `LogBackend` (formerly LogClient)
*   **Location:** `pkg/log/client/mod.go`
*   **Purpose:** The low-level implementation details for specific systems (Splunk, CloudWatch).
*   **Characteristics:** Complex, asynchronous, streaming (`chan LogEntry`), protocol-specific.
*   **Testing:** These are adapted to the `LogClient` interface via `BackendAdapter`.

---

## How to Write Unit Tests

### 1. Using Mocks
When testing a component that needs to query logs (e.g., a CLI command), inject the `client.LogClient` interface and pass a `client.MockLogClient`.

```go
package cmd_test

import (
    "testing"
    "github.com/estran-studio/logviewer/pkg/log/client"
    "github.com/stretchr/testify/assert"
)

func TestMyCommand(t *testing.T) {
    // 1. Setup the Mock
    mock := &client.MockLogClient{
        OnQuery: func(s client.LogSearch) ([]client.LogEntry, error) {
            // Return deterministic fake data
            return []client.LogEntry{
                {Message: "fake log 1", Level: "INFO"},
            }, nil
        },
    }

    // 2. Execute functionality using the mock
    // (Assuming RunMyCommand takes a LogClient)
    err := RunMyCommand(mock, "some-arg")

    // 3. Verify expectations
    assert.NoError(t, err)
    // You can also inspect mock.LastSearch to verify inputs
    assert.Equal(t, "some-arg", mock.LastSearch.Query)
}
```

### 2. Table-Driven Example
When testing pure logic (like query parsing), use table-driven tests.

```go
func TestParser(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {
            name:  "basic equality",
            input: "level=error",
            want:  "Filter(level==error)",
        },
        {
            name:    "invalid syntax",
            input:   "level=",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Parse(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Parse() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

## Running Tests

Run all unit tests (fast):
```bash
go test ./pkg/... ./cmd/...
```

Check coverage:
```bash
go test -coverprofile=coverage.out ./pkg/...
go tool cover -func=coverage.out
```

Run integration tests (requires Docker):
```bash
make integration/start
make integration/test
make integration/stop
```

---

## Understanding Coverage Metrics

**Coverage metrics are optional.** The primary focus should be on **smoke tests** that verify basic functionality works end-to-end. However, we still add unit tests where practical to catch regressions early.

### Integration Test Focus
Driver implementations (SSH, K8s, Splunk, CloudWatch clients) are primarily validated through **integration and E2E tests** rather than mocked unit tests. This ensures real-world compatibility.

### Smoke Tests Priority
The most important tests are:
- E2E tests in `integration/tests/e2e/` - Verify core workflows work with real backends
- Integration tests - Verify adapters work with actual services
- Unit tests for pure logic - Nice to have for catching regressions quickly

**Focus testing efforts on smoke tests that validate real behavior over achieving high coverage percentages.**
