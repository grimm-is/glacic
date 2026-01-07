package network

// RunCommand runs a command and returns its stdout.
func RunCommand(name string, arg ...string) (string, error) {
	return DefaultCommandExecutor.RunCommand(name, arg...)
}
