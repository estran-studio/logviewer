# logviewer

<div align="center">

| Build & Quality | Testing & Coverage | Security |
|:---------------:|:-----------------:|:--------:|
| [![Go Report Card](https://goreportcard.com/badge/github.com/estran-studio/logviewer)](https://goreportcard.com/report/github.com/estran-studio/logviewer) | [![codecov](https://codecov.io/gh/estran-studio/logviewer/branch/main/graph/badge.svg)](https://codecov.io/gh/estran-studio/logviewer) | [![GitHub CodeQL](https://github.com/estran-studio/logviewer/actions/workflows/codeql.yaml/badge.svg)](https://github.com/estran-studio/logviewer/actions/workflows/codeql.yaml) |
| [![Build Status](https://github.com/estran-studio/logviewer/actions/workflows/main.yaml/badge.svg)](https://github.com/estran-studio/logviewer/actions/workflows/main.yaml) | | [![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/estran-studio/logviewer/badge)](https://securityscorecards.dev/viewer/?uri=github.com/estran-studio/logviewer) |
| [![Go Version](https://img.shields.io/github/go-mod/go-version/estran-studio/logviewer)](go.mod) | | [![Dependency Review](https://github.com/estran-studio/logviewer/actions/workflows/dependency-review.yml/badge.svg)](https://github.com/estran-studio/logviewer/actions/workflows/dependency-review.yml) |

| Documentation | Release | License |
|:-------------:|:-------:|:-------:|
| [![Go Reference](https://pkg.go.dev/badge/github.com/estran-studio/logviewer.svg)](https://pkg.go.dev/github.com/estran-studio/logviewer) | [![Release](https://img.shields.io/github/v/release/estran-studio/logviewer)](https://github.com/estran-studio/logviewer/releases/latest) | [![License](https://img.shields.io/github/license/estran-studio/logviewer)](LICENSE) |

</div>

<p align="center">
  <img src="https://raw.githubusercontent.com/estran-studio/logviewer/main/logo.svg" alt="logviewer logo" width="120" />
  <br>
  <strong>One CLI to query all your logs</strong>
  <br>
  <em>Kubernetes • Docker • Splunk • OpenSearch • CloudWatch • SSH</em>
</p>
---

LogViewer is a unified CLI tool for querying logs from multiple sources with consistent syntax. Stop juggling different tools and query languages—learn once, use everywhere.

![demo](https://raw.githubusercontent.com/estran-studio/logviewer/main/demo.gif)

## Features

- **Multi-source support** — Query Kubernetes, Docker, Splunk, OpenSearch, CloudWatch, and SSH with one tool
- **Unified query syntax** — Same commands work across all backends
- **Field extraction** — Turn unstructured logs into searchable fields using regex
- **Custom templates** — Format output for humans or pipe to other tools
- **Config-driven** — Save complex queries as reusable contexts
- **Multi-context search** — Query multiple environments simultaneously
- **Shell autocomplete** — Tab completion for contexts, fields, and more
- **AI integration** — Use as an MCP server with Claude, Copilot, or Gemini
- **High-performance filtering** — Optional [hl](https://github.com/pamburus/hl) integration for fast local/SSH log processing

## Quick Start

### 1. Install

**Homebrew (macOS & Linux)**
```bash
brew tap estran-studio/tap
brew install logviewer
```

**Scoop (Windows)**
```powershell
scoop bucket add estran-studio https://github.com/estran-studio/scoop-bucket
scoop install logviewer
```

**Quick Install Script**
```bash
curl -L "https://github.com/estran-studio/logviewer/releases/latest/download/logviewer-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m | sed 's/x86_64/amd64/')" -o ./logviewer && chmod +x ./logviewer
sudo mv ./logviewer /usr/local/bin/
```

See [Installation](https://github.com/estran-studio/logviewer/wiki/Installation) for more options (Docker, AUR, build from source).

### 2. Configure

Run the interactive wizard:

```bash
logviewer configure
```

Or create `~/.logviewer/config.yaml` manually:

```yaml
clients:
  my-k8s:
    type: k8s

contexts:
  app-logs:
    client: my-k8s
    search:
      options:
        namespace: production
        pod: my-app-*
```

### 3. Query

```bash
# Query logs from your context
logviewer -i app-logs --last 10m query log

# Filter by fields
logviewer -i app-logs -f level=ERROR query log

# Discover available fields
logviewer -i app-logs query field
```

## Use Cases

### Debug across environments
```bash
# Query dev, staging, and prod simultaneously
logviewer -i app-dev -i app-staging -i app-prod --last 30m -f level=ERROR query log
```

### Follow distributed transactions
```bash
# Filter by trace ID across services
logviewer -i api-gateway -i payment-service --last 1h -f traceId=abc-123 query log
```

### Real-time monitoring
```bash
# Tail logs with auto-refresh
logviewer -i app-logs --refresh 2s query log
```

### Custom output formatting
```bash
# Use Go templates
logviewer -i app-logs --format "[{{.Timestamp.Format \"15:04:05\"}}] {{.Level}}: {{.Message}}" query log
```

### Interactive TUI (Alpha)
```bash
# Launch the interactive Text User Interface
logviewer tui -i context
```
> **Note:** The TUI is currently in **Alpha**. See [TUI Documentation](https://github.com/estran-studio/logviewer/wiki/TUI-Mode-(Alpha)) for details.

### AI-powered investigation
```bash
# Start MCP server for AI agents
logviewer mcp --config ~/.logviewer/config.yaml
```

Then ask Claude, Copilot, or Gemini: *"Find all payment errors in the last hour"*

## Supported Backends

| Backend | Type | Native Query | Notes |
|---------|------|--------------|-------|
| Kubernetes | `k8s` | — | |
| Docker | `docker` | — | |
| Local/SSH | `local`, `ssh` | — | [hl](https://github.com/pamburus/hl) support for fast filtering |
| OpenSearch/Elasticsearch | `opensearch` | Lucene | |
| Splunk | `splunk` | SPL | |
| AWS CloudWatch | `cloudwatch` | Insights | |

## Documentation

Full documentation is available in the **[GitHub Wiki](https://github.com/estran-studio/logviewer/wiki)**:

### Getting Started
- [Installation](https://github.com/estran-studio/logviewer/wiki/Installation) — All installation methods
- [CLI Usage](https://github.com/estran-studio/logviewer/wiki/CLI-Usage) — Command reference
- [Configuration](https://github.com/estran-studio/logviewer/wiki/Configuration) — Config file setup

### Features
- [Field Extraction](https://github.com/estran-studio/logviewer/wiki/Field-Extraction) — Parse structured data from logs
- [Templates](https://github.com/estran-studio/logviewer/wiki/Templates) — Custom output formatting
- [Variables](https://github.com/estran-studio/logviewer/wiki/Variables) — Dynamic context parameters
- [Multi-Context Search](https://github.com/estran-studio/logviewer/wiki/Multi-Context-Search) — Query multiple sources

### Backends
- [Backends Reference](https://github.com/estran-studio/logviewer/wiki/Backends) — K8s, Docker, Splunk, OpenSearch, CloudWatch, SSH
- [HL Integration](https://github.com/estran-studio/logviewer/wiki/HL-Integration) — High-performance filtering with hl

### AI Integration
- [MCP Integration](https://github.com/estran-studio/logviewer/wiki/MCP-Integration) — Setup for AI agents
- [LLM Usage Guide](https://github.com/estran-studio/logviewer/wiki/LLM-Usage-Guide) — Best practices for AI

### Help
- [Troubleshooting](https://github.com/estran-studio/logviewer/wiki/Troubleshooting) — Common issues
- [FAQ](https://github.com/estran-studio/logviewer/wiki/FAQ) — Frequently asked questions

## Example Configuration

```yaml
clients:
  prod-splunk:
    type: splunk
    options:
      url: https://splunk.example.com:8089
      token: ${SPLUNK_TOKEN}

  prod-k8s:
    type: k8s
    options:
      kubeConfig: ~/.kube/prod-config

searches:
  json-format:
    fieldExtraction:
      json: true
    printerOptions:
      template: '[{{.Timestamp.Format "15:04:05"}}] {{.Level}} {{.Message}}'

contexts:
  payment-logs:
    description: "Payment service logs in Splunk"
    client: prod-splunk
    searchInherit: ["json-format"]
    search:
      options:
        index: payment-service
        timestampFormat: "2006-01-02 15:04:05" # Optional: custom timestamp format

  api-gateway:
    description: "API Gateway pods in Kubernetes"
    client: prod-k8s
    search:
      options:
        namespace: production
        pod: api-gateway-*
```

## Contributing

Contributions are welcome! Please:

- **Report bugs** via [GitHub Issues](https://github.com/estran-studio/logviewer/issues)
- **Request features** via [GitHub Issues](https://github.com/estran-studio/logviewer/issues)
- **Ask questions** in [GitHub Discussions](https://github.com/estran-studio/logviewer/discussions)
- **Submit PRs** for bug fixes or new features

## License

This project is licensed under the **GNU General Public License v3.0** — see the [LICENSE](LICENSE) file for details.

---

<p align="center">
  <sub>Made with ❤️ for DevOps engineers tired of juggling log tools</sub>
</p>
