---
description: Common errors and how to fix them
---

# Troubleshooting Guide

Quick solutions for common issues when developing Glacic.

## Build Errors

### "cannot find package"

```
go: cannot find package "grimm.is/glacic/internal/foo"
```

**Fix**: Run `go mod tidy` to sync dependencies.

### Cross-compilation failure

```
GOOS=linux CGO_ENABLED=1 go build: requires cgo
```

**Fix**: Use `CGO_ENABLED=0` for pure Go builds (most features work without CGO).

## Test Errors

### "API server unreachable"

```
not ok - FATAL: API server unreachable
```

**Causes**:
1. `ctl` process not running
2. `api` process crashed
3. Socket not ready

**Fix**:
```bash
# In VM, check processes
ps aux | grep glacic

# Check socket exists
ls -la /run/glacic/

# Increase startup wait in test
wait_for_api 15  # Wait 15 seconds instead of default
```

### "Permission denied" in VM

```
/mnt/glacic/t/...: Permission denied
```

**Fix**: Run as root inside VM:
```bash
sudo -i
cd /mnt/glacic
./t/10-api/some_test.sh
```

### "nft: command not found"

**Fix**: Ensure nftables is installed in VM:
```bash
apk add nftables
```

## VM Issues

### VM won't start

```
qemu-system-x86_64: error: ...
```

**Causes**:
1. Another VM using same ports
2. Missing VM image
3. KVM not available

**Fix**:
```bash
# Kill existing VMs
make vm-stop

# Rebuild image
make clean-all
make vm-setup

# Check if KVM available (optional, VM will be slow without)
ls /dev/kvm
```

### Can't SSH to VM

```
ssh: connect to host localhost port 2222: Connection refused
```

**Fix**: Wait for VM boot or check if running:
```bash
# Check VM is running
pgrep -f qemu

# Manual SSH (wait for boot)
sleep 30
ssh -p 2222 root@localhost
```

### VM boot hangs

**Causes**:
1. Kernel panic
2. Init script issue

**Fix**: Check VM console output:
```bash
# Run with visible console
VISIBLE=1 make vm-start
```

## Configuration Errors

### "HCL parse error"

```
Error: Missing required argument "zone"
```

**Fix**: Check HCL syntax:
```bash
glacic check -v /path/to/config.hcl
```

### "unknown config field"

**Fix**: Check spelling and schema version. Run:
```bash
glacic config validate
```

## Network Errors

### "no route to host" from API sandbox

**Cause**: API sandbox network namespace issue.

**Fix**:
```bash
# Check veth pair exists
ip link show glacic-api

# Restart ctl to recreate
kill $(pgrep -f "glacic ctl")
glacic ctl /etc/glacic/glacic.hcl &
```

### Firewall blocking expected traffic

**Debug**:
```bash
# Check nftables rules
nft list ruleset

# Watch drops in realtime
nft monitor trace

# Check counters
nft list chain inet glacic input
```

## Code Issues

### "undefined: SomeType"

**Cause**: Missing import or type in wrong package.

**Fix**: Check `internal/ctlplane/types.go` for RPC types.

### "connection refused" on RPC

**Cause**: Socket path mismatch.

**Fix**: Use `ctlplane.GetSocketPath()` instead of hardcoded path.

## Where to Get Help

1. Check logs: `glacic log -f` or `journalctl -u glacic`
2. Enable debug: `GLACIC_DEBUG=1 glacic ctl ...`
3. Check ARCHITECTURE.md for system design
4. Check docs/dev/ for development guides
