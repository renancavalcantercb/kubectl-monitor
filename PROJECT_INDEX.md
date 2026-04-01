# Project Index: kubectl-monitor

Generated: 2026-04-01 | Version: 2.2.0

---

## Project Structure

```
kubectl-monitor/
├── main.go              # Entire application (1,487 lines)
├── go.mod               # Module: github.com/renancavalcantercb/kubectl-monitor
├── go.sum               # Dependency checksums
├── build.sh             # Cross-platform build script
├── install.sh           # Auto-detect arch, install to /usr/local/bin
├── kubectl-monitor      # Committed binary (main branch)
├── README.md            # User documentation
├── DOCS.md              # Project knowledge base
├── PROJECT_INDEX.md     # This file
├── PROJECT_INDEX.json   # Machine-readable index
└── bin/
    ├── kubectl-monitor-darwin-amd64
    ├── kubectl-monitor-darwin-arm64
    ├── kubectl-monitor-linux-amd64
    └── kubectl-monitor-linux-arm64
```

---

## Entry Points

| Path | Description |
|------|-------------|
| `main.go:434` | `main()` — parses args, creates Monitor, dispatches execution mode |
| `main.go:460` | `parseArguments()` — validates all CLI flags → `*Config` |
| `main.go:345` | `NewMonitor(config)` — sets up context + signal handling |

---

## Core Types (`main.go`)

| Type | Line | Purpose |
|------|------|---------|
| `Config` | 103 | All runtime settings (namespace, flags, format, sort) |
| `PodData` | 121 | Single pod: name, status, restarts, age, CPU/memory (raw + parsed) |
| `PodUsage` | 90 | CPU + memory strings from `kubectl top` |
| `CommandResult` | 96 | Output + error + type ("get"/"top") from a kubectl run |
| `Monitor` | 226 | Orchestrates monitoring; holds runner, context, progress, color |
| `KubectlRunner` | 213 | Interface: `Run(ctx, args...) (string, error)` |
| `DefaultKubectlRunner` | 218 | Real implementation (30s timeout per call) |
| `ProgressIndicator` | 236 | Thread-safe animated spinner |
| `ColorManager` | 306 | ANSI color selection by pod status |

---

## Key Functions (`main.go`)

### Execution Modes
| Function | Line | Description |
|----------|------|-------------|
| `main()` | 434 | Entry; routes to once/watch/interactive |
| `runInteractiveMode()` | 742 | Menu-driven UI with arrow-key nav |

### kubectl Execution
| Function | Line | Description |
|----------|------|-------------|
| `runCommand()` | 710 | Runs any shell command, returns stdout |
| `DefaultKubectlRunner.Run()` | ~218 | kubectl with 30s context timeout |

### Data Processing
| Function | Line | Description |
|----------|------|-------------|
| `parsePods()` | 1184 | Merges `kubectl get` + `kubectl top` → `[]PodData` |
| `parseTopPods()` | 1084 | Extracts CPU/memory map from top output |
| `sortPods()` | 1303 | In-place sort: cpu/memory/restarts/age/name |
| `parseCPU()` | 1259 | "100m" → int64 millicores |
| `parseMemory()` | 1274 | "128Mi"/"1Gi" → int64 bytes |
| `formatAge()` | 1120 | RFC3339 → "5d 2h 10m" |

### Rendering
| Function | Line | Description |
|----------|------|-------------|
| `renderTable()` | 1381 | Entry for table rendering |
| `renderTableFormat()` | 1415 | tablewriter table with colors/borders |
| `renderJSON()` | 1327 | JSON-encoded pod array |
| `renderCSV()` | 1334 | CSV with adaptive headers |
| `formatStatusWithAccessibility()` | 1161 | Adds ✓/⌛/✗/? symbols + ANSI color |
| `setupTableFormat()` | 1458 | Configures tablewriter (borders, alignment) |

### Interactive Helpers
| Function | Line | Description |
|----------|------|-------------|
| `printInteractiveMenu()` | 822 | Renders menu with current config state |
| `promptForNamespace()` | 876 | Discovers + presents available namespaces |
| `showSettings()` | 919 | Inline settings editor (refresh, verbose/quiet) |
| `viewPodLogs()` | 971 | Select pod → choose lines → fetch logs |
| `listPodsForSelection()` | 1044 | Pod list for log viewer selection |
| `showPodLogs()` | 1066 | Runs `kubectl logs --tail=N` |

---

## CLI Flags

| Flag | Default | Values |
|------|---------|--------|
| `--namespace` | `""` (all) | any namespace name |
| `--label` | `""` | e.g. `app=nginx,env=prod` |
| `--watch` | false | — |
| `--refresh` | `5s` | any duration |
| `--interactive` | false | — |
| `--problems` | false | — |
| `--sort` | `""` | `cpu` `memory` `restarts` `age` `name` |
| `--format` | `table` | `table` `json` `csv` |
| `--no-color` | false | — |
| `--quiet` | false | — |
| `--verbose` | false | — |

---

## Configuration

| File | Purpose |
|------|---------|
| `go.mod` | Module declaration, Go version (1.23.2), direct dependencies |
| `go.sum` | Cryptographic checksums for dependency verification |
| `build.sh` | Compiles all 4 platform targets to `bin/` |
| `install.sh` | Detects OS+arch, copies binary to `/usr/local/bin` |

---

## Documentation

| File | Topic |
|------|-------|
| `README.md` | User-facing: install, flags, examples, interactive mode, build |
| `DOCS.md` | Full knowledge base: architecture, data structures, functions |
| `PROJECT_INDEX.md` | This file — compact session bootstrap index |

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/olekukonko/tablewriter` | v0.0.5 | ASCII table rendering |
| `github.com/mattn/go-runewidth` | v0.0.15 | Unicode char width (indirect) |
| `github.com/rivo/uniseg` | v0.4.7 | Unicode segmentation (indirect) |

All core logic uses Go stdlib only.

---

## Quick Start

```bash
# Install
git clone https://github.com/renancavalcantercb/kubectl-monitor.git
cd kubectl-monitor && ./install.sh

# Run
kubectl-monitor                              # all pods
kubectl-monitor --namespace production       # one namespace
kubectl-monitor --watch --refresh 10s        # watch mode
kubectl-monitor --problems --sort restarts   # triage mode
kubectl-monitor --interactive                # menu mode
kubectl-monitor --format json                # JSON output
```

---

## Concurrency Model

```
runMonitor()
  ├── goroutine: kubectl get pods  ──┐
  └── goroutine: kubectl top pods  ──┴→ chan CommandResult → parsePods()
```

- Both kubectl calls fire concurrently, collected via buffered channel
- Watch loop: `time.NewTicker` + `context.Done()` for graceful stop
- `SIGINT`/`SIGTERM` → context cancel → ticker stops → clean exit

---

*Index size: ~4KB | Full codebase: ~60KB | Savings: ~93%*
