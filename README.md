# Kubectl Monitor

Kubectl Monitor is a CLI tool designed to enhance the experience of monitoring Kubernetes pods. It allows you to filter pods by namespace, display resource usage (CPU, memory), and provide a clear and colorful table format for improved readability.

## Features

- Filter pods by namespace using `--namespace` flag.
- Display CPU and memory usage in a visually enhanced table.
- Automatically calculates the age of each pod in human-readable format.
- Easy to use and integrates with your current Kubernetes configuration.

## Installation

### Prerequisites

- [Go](https://golang.org/doc/install) installed on your system.
- `kubectl` configured and working.

### Build from Source

1. Clone the repository:
   ```bash
   git clone https://github.com/renancavalcantercb/kubectl-monitor.git
   cd kubectl-monitor
   ```

2. Build the binary:
   ```bash
   go build -o kubectl-monitor main.go
   ```

3. Move the binary to your PATH:
   ```bash
   sudo mv kubectl-monitor /usr/local/bin
   ```

4. Test the tool:
   ```bash
   kubectl-monitor --namespace default
   ```

### Easy Installation Script

For easier installation, use the provided `install.sh` script:

1. Clone the repository:
   ```bash
   git clone https://github.com/renancavalcantercb/kubectl-monitor.git
   cd kubectl-monitor
   ```

2. Run the install script:
   ```bash
   ./install.sh
   ```

3. Verify installation:
   ```bash
   kubectl-monitor --namespace default
   ```

## Usage

To use the tool, simply run:

- To filter by namespace:
  ```bash
  kubectl-monitor --namespace your-namespace
  ```

- To list all pods across namespaces:
  ```bash
  kubectl-monitor
  ```

### Example Output

```bash
NAMESPACE   NAME                            CPU    MEMORY   STATUS    RESTARTS   AGE
default     example-pod-1234abcd           100m   150Mi    Running   1          5d 2h 10m
kube-system kube-proxy-5678efgh            50m    100Mi    Running   0          10d 4h 30m
```

## Uninstallation

To remove `kubectl-monitor`, simply delete the binary:

```bash
sudo rm /usr/local/bin/kubectl-monitor
```