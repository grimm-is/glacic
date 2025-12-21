# Toolbox Test Orca Architecture

This document describes the architecture of Glacic's integration test framework, which orchestrates parallel test execution across multiple QEMU virtual machines.

## Overview

The toolbox is a **busybox-style multi-binary** that provides test orchestration on the host and test execution inside VMs. A single Go binary (`build/toolbox`) dispatches to different subsystems based on `argv[0]` or an explicit subcommand.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              HOST (macOS/Linux)                              │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │  Orca (build/toolbox orca test)                                       │   │
│  │                                                                       │   │
│  │  ┌─────────────┐    ┌─────────────┐         ┌─────────────────────┐  │   │
│  │  │   Pod       │    │ TestJob     │────────>│  TestResult         │  │   │
│  │  │  Manager    │    │  Queue      │         │   Channel           │  │   │
│  │  └──────┬──────┘    └─────────────┘         └─────────────────────┘  │   │
│  │         │                                                             │   │
│  │  ┌──────┴────────────────────────────────────────────────────────┐   │   │
│  │  │                       Worker Pod                              │   │   │
│  │  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐           │   │   │
│  │  │  │Worker 1 │  │Worker 2 │  │Worker 3 │  │Worker N │           │   │   │
│  │  │  │ (VM 1)  │  │ (VM 2)  │  │ (VM 3)  │  │ (VM N)  │           │   │   │
│  │  │  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘           │   │   │
│  │  └───────│────────────│────────────│────────────│────────────────┘   │   │
│  │          │unix sock   │unix sock   │unix sock   │unix sock           │   │
│  └──────────│────────────│────────────│────────────│────────────────────┘   │
│             │            │            │            │                         │
│        ┌────▼────┐  ┌────▼────┐  ┌────▼────┐  ┌────▼────┐                   │
│        │ QEMU 1  │  │ QEMU 2  │  │ QEMU 3  │  │ QEMU N  │  (virtio-serial)  │
│        │ (Agent) │  │ (Agent) │  │ (Agent) │  │ (Agent) │                   │
│        └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘                   │
└─────────────┼────────────┼────────────┼────────────┼────────────────────────┘
              │            │            │            │
┌─────────────▼────────────▼────────────▼────────────▼────────────────────────┐
│                              GUEST VM (Alpine Linux)                         │
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  Agent (build/toolbox-linux agent)                                     │  │
│  │                                                                        │  │
│  │  • Connects to /dev/virtio-ports/glacic.agent                          │  │
│  │  • Receives TEST commands from orca                                    │  │
│  │  • Executes test scripts, streams TAP output                           │  │
│  │  • Project mounted at /mnt/glacic (virtfs)                             │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Package Structure

```
internal/toolbox/
├── orca/                   # Host-side test coordination
│   ├── orca.go             # Entry point, CLI dispatch
│   ├── pod.go              # VM pod management, test distribution
│   └── vm.go               # QEMU VM lifecycle management
├── harness/                # TAP protocol handling
│   ├── harness.go          # Stub for prove-like harness
│   └── tap.go              # TAP parser implementation
└── agent/                  # In-VM test executor
    └── agent.go            # Command protocol handler
```

---

## Components

### 1. Orca (`orca/orca.go`)

**Entry point** for the host-side toolbox. Dispatches to `run` (interactive VM) or `test` (parallel test execution).

```go
orca.Run(args []string) error
// - "run"  → runVM()      // Single interactive VM
// - "test" → runTests()   // Parallel test execution
```

**Configuration** (passed to VMs):

| Field           | Description                                      |
|-----------------|--------------------------------------------------|
| `KernelPath`    | Path to `build/vmlinuz`                          |
| `InitrdPath`    | Path to `build/initramfs`                        |
| `RootfsPath`    | Path to `build/rootfs.ext4`                      |
| `ProjectRoot`   | Mounted as virtfs share at `/mnt/glacic`         |
| `SocketPath`    | Unix socket for virtio-serial communication      |
| `Debug`         | Enable console output passthrough                |

---

### 2. Pod Manager (`orca/pod.go`)

Manages a pod of QEMU VMs for **parallel test execution**.

#### Key Types

```go
type Pod struct {
    config   Config
    size     int               // Number of parallel VMs
    workers  []*worker         // Active VM workers
    jobs     chan TestJob      // Pending tests
    results  chan TestResult   // Completed tests
    ctx      context.Context
    wg       sync.WaitGroup
}

type TestJob struct {
    ScriptPath string  // e.g., "t/01-sanity/api_test.sh"
}

type TestResult struct {
    Job      TestJob
    Suite    *harness.TestSuite  // Parsed TAP output
    Duration time.Duration
    Error    error
}
```

#### Lifecycle

```
NewPod(cfg, size) → pod.Start() → pod.Submit(job) → pod.Results() → pod.Stop()
                          │                                   │
                    Spawns N VMs                         Blocks for
                    Waits for agent                      test results
```

#### Worker Implementation

Each worker:
1. Creates a unique overlay image for its VM (avoids disk conflicts)
2. Starts QEMU with unique socket path (`/tmp/glacic-vm{id}.sock`)
3. Waits for agent handshake (`HELLO AGENT_v1`)
4. Pulls jobs from shared queue, sends `TEST <path>` commands
5. Reads TAP output until `TAP_END`, parses with harness

---

### 3. VM Manager (`orca/vm.go`)

Wraps QEMU process lifecycle.

#### QEMU Configuration

```go
args := []string{
    "-machine", "virt,accel=hvf",  // macOS ARM64 virtualization
    "-cpu", "cortex-a72",
    "-smp", "2", "-m", "1G",
    "-nographic", "-no-reboot",
    
    // Boot
    "-kernel", kernelPath,
    "-initrd", initrdPath,
    "-append", "root=/dev/vda rw console=ttyAMA0 agent_mode=true",
    
    // Disks (overlay for isolation)
    "-drive", "file=overlay.qcow2,format=qcow2,if=virtio",
    
    // Host filesystem share
    "-virtfs", "local,path=<project>,mount_tag=host_share",
    
    // Virtio-serial for agent communication
    "-device", "virtio-serial-pci",
    "-chardev", "socket,path=/tmp/glacic-vm1.sock,server=on,wait=off",
    "-device", "virtserialport,chardev=channel0,name=glacic.agent",
}
```

**Parallel VM Isolation:**
- Unique overlay qcow2 per VM (COW from shared rootfs)
- Unique socket path per VM
- No host port forwarding (avoids conflicts)

---

### 4. TAP Parser (`harness/tap.go`)

Parses [Test Anything Protocol](https://testanything.org/) output from test scripts.

#### Supported TAP Elements

| Pattern | Example | Purpose |
|---------|---------|---------|
| `1..N` | `1..8` | Test plan (expected count) |
| `ok N - desc` | `ok 1 - loopback exists` | Passed test |
| `not ok N - desc` | `not ok 2 - missing tool` | Failed test |
| `# comment` | `# Running checks...` | Diagnostic |
| `# SKIP reason` | `# SKIP: no dhcpd` | Skipped test |
| `# TODO reason` | `# TODO: not implemented` | Expected failure |
| `TAP_START name` | `TAP_START sanity_test.sh` | Orca marker |
| `TAP_END name exit=N` | `TAP_END sanity_test.sh exit=0` | Orca marker |

#### TestSuite Output

```go
type TestSuite struct {
    Name        string
    PlanCount   int
    Results     []TestResult
    ExitCode    int
}

func (s *TestSuite) Summary() (passed, failed, skipped int)
func (s *TestSuite) Success() bool
```

---

### 5. Agent (`agent/agent.go`)

Runs **inside the VM** to execute tests on behalf of the orca.

#### Command Protocol

| Command | Format | Response |
|---------|--------|----------|
| Handshake | — | `HELLO AGENT_v1` (sent on connect) |
| `PING` | `PING` | `PONG` |
| `EXEC <cmd>` | `EXEC ip addr` | `--- BEGIN OUTPUT ---` ... `--- END OUTPUT (exit=N) ---` |
| `TEST <path>` | `TEST t/01-sanity/sanity_test.sh` | `TAP_START` ... TAP output ... `TAP_END exit=N` |
| `SHELL` | `SHELL` | Interactive shell session |
| `EXIT` | `EXIT` | `BYE` (closes connection) |

#### Agent Startup

1. Detects virtio-serial port (`/dev/virtio-ports/glacic.agent` or `/dev/vport0p1`)
2. Opens port for bidirectional communication
3. Sends `HELLO AGENT_v1` handshake
4. Enters command loop, processes orca requests

---

## Test Discovery

Tests are discovered by walking `t/` for `*_test.sh` files:

```go
func DiscoverTests(projectRoot string) ([]TestJob, error) {
    // Walks t/ looking for *_test.sh
    // Returns TestJob{ScriptPath: "t/01-sanity/api_test.sh"}
}
```

Current test structure:
```
t/
├── 01-sanity/      # Basic environment checks
├── 10-api/         # API endpoint tests
├── 20-dhcp/        # DHCP service tests
├── 25-dns/         # DNS service tests
├── 30-firewall/    # Firewall rule tests
├── 40-network/     # Network behavior tests
├── 50-security/    # Security feature tests
└── ...
```

---

## CLI Dispatch (Busybox Style)

The toolbox uses `argv[0]` to determine which subsystem to invoke:

```
cmd/toolbox/main.go
├── argv[0] = "agent"             → agent.Run()
├── argv[0] = "orca"              → orca.Run()
├── argv[0] = "prove"             → harness.Run()
└── argv[0] = "toolbox"           → toolbox <subcommand>
```

Build creates symlinks:
```bash
make build-toolbox
# Creates:
#   build/toolbox           → host orca
#   build/toolbox-linux     → guest agent (static Linux binary)
#   build/glacic-orca       → symlink to toolbox
#   build/agent             → symlink to toolbox-linux
```

---

## Makefile Integration

| Target | Description |
|--------|-------------|
| `make build-toolbox` | Build host + guest toolbox binaries |
| `make test-orca` | Run parallel tests via orca |
| `make test-int` | Run tests via legacy shell runner |

---

## Data Flow Example

```
1. User runs: make test-orca
                    │
2. build/toolbox orca test
                    │
3. DiscoverTests() → ["t/01-sanity/sanity_test.sh", "t/10-api/api_test.sh", ...]
                    │
4. NewPod(cfg, 2).Start() → spawns 2 QEMU VMs
                    │
5. Workers wait for agent handshake on /tmp/glacic-vm{1,2}.sock
                    │
6. VM boots → agent starts → connects to virtio port → "HELLO AGENT_v1"
                    │
7. Pod.Submit(job) for each test → jobs distributed to idle workers
                    │
8. Worker sends: TEST t/01-sanity/sanity_test.sh\n
                    │
9. Agent executes: /bin/sh /mnt/glacic/t/01-sanity/sanity_test.sh
                    │
10. Agent streams: TAP_START sanity_test.sh
                   1..8
                   ok 1 - loopback interface exists
                   ok 2 - test interfaces present
                   ...
                   TAP_END sanity_test.sh exit=0
                    │
11. Worker parses TAP → TestResult{Suite: ..., Duration: ...}
                    │
12. Pod.Results() receives results → orca prints summary
                    │
13. Pod.Stop() → sends EXIT to agents → stops QEMU processes
```

---

## Future Improvements

- [ ] Parse `-j N` flag for configurable parallelism (Done)
- [ ] Test filtering by name/path pattern
- [ ] Persistent VM pod for faster iteration (keep VMs warm)
- [ ] Structured result reporting (JSON/JUnit XML)
- [ ] Timeout handling at job level
