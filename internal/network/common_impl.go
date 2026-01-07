package network

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DefaultSystemController is the default RealSystemController instance.
var DefaultSystemController SystemController = &RealSystemController{}

// DefaultCommandExecutor is the default RealCommandExecutor instance.
var DefaultCommandExecutor CommandExecutor = &RealCommandExecutor{}

// RealSystemController is a concrete implementation of SystemController using os functions.
type RealSystemController struct{}

// ReadSysctl reads a sysctl value from the specified path.
func (r *RealSystemController) ReadSysctl(path string) (string, error) {
	// If path doesn't start with /, convert dotted notation to /proc/sys/ path
	if !strings.HasPrefix(path, "/") {
		path = "/proc/sys/" + strings.ReplaceAll(path, ".", "/")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteSysctl writes a sysctl value to the specified path.
func (r *RealSystemController) WriteSysctl(path, value string) error {
	// If path doesn't start with /, convert dotted notation to /proc/sys/ path
	if !strings.HasPrefix(path, "/") {
		path = "/proc/sys/" + strings.ReplaceAll(path, ".", "/")
	}
	// On macOS or non-Linux, this might fail if path doesn't exist, which is expected/handled
	return os.WriteFile(path, []byte(value), 0644)
}

// IsNotExist checks if an error indicates that a file or directory does not exist.
func (r *RealSystemController) IsNotExist(err error) bool {
	return os.IsNotExist(err)
}

// RealCommandExecutor is a concrete implementation of CommandExecutor using os/exec.
type RealCommandExecutor struct{}

// RunCommand runs a command and returns its stdout.
func (r *RealCommandExecutor) RunCommand(name string, arg ...string) (string, error) {
	cmd := exec.Command(name, arg...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command %s %v failed: %w, output: %s", name, arg, err, string(output))
	}
	return string(output), nil
}
