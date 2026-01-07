package vmm

type Config struct {
	KernelPath      string
	InitrdPath      string
	RootfsPath      string
	ProjectRoot     string
	Debug           bool
	ConsoleOutput   bool
	RunSkipped      bool // Force normally-skipped tests to run
	Verbose         bool // Show detailed status messages
	StrictIsolation bool // Kill workers after every test (no reuse)
	Trace           bool // Log all JSONL protocol messages
}
