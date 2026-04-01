# kubectl-monitor

A CLI tool for enhanced Kubernetes pod monitoring — real-time status, resource usage, filtering, sorting, and interactive mode.

**Version:** 2.2.0 | **Language:** Go 1.23.2 | **License:** MIT

---

## Features

- **Namespace filtering** — monitor a specific namespace or all at once
- **Resource metrics** — CPU (millicores) and memory (Mi/Gi) per pod
- **Watch mode** — auto-refresh at configurable intervals
- **Interactive mode** — menu-driven interface with arrow-key navigation
- **Label selector filtering** — e.g. `app=nginx,env=prod`
- **Problems filter** — show only pods with issues (non-Running or restarts > 0)
- **Sorting** — by CPU, memory, restarts, age, or name
- **Multiple output formats** — table (default), JSON, CSV
- **Accessibility** — `--no-color` flag, high-contrast colors, status symbols
- **Pod log viewer** — view logs directly from interactive mode

---

## Installation

### Prerequisites

- `kubectl` configured and working

### One-line Install

```bash
git clone https://github.com/renancavalcantercb/kubectl-monitor.git && \
cd kubectl-monitor && \
chmod +x install.sh && \
./install.sh && cd .. && rm -rf kubectl-monitor
```

Installs the binary to `/usr/local/bin/kubectl-monitor`.

### Manual Uninstall

```bash
sudo rm /usr/local/bin/kubectl-monitor
```

---

## Usage

```
kubectl-monitor [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--namespace <ns>` | all | Filter pods by namespace |
| `--label <selector>` | — | Label selector (e.g. `app=nginx,env=prod`) |
| `--watch` | false | Enable auto-refresh watch mode |
| `--refresh <duration>` | `5s` | Refresh interval for watch mode |
| `--interactive` | false | Start interactive menu mode |
| `--problems` | false | Show only pods with issues |
| `--sort <field>` | — | Sort by: `cpu`, `memory`, `restarts`, `age`, `name` |
| `--format <fmt>` | `table` | Output format: `table`, `json`, `csv` |
| `--no-color` | false | Disable colored output |
| `--quiet` | false | Suppress informational messages |
| `--verbose` | false | Show detailed information |
| `-v, --version` | — | Show version |
| `-h, --help` | — | Show help |

### Examples

```bash
# All pods across all namespaces
kubectl-monitor

# Monitor a specific namespace
kubectl-monitor --namespace production

# Watch mode with 10s refresh
kubectl-monitor --watch --refresh 10s

# Show only problematic pods, sorted by restarts
kubectl-monitor --problems --sort restarts

# Filter by label
kubectl-monitor --label "app=nginx,env=prod"

# Interactive menu mode
kubectl-monitor --interactive

# JSON output
kubectl-monitor --format json

# Export to CSV
kubectl-monitor --format csv > pods.csv

# Accessible output (no color, verbose)
kubectl-monitor --no-color --verbose
```

### Example Output (table)

```
NAMESPACE    NAME                       CPU     MEMORY   STATUS    RESTARTS   AGE
default      example-pod-1234abcd      100m    150Mi    ✓ Running  1         5d 2h
kube-system  kube-proxy-5678efgh        50m    100Mi    ✓ Running  0         10d 4h
default      broken-pod-9999xxxx         0m      0Mi    ✗ Error    5         1h 30m
```

---

## Build from Source

```bash
git clone https://github.com/renancavalcantercb/kubectl-monitor.git
cd kubectl-monitor
go build -o kubectl-monitor .
```

### Cross-platform Builds

```bash
chmod +x build.sh
./build.sh
```

Outputs binaries to `bin/`:

| File | Platform |
|------|----------|
| `kubectl-monitor-darwin-amd64` | macOS Intel |
| `kubectl-monitor-darwin-arm64` | macOS Apple Silicon (M1/M2) |
| `kubectl-monitor-linux-amd64` | Linux 64-bit |
| `kubectl-monitor-linux-arm64` | Linux ARM 64-bit |

---

## Interactive Mode

Start with `--interactive` for a menu-driven experience:

1. **Show pods now** — single snapshot
2. **Start watch mode** — begin auto-refresh
3. **Change namespace** — select from discovered namespaces
4. **Toggle colors** — enable/disable ANSI colors
5. **Settings** — modify refresh rate, verbose/quiet mode
6. **View pod logs** — select a pod and inspect its logs
7. **Quit**

---

## Architecture

Single-binary Go application (`main.go`, ~1500 lines):

- `KubectlRunner` interface — pluggable kubectl execution (testable)
- Concurrent `kubectl get pods` + `kubectl top pods` via goroutines
- Context-based graceful shutdown (SIGINT/SIGTERM)
- Thread-safe progress indicator with animated spinner
- 30-second timeout per kubectl command

**Dependencies:**
- [`github.com/olekukonko/tablewriter`](https://github.com/olekukonko/tablewriter) — table rendering
