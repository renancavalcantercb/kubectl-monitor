# kubectl-monitor — Project Knowledge Base

> Generated index: 2026-04-01 | Version: 2.2.0

---

## Table of Contents

1. [Project Overview](#1-project-overview)
2. [File Structure](#2-file-structure)
3. [Architecture](#3-architecture)
4. [Core Data Structures](#4-core-data-structures)
5. [CLI Reference](#5-cli-reference)
6. [Key Functions](#6-key-functions)
7. [Output Formats](#7-output-formats)
8. [Interactive Mode](#8-interactive-mode)
9. [Concurrency Model](#9-concurrency-model)
10. [Accessibility](#10-accessibility)
11. [Build & Install](#11-build--install)
12. [Dependencies](#12-dependencies)

---

## 1. Project Overview

**kubectl-monitor** is a single-binary Go CLI that wraps `kubectl` to provide an enhanced pod monitoring experience. It adds real-time watch mode, label/problem filtering, multiple sort options, interactive menus, and accessible color output on top of standard `kubectl get pods` + `kubectl top pods`.

- **Language:** Go 1.23.2
- **Module:** `github.com/renancavalcantercb/kubectl-monitor`
- **Binary size:** minimal (single static binary per platform)
- **Runtime dependency:** `kubectl` (must be configured)

---

## 2. File Structure

```
kubectl-monitor/
├── main.go          # Entire application (~1,487 lines)
├── go.mod           # Module definition
├── go.sum           # Dependency checksums
├── README.md        # User-facing documentation
├── DOCS.md          # This file — project knowledge base
├── build.sh         # Cross-platform build script
├── install.sh       # Detects arch, copies binary to /usr/local/bin
├── kubectl-monitor  # Compiled binary (committed to repo)
└── bin/
    ├── kubectl-monitor-darwin-amd64
    ├── kubectl-monitor-darwin-arm64
    ├── kubectl-monitor-linux-amd64
    └── kubectl-monitor-linux-arm64
```

**All application logic lives in `main.go`.** There are no subpackages.

---

## 3. Architecture

### Execution Paths

```
main()
 ├── parseArguments()     → Config
 └── NewMonitor(config)
      ├── runOnceMode()        (default)
      ├── runWatchMode()       (--watch)
      └── runInteractiveMode() (--interactive)
```

### Core Components

| Component | Type | Responsibility |
|-----------|------|----------------|
| `Monitor` | struct | Orchestrates all monitoring logic |
| `Config` | struct | Holds all parsed CLI flags/settings |
| `KubectlRunner` | interface | Abstracts `kubectl` execution (testable) |
| `DefaultKubectlRunner` | struct | Real `kubectl` implementation |
| `ProgressIndicator` | struct | Thread-safe animated spinner |
| `ColorManager` | struct | Status-based ANSI color selection |

### Signal Handling

`SIGINT` / `SIGTERM` → context cancellation → graceful shutdown of watch loop.

---

## 4. Core Data Structures

### `Config`
```go
type Config struct {
    Namespace     string        // "" means all namespaces
    Watch         bool
    Interactive   bool
    NoColor       bool
    Quiet         bool
    Verbose       bool
    RefreshRate   time.Duration // default: 5s
    LabelSelector string
    ProblemsOnly  bool
    SortBy        string        // cpu|memory|restarts|age|name
    OutputFormat  string        // table|json|csv
}
```

### `PodData`
```go
type PodData struct {
    Namespace   string
    Name        string
    Status      string
    Restarts    int
    Age         string
    AgeTime     time.Time
    CPU         string        // formatted, e.g. "100m"
    Memory      string        // formatted, e.g. "150Mi"
    CPUValue    int64         // parsed millicores
    MemoryValue int64         // parsed bytes
}
```

### `CommandResult`
```go
type CommandResult struct {
    Output string
    Error  error
    Type   string  // "get" or "top"
}
```

---

## 5. CLI Reference

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--namespace` | string | `""` (all) | Namespace to monitor |
| `--label` | string | `""` | Label selector filter |
| `--watch` | bool | false | Auto-refresh mode |
| `--refresh` | duration | `5s` | Watch refresh interval |
| `--interactive` | bool | false | Interactive menu |
| `--problems` | bool | false | Show only pods with issues |
| `--sort` | string | `""` | Sort field: cpu/memory/restarts/age/name |
| `--format` | string | `table` | Output format: table/json/csv |
| `--no-color` | bool | false | Disable ANSI colors |
| `--quiet` | bool | false | Suppress info messages |
| `--verbose` | bool | false | Show extra detail |
| `--version` / `-v` | — | — | Print version and exit |
| `--help` / `-h` | — | — | Print usage and exit |

### Problems Filter Logic

A pod is considered "problematic" if:
- Status is not `Running`, **OR**
- Restart count > 0

---

## 6. Key Functions

### Parsing & Setup

| Function | Location | Purpose |
|----------|----------|---------|
| `parseArguments()` | main.go | Validates all CLI flags → `Config` |
| `NewMonitor(config)` | main.go | Creates Monitor with context + signal handler |

### kubectl Execution

| Function | Purpose |
|----------|---------|
| `runKubectlCommands()` | Runs `kubectl get` and `kubectl top` concurrently via goroutines |
| `DefaultKubectlRunner.Run()` | Executes a kubectl command with 30s timeout |

### Data Processing

| Function | Purpose |
|----------|---------|
| `parsePods(getOutput, topOutput)` | Merges get+top output into `[]PodData` |
| `parseTopPods(topOutput)` | Extracts CPU/memory map from `kubectl top` |
| `filterPods(pods)` | Applies label + problems filters |
| `sortPods(pods, sortBy)` | Sorts slice in-place by specified field |
| `formatAge(timestamp)` | RFC3339 → human-readable (e.g. "5d 2h") |
| `parseCPU(s)` | "100m" → int64 millicores |
| `parseMemory(s)` | "128Mi"/"1Gi" → int64 bytes |

### Rendering

| Function | Purpose |
|----------|---------|
| `renderTable(pods, config)` | Outputs formatted ASCII table via tablewriter |
| `renderJSON(pods)` | JSON-encoded output with indentation |
| `renderCSV(pods, config)` | CSV with headers adapted to namespace mode |
| `formatStatusWithAccessibility(status, noColor)` | Adds symbols (✓ ⌛ ✗ ?) + ANSI color |

### Logging (on `Monitor`)

| Method | Output |
|--------|--------|
| `LogInfo(msg)` | `ℹ msg` |
| `LogError(msg)` | `✗ msg` (stderr) |
| `LogWarning(msg)` | `⚠ msg` |
| `LogSuccess(msg)` | `✓ msg` |
| `LogVerbose(msg)` | only printed when `--verbose` |

---

## 7. Output Formats

### Table (default)

Uses `github.com/olekukonko/tablewriter`. Columns:
- All namespaces: `NAMESPACE | NAME | CPU | MEMORY | STATUS | RESTARTS | AGE`
- Single namespace: `NAME | CPU | MEMORY | STATUS | RESTARTS | AGE`

### JSON

Array of pod objects. Each object mirrors `PodData` fields.

### CSV

Same columns as table. Suitable for export:
```bash
kubectl-monitor --format csv > pods.csv
```

---

## 8. Interactive Mode

Started with `--interactive`. Menu options:

```
1. Show pods now
2. Start watch mode
3. Change namespace    ← discovers namespaces dynamically
4. Toggle colors
5. Settings            ← modify refresh rate, verbose/quiet inline
6. View pod logs       ← select pod → choose line count → fetch logs
7. Quit
```

Navigation uses arrow keys. All config changes take effect immediately.

---

## 9. Concurrency Model

```
runMonitor()
  │
  ├── go Run("kubectl get pods ...")  ──┐
  └── go Run("kubectl top pods ...")  ──┤→ channel CommandResult
                                        │
  WaitGroup.Wait() ──────────────────────┘
  parsePods(getResult, topResult)
```

- Two goroutines fire concurrently; results collected via buffered channel
- 30-second `context.WithTimeout` per kubectl invocation
- Watch mode uses `time.NewTicker(refreshRate)` + context done channel
- `ProgressIndicator` uses its own goroutine + mutex for thread-safe spinner

---

## 10. Accessibility

- `--no-color` disables all ANSI escape codes
- Status symbols used in addition to text: `✓` Running, `⌛` Pending, `✗` Error/CrashLoop, `?` Unknown
- High-contrast color variants available in `ColorManager`
- Terminal detection prevents escape codes in non-TTY output (e.g. pipes)

---

## 11. Build & Install

### Quick Install
```bash
git clone https://github.com/renancavalcantercb/kubectl-monitor.git && \
cd kubectl-monitor && chmod +x install.sh && ./install.sh && cd .. && rm -rf kubectl-monitor
```

### Build from Source
```bash
go build -o kubectl-monitor .
```

### Cross-platform Build
```bash
./build.sh
# outputs to bin/ for darwin/linux × amd64/arm64
```

### install.sh Logic
1. Detect OS (`uname -s`) and arch (`uname -m`)
2. Normalize arch: `x86_64` → `amd64`, `aarch64|arm64` → `arm64`
3. Copy `bin/kubectl-monitor-<os>-<arch>` → `/usr/local/bin/kubectl-monitor`
4. `chmod +x`

### Uninstall
```bash
sudo rm /usr/local/bin/kubectl-monitor
```

---

## 12. Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/olekukonko/tablewriter` | v0.0.5 | ASCII table rendering |
| `github.com/mattn/go-runewidth` | v0.0.15 | Unicode character width (tablewriter dep) |
| `github.com/rivo/uniseg` | v0.4.7 | Unicode segmentation (tablewriter dep) |

All other functionality uses the Go standard library only.

---

*This document was auto-generated by `/sc:index`. Update manually for significant feature changes.*
