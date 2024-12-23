package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
)

func main() {
	namespace := "--all-namespaces"

	if len(os.Args) > 2 && os.Args[1] == "--namespace" {
		namespace = os.Args[2]
	}

	getPodsOutput, err := runCommand("kubectl", "get", "pods", "--all-namespaces", "-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,STATUS:.status.phase,RESTARTS:.status.containerStatuses[0].restartCount,AGE:.metadata.creationTimestamp")
	if err != nil {
		fmt.Println("Error fetching pods:", err)
		return
	}

	topPodsOutput, err := runCommand("kubectl", "top", "pods", "--all-namespaces")
	if err != nil {
		fmt.Println("Error fetching pod metrics:", err)
		return
	}

	renderTable(getPodsOutput, topPodsOutput, namespace)
}

func runCommand(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error executing command: %v, output: %s", err, string(output))
	}
	return string(output), nil
}

func parseTopPods(lines []string) map[string]map[string][2]string {
	usage := make(map[string]map[string][2]string)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "NAME") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		namespace := fields[0]
		name := fields[1]
		cpu := fields[2]
		memory := fields[3]

		if _, exists := usage[namespace]; !exists {
			usage[namespace] = make(map[string][2]string)
		}
		usage[namespace][name] = [2]string{cpu, memory}
	}
	return usage
}

func formatAge(age string) string {
	parsedTime, err := time.Parse(time.RFC3339, age)
	if err != nil {
		return "N/A"
	}

	duration := time.Since(parsedTime)
	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}

func colorStatus(status string) string {
	switch status {
	case "Running":
		return "\033[32m" + status + "\033[0m" // Green
	case "Pending":
		return "\033[33m" + status + "\033[0m" // Yellow
	case "Failed":
		return "\033[31m" + status + "\033[0m" // Red
	case "Unknown":
		return "\033[35m" + status + "\033[0m" // Magenta
	default:
		return status
	}
}

func renderTable(getPodsOutput, topPodsOutput, namespace string) {
	topPodsUsage := parseTopPods(strings.Split(topPodsOutput, "\n"))
	lines := strings.Split(getPodsOutput, "\n")

	table := tablewriter.NewWriter(os.Stdout)
	if namespace == "--all-namespaces" {
		table.SetHeader([]string{"NAMESPACE", "NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"})
	} else {
		table.SetHeader([]string{"NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"})
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "NAMESPACE") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		podNamespace := fields[0]
		name := fields[1]
		status := colorStatus(fields[2])
		restarts := fields[3]
		ageRaw := strings.Join(fields[4:], " ")
		age := formatAge(ageRaw)

		cpu := "N/A"
		memory := "N/A"
		if nsUsage, exists := topPodsUsage[podNamespace]; exists {
			if usage, found := nsUsage[name]; found {
				cpu = usage[0]
				memory = usage[1]
			}
		}

		if namespace == "--all-namespaces" {
			table.Append([]string{podNamespace, name, cpu, memory, status, restarts, age})
		} else if namespace == podNamespace {
			table.Append([]string{name, cpu, memory, status, restarts, age})
		}
	}

	table.SetBorder(true)
	table.SetRowLine(false)
	table.Render()
}
