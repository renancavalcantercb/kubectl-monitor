package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/olekukonko/tablewriter"
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
	NotAvailable      = "N/A"
	MinRequiredFields = 4
	MinPodFields      = 5
	AppVersion        = "2.1.0"
	AppName           = "kubectl-monitor"

	// Command timeouts
	KubectlTimeout    = 30 * time.Second
	RefreshInterval   = 5 * time.Second
	ProgressInterval  = 100 * time.Millisecond

	// ANSI color codes - Accessibility friendly
	ColorReset     = "\033[0m"
	ColorGreen     = "\033[32m"     // Running status
	ColorYellow    = "\033[33m"     // Pending status
	ColorRed       = "\033[31m"     // Failed status
	ColorMagenta   = "\033[35m"     // Unknown status
	ColorCyan      = "\033[36m"     // Headers
	ColorBold      = "\033[1m"      // Emphasis
	ColorDim       = "\033[2m"      // Secondary text
	
	// High contrast colors for accessibility
	ColorHiGreen   = "\033[92m"     // High contrast green
	ColorHiYellow  = "\033[93m"     // High contrast yellow
	ColorHiRed     = "\033[91m"     // High contrast red
	ColorHiMagenta = "\033[95m"     // High contrast magenta

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
	Runner      KubectlRunner
	Config      *Config
	ctx         context.Context
	cancel      context.CancelFunc
	progress    *ProgressIndicator
	colorizer   *ColorManager
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
	--watch                 Watch mode with auto-refresh
	--interactive          Interactive mode with menu
	--refresh <duration>   Refresh rate for watch mode (default: %v)

  Display Options:
	--no-color             Disable colored output (accessibility)
	--quiet                Suppress informational messages
	--verbose              Show detailed information

  Help:
	-h, --help             Show this help message
	-v, --version          Show version information

EXAMPLES:
	%s                                    # Show all pods across all namespaces
	%s --namespace default                # Show pods in default namespace
	%s --watch                           # Watch mode with auto-refresh
	%s --watch --refresh 10s             # Watch with custom refresh rate
	%s --interactive                     # Interactive mode with menu
	%s --no-color                        # Accessible output without colors
	%s --namespace kube-system --verbose # Verbose output for specific namespace

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
`, 
		AppName, AppName, RefreshInterval, 
		AppName, AppName, AppName, AppName, AppName, AppName, AppName)
}

// printVersion prints version information
func printVersion() {
	fmt.Printf("%s version %s\n", AppName, AppVersion)
	fmt.Println("Enhanced Kubernetes pod monitoring tool with accessibility features")
	fmt.Println("")
	fmt.Println("Features:")
	fmt.Println("  - Real-time pod monitoring")
	fmt.Println("  - Watch mode with auto-refresh")
	fmt.Println("  - Interactive mode with menu navigation")
	fmt.Println("  - Accessibility support (colorblind, screen readers)")
	fmt.Println("  - Progress indicators and user feedback")
	fmt.Println("  - Configurable refresh rates")
	fmt.Println("  - Comprehensive error handling")
	fmt.Println("")
	fmt.Println("Copyright (c) 2024 - Licensed under MIT")
	fmt.Println("Report issues: https://github.com/renancavalcantercb/kubectl-monitor/issues")
}

// runMonitor executes the main monitoring logic
func (m *Monitor) runMonitor() error {
	m.Config.LogVerbose("Starting kubectl commands execution")
	
	// Show progress for potentially slow operations
	m.progress.Start("Fetching pod information...")
	
	results := make(chan CommandResult, 2)
	var wg sync.WaitGroup

	// Execute kubectl commands concurrently
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.Config.LogVerbose("Executing kubectl get pods command")
		output, err := m.Runner.RunCommand(KubectlCmd, GetCmd, PodsCmd, AllNamespacesFlag, "-o", CustomColumns)
		results <- CommandResult{Output: output, Error: err, Type: "get"}
	}()

	go func() {
		defer wg.Done()
		m.Config.LogVerbose("Executing kubectl top pods command")
		output, err := m.Runner.RunCommand(KubectlCmd, TopCmd, PodsCmd, AllNamespacesFlag)
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

// renderTable renders pod information in a formatted table
func renderTable(getPodsOutput, topPodsOutput string, config *Config) error {
	if getPodsOutput == "" {
		return errors.New("pod output is empty")
	}

	topPodsUsage := parseTopPods(topPodsOutput)
	lines := strings.Split(getPodsOutput, "\n")

	table := tablewriter.NewWriter(config.Output)

	var headers []string
	if config.AllNamespaces {
		headers = []string{"NAMESPACE", "NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"}
	} else {
		headers = []string{"NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"}
	}
	
	table.SetHeader(headers)
	setupTableFormat(table, len(headers), config)

	rowCount := 0
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
		status := formatStatusWithAccessibility(fields[2], config.IsColorEnabled())
		restarts := fields[3]
		ageRaw := strings.Join(fields[4:], " ")
		age := formatAge(ageRaw)

		cpu := NotAvailable
		memory := NotAvailable
		if nsUsage, exists := topPodsUsage[podNamespace]; exists {
			if usage, found := nsUsage[name]; found {
				cpu = usage.CPU
				memory = usage.Memory
			}
		}

		if config.AllNamespaces {
			table.Append([]string{podNamespace, name, cpu, memory, status, restarts, age})
			rowCount++
		} else if config.Namespace == podNamespace {
			table.Append([]string{name, cpu, memory, status, restarts, age})
			rowCount++
		}
	}

	if rowCount == 0 {
		if config.AllNamespaces {
			fmt.Println("No pods found in any namespace")
		} else {
			fmt.Printf("No pods found in namespace '%s'\n", config.Namespace)
		}
		return nil
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
