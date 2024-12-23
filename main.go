package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
)

func main() {
	namespace := parseArgs(os.Args)
	output, err := executeKubectl(namespace)
	if err != nil {
		log.Fatalf("Error: %v\n", err)
	}
	renderTable(output)
}

func parseArgs(args []string) string {
	if len(args) > 1 && args[1] == "--namespace" {
		if len(args) > 2 {
			return args[2]
		}
		log.Fatalln("Error: Namespace not specified")
	}
	return "--all-namespaces"
}

func executeKubectl(namespace string) (string, error) {
	args := buildKubectlArgs(namespace)
	cmd := exec.Command("kubectl", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to execute kubectl: %w\nOutput: %s", err, out.String())
	}
	return out.String(), nil
}

func buildKubectlArgs(namespace string) []string {
	args := []string{
		"get", "pods",
		"-o", "custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,CPU:.spec.containers[*].resources.requests.cpu,MEMORY:.spec.containers[*].resources.requests.memory,STATUS:.status.phase,RESTARTS:.status.containerStatuses[*].restartCount,AGE:.metadata.creationTimestamp",
	}
	if namespace != "--all-namespaces" {
		args = append(args, "--namespace", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}
	return args
}

func renderTable(output string) {
	lines := strings.Split(output, "\n")
	if len(lines) <= 1 {
		fmt.Println("No data available")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"NAMESPACE", "NAME", "CPU", "MEMORY", "STATUS", "RESTARTS", "AGE"})
	table.SetBorder(true)

	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		namespace, name, cpu, memory, status, restarts := fields[0], fields[1], fields[2], fields[3], fields[4], fields[5]
		creationTimestamp := strings.Join(fields[6:], " ")
		age := calculateAge(creationTimestamp)

		coloredStatus := colorStatus(status)

		table.Append([]string{namespace, name, cpu, memory, coloredStatus, restarts, age})
	}

	table.Render()
}

func colorStatus(status string) string {
	switch status {
	case "Running":
		return "\033[32m" + status + "\033[0m"
	case "Pending":
		return "\033[33m" + status + "\033[0m"
	case "Failed":
		return "\033[31m" + status + "\033[0m"
	case "Unknown":
		return "\033[35m" + status + "\033[0m"
	default:
		return status
	}
}

func calculateAge(timestamp string) string {
	layout := time.RFC3339
	t, err := time.Parse(layout, timestamp)
	if err != nil {
		return "Invalid timestamp"
	}
	duration := time.Since(t)
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}
