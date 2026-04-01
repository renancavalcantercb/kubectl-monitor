package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/olekukonko/tablewriter"
	"gopkg.in/yaml.v3"
)

// Constants for magic strings and values
const (
	AllNamespacesFlag = "--all-namespaces"
	NamespaceFlag     = "--namespace"
	WatchFlag         = "--watch"
	NoColorFlag       = "--no-color"
	QuietFlag         = "--quiet"
	VerboseFlag       = "--verbose"
	InteractiveFlag   = "--interactive"
	LabelFlag         = "--label"
	ProblemsFlag      = "--problems"
	SortFlag          = "--sort"
	OutputFormatFlag  = "--format"
	SinceFlag         = "--since"
	NotAvailable      = "N/A"
	MinRequiredFields = 4
	MinPodFields      = 5
	AppVersion        = "2.2.0"
	AppName           = "kubectl-monitor"

	// Command timeouts
	KubectlTimeout   = 30 * time.Second
	RefreshInterval  = 5 * time.Second
	ProgressInterval = 100 * time.Millisecond

	// ANSI color codes - Accessibility friendly
	ColorReset   = "\033[0m"
	ColorGreen   = "\033[32m" // Running status
	ColorYellow  = "\033[33m" // Pending status
	ColorRed     = "\033[31m" // Failed status
	ColorMagenta = "\033[35m" // Unknown status
	ColorCyan    = "\033[36m" // Headers
	ColorBold    = "\033[1m"  // Emphasis
	ColorDim     = "\033[2m"  // Secondary text

	// High contrast colors for accessibility
	ColorHiGreen   = "\033[92m" // High contrast green
	ColorHiYellow  = "\033[93m" // High contrast yellow
	ColorHiRed     = "\033[91m" // High contrast red
	ColorHiMagenta = "\033[95m" // High contrast magenta

	// kubectl commands
	KubectlCmd = "kubectl"
	GetCmd     = "get"
	TopCmd     = "top"
	PodsCmd    = "pods"

	// Custom columns for kubectl get pods
	CustomColumns = "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount,AGE:.metadata.creationTimestamp"

	// Progress and UI symbols
	SpinnerFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	CheckMark     = "✓"
	CrossMark     = "✗"
	InfoMark      = "ℹ"
	WarningMark   = "⚠"
)

// Pod status constants
const (
	StatusRunning = "Running"
	StatusPending = "Pending"
	StatusFailed  = "Failed"
	StatusUnknown = "Unknown"
)

// PodUsage represents CPU and memory usage for a pod
type PodUsage struct {
	CPU    string
	Memory string
}

// CommandResult represents the result of a kubectl command
type CommandResult struct {
	Output string
	Error  error
	Type   string // "get" or "top"
}

// PodChange represents a change detected in a pod between watch refreshes
type PodChange struct {
	Pod           PodData
	OldStatus     string // non-empty when status changed
	RestartsAdded int    // delta in restart count
}

// PodDiff represents changes between two watch refreshes
type PodDiff struct {
	Added   []PodData
	Removed []string // "namespace/name" keys
	Changed []PodChange
}

// HasChanges returns true if there are any changes in the diff
func (d *PodDiff) HasChanges() bool {
	return len(d.Added) > 0 || len(d.Removed) > 0 || len(d.Changed) > 0
}

// FileConfig represents the structure of ~/.kubectl-monitor.yaml
type FileConfig struct {
	Namespace string `yaml:"namespace"`
	Sort      string `yaml:"sort"`
	Format    string `yaml:"format"`
	Refresh   string `yaml:"refresh"`
	NoColor   bool   `yaml:"no_color"`
	Quiet     bool   `yaml:"quiet"`
	Verbose   bool   `yaml:"verbose"`
	Labels    string `yaml:"label"`
	Since     string `yaml:"since"`
}

// Config holds application configuration
type Config struct {
	Namespace     string
	AllNamespaces bool
	Watch         bool
	NoColor       bool
	Quiet         bool
	Verbose       bool
	Interactive   bool
	RefreshRate   time.Duration
	Output        io.Writer
	ErrorOutput   io.Writer
	Labels        string        // Label selector (e.g., "app=nginx,env=prod")
	ProblemsOnly  bool          // Show only pods with problems
	SortBy        string        // Sort by: "cpu", "memory", "restarts", "age", "name"
	OutputFormat  string        // Output format: "table", "json", "csv"
	Since         time.Duration // Show only pods created within this duration (0 = disabled)
}

// PodData represents parsed pod information for sorting and output
type PodData struct {
	Namespace   string    `json:"namespace"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Restarts    int       `json:"restarts"`
	Age         string    `json:"age"`
	AgeTime     time.Time `json:"-"`
	CPU         string    `json:"cpu"`
	Memory      string    `json:"memory"`
	CPUMillis   int64     `json:"-"`
	MemoryBytes int64     `json:"-"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if !c.AllNamespaces && c.Namespace == "" {
		return errors.New("namespace cannot be empty when not using all namespaces")
	}
	if c.AllNamespaces && c.Namespace != AllNamespacesFlag {
		return errors.New("namespace should be '--all-namespaces' when using all namespaces")
	}
	if c.RefreshRate < time.Second {
		c.RefreshRate = RefreshInterval
	}
	if c.Output == nil {
		c.Output = os.Stdout
	}
	if c.ErrorOutput == nil {
		c.ErrorOutput = os.Stderr
	}
	return nil
}

// IsColorEnabled returns true if color output is enabled
func (c *Config) IsColorEnabled() bool {
	return !c.NoColor && isTerminal(c.Output)
}

// LogInfo logs informational messages
func (c *Config) LogInfo(format string, args ...interface{}) {
	if !c.Quiet {
		prefix := InfoMark + " "
		if c.IsColorEnabled() {
			prefix = ColorCyan + InfoMark + ColorReset + " "
		}
		fmt.Fprintf(c.Output, prefix+format+"\n", args...)
	}
}

// LogError logs error messages
func (c *Config) LogError(format string, args ...interface{}) {
	prefix := CrossMark + " Error: "
	if c.IsColorEnabled() {
		prefix = ColorRed + CrossMark + " Error: " + ColorReset
	}
	fmt.Fprintf(c.ErrorOutput, prefix+format+"\n", args...)
}

// LogWarning logs warning messages
func (c *Config) LogWarning(format string, args ...interface{}) {
	if !c.Quiet {
		prefix := WarningMark + " Warning: "
		if c.IsColorEnabled() {
			prefix = ColorYellow + WarningMark + " Warning: " + ColorReset
		}
		fmt.Fprintf(c.Output, prefix+format+"\n", args...)
	}
}

// LogSuccess logs success messages
func (c *Config) LogSuccess(format string, args ...interface{}) {
	if !c.Quiet {
		prefix := CheckMark + " "
		if c.IsColorEnabled() {
			prefix = ColorGreen + CheckMark + " " + ColorReset
		}
		fmt.Fprintf(c.Output, prefix+format+"\n", args...)
	}
}

// LogVerbose logs verbose messages
func (c *Config) LogVerbose(format string, args ...interface{}) {
	if c.Verbose && !c.Quiet {
		prefix := "  "
		if c.IsColorEnabled() {
			prefix = ColorDim + "  " + ColorReset
		}
		fmt.Fprintf(c.Output, prefix+format+"\n", args...)
	}
}

// KubectlRunner interface for executing kubectl commands
type KubectlRunner interface {
	RunCommand(command string, args ...string) (string, error)
}

// DefaultKubectlRunner implements KubectlRunner
type DefaultKubectlRunner struct{}

// RunCommand implements KubectlRunner interface
func (r *DefaultKubectlRunner) RunCommand(command string, args ...string) (string, error) {
	return runCommand(command, args...)
}

// Monitor handles the main monitoring logic
type Monitor struct {
	Runner       KubectlRunner
	Config       *Config
	ctx          context.Context
	cancel       context.CancelFunc
	progress     *ProgressIndicator
	colorizer    *ColorManager
	previousPods map[string]PodData // previous state for diff in watch mode
}

// ProgressIndicator manages progress display
type ProgressIndicator struct {
	config     *Config
	spinnerIdx int
	active     bool
	mutex      sync.Mutex
}

// NewProgressIndicator creates a new progress indicator
func NewProgressIndicator(config *Config) *ProgressIndicator {
	return &ProgressIndicator{
		config: config,
	}
}

// Start begins showing progress
func (p *ProgressIndicator) Start(message string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.config.Quiet {
		return
	}

	p.active = true
	p.spinnerIdx = 0

	go func() {
		for p.active {
			if p.config.IsColorEnabled() {
				fmt.Fprintf(p.config.Output, "\r%s%c%s %s",
					ColorCyan, rune(SpinnerFrames[p.spinnerIdx%len(SpinnerFrames)]), ColorReset, message)
			} else {
				fmt.Fprintf(p.config.Output, "\r[%d] %s", p.spinnerIdx%4, message)
			}
			p.spinnerIdx++
			time.Sleep(ProgressInterval)
		}
	}()
}

// Stop stops the progress indicator
func (p *ProgressIndicator) Stop(success bool, message string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.config.Quiet {
		return
	}

	p.active = false
	time.Sleep(ProgressInterval) // Let spinner complete

	if success {
		if p.config.IsColorEnabled() {
			fmt.Fprintf(p.config.Output, "\r%s%s%s %s\n",
				ColorGreen, CheckMark, ColorReset, message)
		} else {
			fmt.Fprintf(p.config.Output, "\r[OK] %s\n", message)
		}
	} else {
		if p.config.IsColorEnabled() {
			fmt.Fprintf(p.config.Output, "\r%s%s%s %s\n",
				ColorRed, CrossMark, ColorReset, message)
		} else {
			fmt.Fprintf(p.config.Output, "\r[FAIL] %s\n", message)
		}
	}
}

// ColorManager handles color output and accessibility
type ColorManager struct {
	config *Config
}

// NewColorManager creates a new color manager
func NewColorManager(config *Config) *ColorManager {
	return &ColorManager{config: config}
}

// GetStatusColor returns the appropriate color for a pod status
func (c *ColorManager) GetStatusColor(status string) string {
	if !c.config.IsColorEnabled() {
		return ""
	}

	switch status {
	case StatusRunning:
		return ColorHiGreen
	case StatusPending:
		return ColorHiYellow
	case StatusFailed:
		return ColorHiRed
	case StatusUnknown:
		return ColorHiMagenta
	default:
		return ""
	}
}

// ColorizeStatus applies color to a status string
func (c *ColorManager) ColorizeStatus(status string) string {
	color := c.GetStatusColor(status)
	if color == "" {
		return status
	}
	return color + status + ColorReset
}

// NewMonitor creates a new Monitor instance
func NewMonitor(config *Config) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Monitor{
		Runner:    &DefaultKubectlRunner{},
		Config:    config,
		ctx:       ctx,
		cancel:    cancel,
		progress:  NewProgressIndicator(config),
		colorizer: NewColorManager(config),
	}
}

// Close gracefully shuts down the monitor
func (m *Monitor) Close() {
	if m.cancel != nil {
		m.cancel()
	}
}

// Run executes the monitoring process
func (m *Monitor) Run() error {
	if err := m.Config.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %v", err)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		m.Config.LogInfo("Received shutdown signal, cleaning up...")
		m.Close()
	}()

	if m.Config.Watch {
		return m.runWatchMode()
	}

	return m.runOnceMode()
}

// runOnceMode runs the monitor once and exits
func (m *Monitor) runOnceMode() error {
	m.Config.LogVerbose("Running in single execution mode")
	return m.runMonitor()
}

// runWatchMode runs the monitor in watch mode
func (m *Monitor) runWatchMode() error {
	m.Config.LogInfo("Starting watch mode (refresh every %v)", m.Config.RefreshRate)
	m.Config.LogInfo("Press Ctrl+C to stop")

	ticker := time.NewTicker(m.Config.RefreshRate)
	defer ticker.Stop()

	// Run initial execution
	if err := m.runMonitor(); err != nil {
		m.Config.LogError("Initial execution failed: %v", err)
		return err
	}

	for {
		select {
		case <-m.ctx.Done():
			m.Config.LogInfo("Watch mode stopped")
			return nil
		case <-ticker.C:
			m.clearScreen()
			if err := m.runMonitor(); err != nil {
				m.Config.LogError("Watch execution failed: %v", err)
				// Continue watching despite errors
			}
		}
	}
}

// clearScreen clears the terminal screen
func (m *Monitor) clearScreen() {
	if m.Config.IsColorEnabled() {
		fmt.Fprint(m.Config.Output, "\033[2J\033[H")
	} else {
		// Fallback for non-color terminals
		for i := 0; i < 50; i++ {
			fmt.Fprintln(m.Config.Output)
		}
	}
}

func main() {
	config, err := parseArguments()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing arguments: %v\n", err)
		os.Exit(1)
	}

	// Interactive mode setup
	if config.Interactive {
		if err := runInteractiveMode(config); err != nil {
			config.LogError("Interactive mode failed: %v", err)
			os.Exit(1)
		}
		return
	}

	monitor := NewMonitor(config)
	defer monitor.Close()

	if err := monitor.Run(); err != nil {
		config.LogError("Monitor execution failed: %v", err)
		os.Exit(1)
	}
}

// parseArguments parses command line arguments and returns configuration
func parseArguments() (*Config, error) {
	config := &Config{
		Namespace:     AllNamespacesFlag,
		AllNamespaces: true,
		RefreshRate:   RefreshInterval,
		Output:        os.Stdout,
		ErrorOutput:   os.Stderr,
		OutputFormat:  "table",
	}

	// Load config file first (CLI args override file values)
	if fc, err := loadConfigFile(); err != nil {
		return nil, err
	} else if fc != nil {
		if err := applyFileConfig(config, fc); err != nil {
			return nil, err
		}
	}

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case NamespaceFlag:
			if i+1 >= len(args) {
				return nil, errors.New("namespace flag requires a value")
			}
			namespace := strings.TrimSpace(args[i+1])
			if namespace == "" {
				return nil, errors.New("namespace cannot be empty")
			}
			config.Namespace = namespace
			config.AllNamespaces = false
			i++ // Skip the namespace value
		case LabelFlag:
			if i+1 >= len(args) {
				return nil, errors.New("label flag requires a value")
			}
			config.Labels = strings.TrimSpace(args[i+1])
			i++
		case ProblemsFlag:
			config.ProblemsOnly = true
		case SortFlag:
			if i+1 >= len(args) {
				return nil, errors.New("sort flag requires a value (cpu, memory, restarts, age, name)")
			}
			sortBy := strings.ToLower(strings.TrimSpace(args[i+1]))
			validSorts := map[string]bool{"cpu": true, "memory": true, "restarts": true, "age": true, "name": true}
			if !validSorts[sortBy] {
				return nil, fmt.Errorf("invalid sort value: %s (valid: cpu, memory, restarts, age, name)", sortBy)
			}
			config.SortBy = sortBy
			i++
		case OutputFormatFlag:
			if i+1 >= len(args) {
				return nil, errors.New("format flag requires a value (table, json, csv)")
			}
			format := strings.ToLower(strings.TrimSpace(args[i+1]))
			validFormats := map[string]bool{"table": true, "json": true, "csv": true}
			if !validFormats[format] {
				return nil, fmt.Errorf("invalid format value: %s (valid: table, json, csv)", format)
			}
			config.OutputFormat = format
			i++
		case WatchFlag:
			config.Watch = true
		case NoColorFlag:
			config.NoColor = true
		case QuietFlag:
			config.Quiet = true
		case VerboseFlag:
			config.Verbose = true
		case InteractiveFlag:
			config.Interactive = true
		case "--refresh":
			if i+1 >= len(args) {
				return nil, errors.New("refresh flag requires a value (e.g., 5s, 10s)")
			}
			refresh, err := time.ParseDuration(args[i+1])
			if err != nil {
				return nil, fmt.Errorf("invalid refresh duration: %v", err)
			}
			config.RefreshRate = refresh
			i++ // Skip the refresh value
		case SinceFlag:
			if i+1 >= len(args) {
				return nil, errors.New("since flag requires a value (e.g., 10m, 2h)")
			}
			since, err := time.ParseDuration(args[i+1])
			if err != nil {
				return nil, fmt.Errorf("invalid since duration: %v", err)
			}
			config.Since = since
			i++
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		case "-v", "--version":
			printVersion()
			os.Exit(0)
		default:
			return nil, fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	return config, nil
}

// printUsage prints usage information
func printUsage() {
	fmt.Printf(`%s - Enhanced Kubernetes pod monitoring with accessibility features

USAGE:
	%s [flags]

FLAGS:
  Core Options:
	--namespace <namespace>  Filter pods by namespace
	--label <selector>       Filter pods by labels (e.g., "app=nginx,env=prod")
	--watch                  Watch mode with auto-refresh
	--interactive            Interactive mode with menu
	--refresh <duration>     Refresh rate for watch mode (default: %v)

  Filtering & Sorting:
	--problems               Show only pods with problems (non-Running or restarts > 0)
	--sort <field>           Sort output by field: cpu, memory, restarts, age, name
	--since <duration>       Show only pods created within duration (e.g. 10m, 2h, 1d)

  Output Options:
	--format <format>        Output format: table (default), json, csv
	--no-color               Disable colored output (accessibility)
	--quiet                  Suppress informational messages
	--verbose                Show detailed information

  Help:
	-h, --help               Show this help message
	-v, --version            Show version information

EXAMPLES:
	%s                                    # Show all pods across all namespaces
	%s --namespace default                # Show pods in default namespace
	%s --label app=nginx                  # Filter pods by label
	%s --label "app=nginx,env=prod"       # Filter by multiple labels
	%s --problems                         # Show only problematic pods
	%s --sort restarts                    # Sort by restart count
	%s --sort cpu                         # Sort by CPU usage
	%s --format json                      # Output in JSON format
	%s --format csv > pods.csv            # Export to CSV file
	%s --watch                            # Watch mode with auto-refresh
	%s --watch --refresh 10s              # Watch with custom refresh rate
	%s --interactive                      # Interactive mode with menu
	%s --no-color                         # Accessible output without colors
	%s --namespace kube-system --verbose  # Verbose output for specific namespace
	%s --since 10m                        # Pods created in the last 10 minutes
	%s --since 2h --problems              # Recent problematic pods
	kubectl-monitor --label app=nginx --problems --sort restarts --format json  # Combined

CONFIG FILE:
	Defaults can be set in ~/.kubectl-monitor.yaml:
	  namespace: production
	  sort: restarts
	  format: table
	  refresh: 10s
	  no_color: false
	  since: 2h
	CLI flags always override config file values.

ACCESSIBILITY:
	This tool supports screen readers and colorblind users:
	- Use --no-color for screen readers or colorblind accessibility
	- Status indicators use symbols in addition to colors
	- High contrast colors are used when colors are enabled
	- Interactive mode provides keyboard navigation

KEYBOARD SHORTCUTS (Interactive Mode):
	Arrow Keys - Navigate menu
	Enter      - Select option
	q, Ctrl+C  - Quit
	r          - Refresh
	n          - Change namespace
	w          - Toggle watch mode
	c          - Toggle colors
	l          - View pod logs
`,
		AppName, AppName, RefreshInterval,
		AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName, AppName)
}

// printVersion prints version information
func printVersion() {
	fmt.Printf("%s version %s\n", AppName, AppVersion)
	fmt.Println("Enhanced Kubernetes pod monitoring tool with accessibility features")
	fmt.Println("")
	fmt.Println("Features:")
	fmt.Println("  - Real-time pod monitoring")
	fmt.Println("  - Watch mode with auto-refresh")
	fmt.Println("  - Interactive mode with menu navigation and pod logs")
	fmt.Println("  - Label selector filtering (--label)")
	fmt.Println("  - Problem pods filtering (--problems)")
	fmt.Println("  - Recent pods filter (--since)")
	fmt.Println("  - Sorting by cpu, memory, restarts, age, name (--sort)")
	fmt.Println("  - JSON and CSV output formats (--format)")
	fmt.Println("  - Accessibility support (colorblind, screen readers)")
	fmt.Println("  - Progress indicators and user feedback")
	fmt.Println("  - Configurable refresh rates")
	fmt.Println("  - Config file support (~/.kubectl-monitor.yaml)")
	fmt.Println("  - Watch mode diff (highlights changes between refreshes)")
	fmt.Println("  - Comprehensive error handling")
	fmt.Println("")
	fmt.Println("Copyright (c) 2024 - Licensed under MIT")
	fmt.Println("Report issues: https://github.com/renancavalcantercb/kubectl-monitor/issues")
}

// loadConfigFile reads ~/.kubectl-monitor.yaml and returns parsed config, or nil if not found
func loadConfigFile() (*FileConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil // no home dir, skip silently
	}
	path := filepath.Join(home, ".kubectl-monitor.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // file doesn't exist, that's fine
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	var fc FileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("invalid config file (~/.kubectl-monitor.yaml): %w", err)
	}
	return &fc, nil
}

// applyFileConfig applies file-based config values as defaults (CLI args override these)
func applyFileConfig(config *Config, fc *FileConfig) error {
	if fc.Namespace != "" {
		config.Namespace = fc.Namespace
		config.AllNamespaces = false
	}
	if fc.Sort != "" {
		validSorts := map[string]bool{"cpu": true, "memory": true, "restarts": true, "age": true, "name": true}
		if !validSorts[strings.ToLower(fc.Sort)] {
			return fmt.Errorf("config file: invalid sort value: %s (valid: cpu, memory, restarts, age, name)", fc.Sort)
		}
		config.SortBy = strings.ToLower(fc.Sort)
	}
	if fc.Format != "" {
		validFormats := map[string]bool{"table": true, "json": true, "csv": true}
		if !validFormats[strings.ToLower(fc.Format)] {
			return fmt.Errorf("config file: invalid format value: %s (valid: table, json, csv)", fc.Format)
		}
		config.OutputFormat = strings.ToLower(fc.Format)
	}
	if fc.Refresh != "" {
		d, err := time.ParseDuration(fc.Refresh)
		if err != nil {
			return fmt.Errorf("config file: invalid refresh duration: %v", err)
		}
		config.RefreshRate = d
	}
	if fc.Since != "" {
		d, err := time.ParseDuration(fc.Since)
		if err != nil {
			return fmt.Errorf("config file: invalid since duration: %v", err)
		}
		config.Since = d
	}
	if fc.NoColor {
		config.NoColor = true
	}
	if fc.Quiet {
		config.Quiet = true
	}
	if fc.Verbose {
		config.Verbose = true
	}
	if fc.Labels != "" {
		config.Labels = fc.Labels
	}
	return nil
}

// podsToMap converts a slice of PodData to a map keyed by "namespace/name"
func podsToMap(pods []PodData) map[string]PodData {
	m := make(map[string]PodData, len(pods))
	for _, p := range pods {
		m[p.Namespace+"/"+p.Name] = p
	}
	return m
}

// diffPods computes what changed between two pod maps
func diffPods(previous, current map[string]PodData) PodDiff {
	var diff PodDiff

	for key, cur := range current {
		prev, existed := previous[key]
		if !existed {
			diff.Added = append(diff.Added, cur)
			continue
		}
		change := PodChange{Pod: cur}
		changed := false
		if cur.Status != prev.Status {
			change.OldStatus = prev.Status
			changed = true
		}
		if cur.Restarts > prev.Restarts {
			change.RestartsAdded = cur.Restarts - prev.Restarts
			changed = true
		}
		if changed {
			diff.Changed = append(diff.Changed, change)
		}
	}

	for key := range previous {
		if _, exists := current[key]; !exists {
			diff.Removed = append(diff.Removed, key)
		}
	}

	return diff
}

// printDiff prints a summary of pod changes since the last watch refresh
func (m *Monitor) printDiff(diff PodDiff) {
	colorEnabled := m.Config.IsColorEnabled()
	sep := strings.Repeat("─", 52)

	if colorEnabled {
		fmt.Fprintf(m.Config.Output, "%s── Changes since last refresh ─────────────────────%s\n", ColorBold, ColorReset)
	} else {
		fmt.Fprintln(m.Config.Output, "-- Changes since last refresh --")
	}

	for _, pod := range diff.Added {
		name := pod.Namespace + "/" + pod.Name
		if !m.Config.AllNamespaces {
			name = pod.Name
		}
		label := fmt.Sprintf("  + %s (new)", name)
		if colorEnabled {
			fmt.Fprintf(m.Config.Output, "%s%s%s\n", ColorHiGreen, label, ColorReset)
		} else {
			fmt.Fprintln(m.Config.Output, label)
		}
	}

	for _, key := range diff.Removed {
		parts := strings.SplitN(key, "/", 2)
		name := key
		if len(parts) == 2 && !m.Config.AllNamespaces {
			name = parts[1]
		}
		label := fmt.Sprintf("  - %s (removed)", name)
		if colorEnabled {
			fmt.Fprintf(m.Config.Output, "%s%s%s\n", ColorHiRed, label, ColorReset)
		} else {
			fmt.Fprintln(m.Config.Output, label)
		}
	}

	for _, c := range diff.Changed {
		name := c.Pod.Namespace + "/" + c.Pod.Name
		if !m.Config.AllNamespaces {
			name = c.Pod.Name
		}
		var parts []string
		if c.OldStatus != "" {
			parts = append(parts, fmt.Sprintf("%s→%s", c.OldStatus, c.Pod.Status))
		}
		if c.RestartsAdded > 0 {
			parts = append(parts, fmt.Sprintf("restarts +%d (total: %d)", c.RestartsAdded, c.Pod.Restarts))
		}
		label := fmt.Sprintf("  ~ %s  %s", name, strings.Join(parts, ", "))
		if colorEnabled {
			fmt.Fprintf(m.Config.Output, "%s%s%s\n", ColorHiYellow, label, ColorReset)
		} else {
			fmt.Fprintln(m.Config.Output, label)
		}
	}

	fmt.Fprintln(m.Config.Output, sep)
}

// runMonitor executes the main monitoring logic
func (m *Monitor) runMonitor() error {
	m.Config.LogVerbose("Starting kubectl commands execution")

	// Show progress for potentially slow operations
	m.progress.Start("Fetching pod information...")

	results := make(chan CommandResult, 2)
	var wg sync.WaitGroup

	// Build kubectl get pods arguments
	getArgs := []string{GetCmd, PodsCmd, AllNamespacesFlag, "-o", CustomColumns}
	if m.Config.Labels != "" {
		getArgs = append(getArgs, "-l", m.Config.Labels)
	}

	// Build kubectl top pods arguments
	topArgs := []string{TopCmd, PodsCmd, AllNamespacesFlag}
	if m.Config.Labels != "" {
		topArgs = append(topArgs, "-l", m.Config.Labels)
	}

	// Execute kubectl commands concurrently
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.Config.LogVerbose("Executing kubectl get pods command")
		output, err := m.Runner.RunCommand(KubectlCmd, getArgs...)
		results <- CommandResult{Output: output, Error: err, Type: "get"}
	}()

	go func() {
		defer wg.Done()
		m.Config.LogVerbose("Executing kubectl top pods command")
		output, err := m.Runner.RunCommand(KubectlCmd, topArgs...)
		results <- CommandResult{Output: output, Error: err, Type: "top"}
	}()

	wg.Wait()
	close(results)

	// Collect results
	var getPodsOutput, topPodsOutput string
	var getError, topError error

	for result := range results {
		if result.Type == "get" {
			getPodsOutput = result.Output
			getError = result.Error
		} else {
			topPodsOutput = result.Output
			topError = result.Error
		}
	}

	// Handle errors gracefully
	if getError != nil {
		m.progress.Stop(false, "Failed to fetch pod information")
		return fmt.Errorf("kubectl get pods failed: %v", getError)
	}

	if topError != nil {
		m.Config.LogWarning("kubectl top pods failed (metrics may not be available): %v", topError)
		m.Config.LogVerbose("Continuing without resource metrics...")
		topPodsOutput = "" // Continue without metrics
	}

	m.progress.Stop(true, "Pod information retrieved successfully")

	// Compute and display diff in watch mode
	if m.Config.Watch {
		currentPods, err := parsePods(getPodsOutput, topPodsOutput, m.Config)
		if err == nil {
			currentMap := podsToMap(currentPods)
			if m.previousPods != nil {
				diff := diffPods(m.previousPods, currentMap)
				if diff.HasChanges() {
					m.printDiff(diff)
				}
			}
			m.previousPods = currentMap
		}
	}

	return renderTable(getPodsOutput, topPodsOutput, m.Config)
}

// runCommand executes a shell command with timeout and returns its output
func runCommand(command string, args ...string) (string, error) {
	if command == "" {
		return "", errors.New("command cannot be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), KubectlTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command '%s %s' timed out after %v",
				command, strings.Join(args, " "), KubectlTimeout)
		}
		return "", fmt.Errorf("failed to execute '%s %s': %v\nOutput: %s",
			command, strings.Join(args, " "), err, string(output))
	}
	return string(output), nil
}

// isTerminal checks if the output is a terminal
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		// Simple terminal detection - in a real implementation, you might use
		// a library like golang.org/x/crypto/ssh/terminal
		return f.Fd() == 1 || f.Fd() == 2
	}
	return false
}

// runInteractiveMode runs the monitor in interactive mode
func runInteractiveMode(config *Config) error {
	config.LogInfo("Starting interactive mode")
	config.LogInfo("Use arrow keys to navigate, Enter to select, 'q' to quit")

	reader := bufio.NewReader(os.Stdin)
	currentNamespace := config.Namespace
	colorsEnabled := !config.NoColor

	for {
		// Clear screen and show menu
		if config.IsColorEnabled() {
			fmt.Print("\033[2J\033[H")
		}

		printInteractiveMenu(config, currentNamespace, colorsEnabled)

		// Get user input
		fmt.Print("\nChoice: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %v", err)
		}

		input = strings.TrimSpace(input)

		switch input {
		case "1": // Show pods
			tempConfig := *config
			tempConfig.Namespace = currentNamespace
			tempConfig.AllNamespaces = (currentNamespace == AllNamespacesFlag)
			tempConfig.Watch = false
			tempConfig.NoColor = !colorsEnabled

			monitor := NewMonitor(&tempConfig)
			if err := monitor.runOnceMode(); err != nil {
				config.LogError("Failed to show pods: %v", err)
			}
			monitor.Close()

			fmt.Print("\nPress Enter to continue...")
			reader.ReadString('\n')

		case "2": // Start watch mode
			tempConfig := *config
			tempConfig.Namespace = currentNamespace
			tempConfig.AllNamespaces = (currentNamespace == AllNamespacesFlag)
			tempConfig.Watch = true
			tempConfig.NoColor = !colorsEnabled

			monitor := NewMonitor(&tempConfig)
			config.LogInfo("Starting watch mode - Press Ctrl+C to return to menu")
			monitor.Run() // This will run until interrupted
			monitor.Close()

		case "3": // Change namespace
			currentNamespace = promptForNamespace(config, reader)

		case "4": // Toggle colors
			colorsEnabled = !colorsEnabled
			config.LogInfo("Colors %s", map[bool]string{true: "enabled", false: "disabled"}[colorsEnabled])
			time.Sleep(1 * time.Second)

		case "5": // Settings
			showSettings(config, reader)

		case "6": // View pod logs
			viewPodLogs(config, reader, currentNamespace)

		case "q", "quit", "exit":
			config.LogInfo("Goodbye!")
			return nil

		default:
			config.LogWarning("Invalid choice: %s", input)
			time.Sleep(1 * time.Second)
		}
	}
}

// printInteractiveMenu displays the interactive menu
func printInteractiveMenu(config *Config, namespace string, colorsEnabled bool) {
	title := "kubectl-monitor Interactive Mode"
	if config.IsColorEnabled() && colorsEnabled {
		title = ColorBold + ColorCyan + title + ColorReset
	}

	fmt.Println(title)
	fmt.Println(strings.Repeat("=", len("kubectl-monitor Interactive Mode")))
	fmt.Println()

	fmt.Printf("Current Namespace: %s\n", formatNamespaceDisplay(config, namespace, colorsEnabled))
	fmt.Printf("Colors: %s\n", formatBooleanDisplay(config, colorsEnabled, colorsEnabled))
	fmt.Println()

	fmt.Println("Options:")
	fmt.Println("  1. Show pods now")
	fmt.Println("  2. Start watch mode")
	fmt.Println("  3. Change namespace")
	fmt.Println("  4. Toggle colors")
	fmt.Println("  5. Settings")
	fmt.Println("  6. View pod logs")
	fmt.Println("  q. Quit")
}

// formatNamespaceDisplay formats namespace for display
func formatNamespaceDisplay(config *Config, namespace string, colorsEnabled bool) string {
	if !config.IsColorEnabled() || !colorsEnabled {
		return namespace
	}

	if namespace == AllNamespacesFlag {
		return ColorYellow + "All Namespaces" + ColorReset
	}
	return ColorGreen + namespace + ColorReset
}

// formatBooleanDisplay formats boolean values for display
func formatBooleanDisplay(config *Config, value, colorsEnabled bool) string {
	display := "Disabled"
	if value {
		display = "Enabled"
	}

	if !config.IsColorEnabled() || !colorsEnabled {
		return display
	}

	if value {
		return ColorGreen + display + ColorReset
	}
	return ColorDim + display + ColorReset
}

// promptForNamespace prompts user to enter a namespace
func promptForNamespace(config *Config, reader *bufio.Reader) string {
	fmt.Println("\nNamespace Options:")
	fmt.Println("  1. All namespaces")
	fmt.Println("  2. default")
	fmt.Println("  3. kube-system")
	fmt.Println("  4. Enter custom namespace")
	fmt.Print("\nChoice: ")

	input, err := reader.ReadString('\n')
	if err != nil {
		config.LogError("Failed to read input: %v", err)
		return AllNamespacesFlag
	}

	input = strings.TrimSpace(input)

	switch input {
	case "1":
		return AllNamespacesFlag
	case "2":
		return "default"
	case "3":
		return "kube-system"
	case "4":
		fmt.Print("Enter namespace name: ")
		namespace, err := reader.ReadString('\n')
		if err != nil {
			config.LogError("Failed to read namespace: %v", err)
			return AllNamespacesFlag
		}
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			config.LogWarning("Empty namespace, using all namespaces")
			return AllNamespacesFlag
		}
		return namespace
	default:
		config.LogWarning("Invalid choice, using all namespaces")
		return AllNamespacesFlag
	}
}

// showSettings displays and allows modification of settings
func showSettings(config *Config, reader *bufio.Reader) {
	fmt.Println("\nSettings:")
	fmt.Printf("  Current refresh rate: %v\n", config.RefreshRate)
	fmt.Printf("  Verbose mode: %v\n", config.Verbose)
	fmt.Printf("  Quiet mode: %v\n", config.Quiet)
	fmt.Println("\nOptions:")
	fmt.Println("  1. Change refresh rate")
	fmt.Println("  2. Toggle verbose mode")
	fmt.Println("  3. Toggle quiet mode")
	fmt.Println("  4. Return to main menu")
	fmt.Print("\nChoice: ")

	input, err := reader.ReadString('\n')
	if err != nil {
		config.LogError("Failed to read input: %v", err)
		return
	}

	input = strings.TrimSpace(input)

	switch input {
	case "1":
		fmt.Print("Enter refresh rate (e.g., 5s, 10s): ")
		rate, err := reader.ReadString('\n')
		if err != nil {
			config.LogError("Failed to read refresh rate: %v", err)
			return
		}
		rate = strings.TrimSpace(rate)
		if duration, err := time.ParseDuration(rate); err == nil {
			config.RefreshRate = duration
			config.LogSuccess("Refresh rate updated to %v", duration)
		} else {
			config.LogError("Invalid duration: %v", err)
		}
	case "2":
		config.Verbose = !config.Verbose
		config.LogInfo("Verbose mode %s", map[bool]string{true: "enabled", false: "disabled"}[config.Verbose])
	case "3":
		config.Quiet = !config.Quiet
		config.LogInfo("Quiet mode %s", map[bool]string{true: "enabled", false: "disabled"}[config.Quiet])
	case "4":
		return
	default:
		config.LogWarning("Invalid choice")
	}

	fmt.Print("\nPress Enter to continue...")
	reader.ReadString('\n')
}

// viewPodLogs handles the interactive pod logs viewing
func viewPodLogs(config *Config, reader *bufio.Reader, namespace string) {
	// Get list of pods
	pods, err := listPodsForSelection(config, namespace)
	if err != nil {
		config.LogError("Failed to list pods: %v", err)
		fmt.Print("\nPress Enter to continue...")
		reader.ReadString('\n')
		return
	}

	if len(pods) == 0 {
		config.LogWarning("No pods found")
		fmt.Print("\nPress Enter to continue...")
		reader.ReadString('\n')
		return
	}

	// Display pod selection menu
	fmt.Println("\nSelect a pod to view logs:")
	fmt.Println(strings.Repeat("-", 60))
	for i, pod := range pods {
		if namespace == AllNamespacesFlag {
			fmt.Printf("  %d. %s/%s (%s)\n", i+1, pod.Namespace, pod.Name, pod.Status)
		} else {
			fmt.Printf("  %d. %s (%s)\n", i+1, pod.Name, pod.Status)
		}
	}
	fmt.Println("  0. Cancel")
	fmt.Print("\nChoice: ")

	input, err := reader.ReadString('\n')
	if err != nil {
		config.LogError("Failed to read input: %v", err)
		return
	}

	input = strings.TrimSpace(input)
	if input == "0" || input == "" {
		return
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(pods) {
		config.LogWarning("Invalid selection")
		fmt.Print("\nPress Enter to continue...")
		reader.ReadString('\n')
		return
	}

	selectedPod := pods[choice-1]

	// Ask for number of lines
	fmt.Print("Number of log lines to show (default: 50): ")
	linesInput, _ := reader.ReadString('\n')
	linesInput = strings.TrimSpace(linesInput)
	lines := 50
	if linesInput != "" {
		if parsed, err := strconv.Atoi(linesInput); err == nil && parsed > 0 {
			lines = parsed
		}
	}

	// Show logs
	err = showPodLogs(config, selectedPod.Namespace, selectedPod.Name, lines)
	if err != nil {
		config.LogError("Failed to get logs: %v", err)
	}

	fmt.Print("\nPress Enter to continue...")
	reader.ReadString('\n')
}

// listPodsForSelection returns a list of pods for the interactive selection
func listPodsForSelection(config *Config, namespace string) ([]PodData, error) {
	var args []string
	if namespace == AllNamespacesFlag {
		args = []string{GetCmd, PodsCmd, AllNamespacesFlag, "-o", CustomColumns}
	} else {
		args = []string{GetCmd, PodsCmd, "-n", namespace, "-o", CustomColumns}
	}

	output, err := runCommand(KubectlCmd, args...)
	if err != nil {
		return nil, err
	}

	tempConfig := &Config{
		AllNamespaces: namespace == AllNamespacesFlag,
		Namespace:     namespace,
	}

	return parsePods(output, "", tempConfig)
}

// showPodLogs displays logs for a specific pod
func showPodLogs(config *Config, namespace, podName string, lines int) error {
	args := []string{"logs", "-n", namespace, podName, "--tail", strconv.Itoa(lines)}

	config.LogInfo("Fetching logs for %s/%s (last %d lines)...", namespace, podName, lines)
	fmt.Println(strings.Repeat("-", 60))

	output, err := runCommand(KubectlCmd, args...)
	if err != nil {
		return err
	}

	fmt.Println(output)
	fmt.Println(strings.Repeat("-", 60))

	return nil
}

// parseTopPods parses kubectl top pods output into a usage map
func parseTopPods(output string) map[string]map[string]PodUsage {
	if output == "" {
		return make(map[string]map[string]PodUsage)
	}

	lines := strings.Split(output, "\n")
	usage := make(map[string]map[string]PodUsage, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "NAME") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < MinRequiredFields {
			continue
		}

		namespace := fields[0]
		name := fields[1]
		cpu := fields[2]
		memory := fields[3]

		if _, exists := usage[namespace]; !exists {
			usage[namespace] = make(map[string]PodUsage)
		}
		usage[namespace][name] = PodUsage{
			CPU:    cpu,
			Memory: memory,
		}
	}
	return usage
}

// formatAge converts RFC3339 timestamp to human-readable age
func formatAge(age string) string {
	if age == "" {
		return NotAvailable
	}

	parsedTime, err := time.Parse(time.RFC3339, age)
	if err != nil {
		return NotAvailable
	}

	duration := time.Since(parsedTime)
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", minutes)
	}
}

// getStatusSymbol returns a symbol for the pod status (accessibility)
func getStatusSymbol(status string) string {
	switch status {
	case StatusRunning:
		return "✓" // ✓
	case StatusPending:
		return "⌛" // ⌛
	case StatusFailed:
		return "✗" // ✗
	case StatusUnknown:
		return "?" // ?
	default:
		return "-"
	}
}

// formatStatusWithAccessibility formats status with both color and symbols
func formatStatusWithAccessibility(status string, colorEnabled bool) string {
	symbol := getStatusSymbol(status)

	if !colorEnabled {
		return fmt.Sprintf("%s %s", symbol, status)
	}

	// Use high contrast colors for better accessibility
	switch status {
	case StatusRunning:
		return fmt.Sprintf("%s%s %s%s", ColorHiGreen, symbol, status, ColorReset)
	case StatusPending:
		return fmt.Sprintf("%s%s %s%s", ColorHiYellow, symbol, status, ColorReset)
	case StatusFailed:
		return fmt.Sprintf("%s%s %s%s", ColorHiRed, symbol, status, ColorReset)
	case StatusUnknown:
		return fmt.Sprintf("%s%s %s%s", ColorHiMagenta, symbol, status, ColorReset)
	default:
		return fmt.Sprintf("%s %s", symbol, status)
	}
}

// parsePods parses kubectl output into PodData slice
func parsePods(getPodsOutput, topPodsOutput string, config *Config) ([]PodData, error) {
	if getPodsOutput == "" {
		return nil, errors.New("pod output is empty")
	}

	topPodsUsage := parseTopPods(topPodsOutput)
	lines := strings.Split(getPodsOutput, "\n")
	pods := make([]PodData, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "NAMESPACE") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < MinPodFields {
			continue
		}

		podNamespace := fields[0]
		name := fields[1]
		status := fields[2]
		restartsStr := fields[3]
		ageRaw := strings.Join(fields[4:], " ")

		// Filter by namespace if not using all namespaces
		if !config.AllNamespaces && config.Namespace != podNamespace {
			continue
		}

		restarts, _ := strconv.Atoi(restartsStr)
		ageTime, _ := time.Parse(time.RFC3339, ageRaw)

		cpu := NotAvailable
		memory := NotAvailable
		var cpuMillis int64
		var memoryBytes int64

		if nsUsage, exists := topPodsUsage[podNamespace]; exists {
			if usage, found := nsUsage[name]; found {
				cpu = usage.CPU
				memory = usage.Memory
				cpuMillis = parseCPU(cpu)
				memoryBytes = parseMemory(memory)
			}
		}

		pod := PodData{
			Namespace:   podNamespace,
			Name:        name,
			Status:      status,
			Restarts:    restarts,
			Age:         formatAge(ageRaw),
			AgeTime:     ageTime,
			CPU:         cpu,
			Memory:      memory,
			CPUMillis:   cpuMillis,
			MemoryBytes: memoryBytes,
		}

		// Filter by problems if --problems flag is set
		if config.ProblemsOnly {
			if status == StatusRunning && restarts == 0 {
				continue // Skip healthy pods
			}
		}

		// Filter by --since: only pods created within the given duration
		if config.Since > 0 && !ageTime.IsZero() {
			if !ageTime.After(time.Now().Add(-config.Since)) {
				continue
			}
		}

		pods = append(pods, pod)
	}

	return pods, nil
}

// parseCPU parses CPU string (e.g., "100m", "1") to millicores
func parseCPU(cpu string) int64 {
	if cpu == NotAvailable || cpu == "" {
		return 0
	}
	cpu = strings.TrimSpace(cpu)
	if strings.HasSuffix(cpu, "m") {
		val, _ := strconv.ParseInt(strings.TrimSuffix(cpu, "m"), 10, 64)
		return val
	}
	// If no suffix, it's in cores
	val, _ := strconv.ParseFloat(cpu, 64)
	return int64(val * 1000)
}

// parseMemory parses memory string (e.g., "128Mi", "1Gi") to bytes
func parseMemory(memory string) int64 {
	if memory == NotAvailable || memory == "" {
		return 0
	}
	memory = strings.TrimSpace(memory)

	multipliers := map[string]int64{
		"Ki": 1024,
		"Mi": 1024 * 1024,
		"Gi": 1024 * 1024 * 1024,
		"Ti": 1024 * 1024 * 1024 * 1024,
		"K":  1000,
		"M":  1000 * 1000,
		"G":  1000 * 1000 * 1000,
		"T":  1000 * 1000 * 1000 * 1000,
	}

	for suffix, mult := range multipliers {
		if strings.HasSuffix(memory, suffix) {
			val, _ := strconv.ParseInt(strings.TrimSuffix(memory, suffix), 10, 64)
			return val * mult
		}
	}

	val, _ := strconv.ParseInt(memory, 10, 64)
	return val
}

// sortPods sorts pods by the specified field
func sortPods(pods []PodData, sortBy string) {
	if sortBy == "" {
		return
	}

	sort.Slice(pods, func(i, j int) bool {
		switch sortBy {
		case "cpu":
			return pods[i].CPUMillis > pods[j].CPUMillis // Descending
		case "memory":
			return pods[i].MemoryBytes > pods[j].MemoryBytes // Descending
		case "restarts":
			return pods[i].Restarts > pods[j].Restarts // Descending
		case "age":
			return pods[i].AgeTime.Before(pods[j].AgeTime) // Oldest first
		case "name":
			return pods[i].Name < pods[j].Name // Alphabetical
		default:
			return false
		}
	})
}

// renderJSON outputs pods in JSON format
func renderJSON(pods []PodData, config *Config) error {
	encoder := json.NewEncoder(config.Output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(pods)
}

// renderCSV outputs pods in CSV format
func renderCSV(pods []PodData, config *Config) error {
	writer := csv.NewWriter(config.Output)
	defer writer.Flush()

	// Write header
	var headers []string
	if config.AllNamespaces {
		headers = []string{"NAMESPACE", "NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"}
	} else {
		headers = []string{"NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"}
	}
	if err := writer.Write(headers); err != nil {
		return err
	}

	// Write data rows
	for _, pod := range pods {
		var row []string
		if config.AllNamespaces {
			row = []string{
				pod.Namespace,
				pod.Name,
				pod.CPU,
				pod.Memory,
				pod.Status,
				strconv.Itoa(pod.Restarts),
				pod.Age,
			}
		} else {
			row = []string{
				pod.Name,
				pod.CPU,
				pod.Memory,
				pod.Status,
				strconv.Itoa(pod.Restarts),
				pod.Age,
			}
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// renderTable renders pod information in a formatted table
func renderTable(getPodsOutput, topPodsOutput string, config *Config) error {
	// Parse pods into PodData slice
	pods, err := parsePods(getPodsOutput, topPodsOutput, config)
	if err != nil {
		return err
	}

	// Sort pods if --sort flag is set
	sortPods(pods, config.SortBy)

	// Check for empty results
	if len(pods) == 0 {
		if config.ProblemsOnly {
			fmt.Fprintln(config.Output, "No problematic pods found")
		} else if config.AllNamespaces {
			fmt.Fprintln(config.Output, "No pods found in any namespace")
		} else {
			fmt.Fprintf(config.Output, "No pods found in namespace '%s'\n", config.Namespace)
		}
		return nil
	}

	// Render based on output format
	switch config.OutputFormat {
	case "json":
		return renderJSON(pods, config)
	case "csv":
		return renderCSV(pods, config)
	default:
		return renderTableFormat(pods, config)
	}
}

// renderTableFormat renders pods in table format
func renderTableFormat(pods []PodData, config *Config) error {
	table := tablewriter.NewWriter(config.Output)

	var headers []string
	if config.AllNamespaces {
		headers = []string{"NAMESPACE", "NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"}
	} else {
		headers = []string{"NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"}
	}

	table.SetHeader(headers)
	setupTableFormat(table, len(headers), config)

	for _, pod := range pods {
		status := formatStatusWithAccessibility(pod.Status, config.IsColorEnabled())

		if config.AllNamespaces {
			table.Append([]string{
				pod.Namespace,
				pod.Name,
				pod.CPU,
				pod.Memory,
				status,
				strconv.Itoa(pod.Restarts),
				pod.Age,
			})
		} else {
			table.Append([]string{
				pod.Name,
				pod.CPU,
				pod.Memory,
				status,
				strconv.Itoa(pod.Restarts),
				pod.Age,
			})
		}
	}

	table.Render()
	return nil
}

// setupTableFormat configures table formatting options
func setupTableFormat(table *tablewriter.Table, headerCount int, config *Config) {
	table.SetBorder(true)
	table.SetRowLine(false)
	table.SetCenterSeparator("|")
	table.SetColumnSeparator("|")
	table.SetRowSeparator("-")
	table.SetHeaderLine(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)

	// Only apply colors if enabled (accessibility)
	if config.IsColorEnabled() {
		// Create header colors based on the number of headers
		headerColors := make([]tablewriter.Colors, headerCount)
		for i := 0; i < headerCount; i++ {
			headerColors[i] = tablewriter.Colors{tablewriter.Bold, tablewriter.FgHiBlueColor}
		}
		table.SetHeaderColor(headerColors...)
	}

	// Set table caption for screen readers
	if config.AllNamespaces {
		table.SetCaption(true, "Kubernetes pods across all namespaces with resource usage")
	} else {
		table.SetCaption(true, fmt.Sprintf("Kubernetes pods in namespace '%s' with resource usage", config.Namespace))
	}
}
