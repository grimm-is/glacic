//go:build linux
// +build linux

package firewall

import (
	"fmt"
	"os/exec"
	"strings"
)

// Run executes a command without capturing output.
func (r *RealCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command %s failed: %w: %s", name, err, string(out))
	}
	return nil
}

// Output executes a command and returns its output.
func (r *RealCommandRunner) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// RunInput executes a command with input via stdin.
func (r *RealCommandRunner) RunInput(input string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(input)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command %s failed: %w: %s", name, err, string(out))
	}
	return nil
}
