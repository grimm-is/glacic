#!/bin/sh
# Calculates optimal number of parallel runners based on CPU and RAM
# Assumes each runner requires ~1 CPU and ~512MB RAM

get_cpu_count() {
    if command -v nproc >/dev/null 2>&1; then
        nproc
    elif command -v sysctl >/dev/null 2>&1; then
        sysctl -n hw.ncpu 2>/dev/null || echo 2
    else
        echo 2
    fi
}

get_mem_mb() {
    if [ -f /proc/meminfo ]; then
        # Linux
        awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo
    elif command -v sysctl >/dev/null 2>&1; then
        # macOS (hw.memsize is in bytes)
        BYTES=$(sysctl -n hw.memsize 2>/dev/null)
        echo $((BYTES / 1024 / 1024))
    else
        echo 2048 # Default 2GB
    fi
}

CPUS=$(get_cpu_count)
MEM_MB=$(get_mem_mb)

# VM requirement
VM_RAM_MB=512

# Calculate max jobs based on RAM
MAX_JOBS_RAM=$((MEM_MB / VM_RAM_MB))

# Determine bottleneck
JOBS=$CPUS
if [ "$MAX_JOBS_RAM" -lt "$JOBS" ]; then
    JOBS=$MAX_JOBS_RAM
fi

# Ensure at least 1, cap at 16 (arbitrary safety limit)
if [ "$JOBS" -lt 1 ]; then JOBS=1; fi
if [ "$JOBS" -gt 16 ]; then JOBS=16; fi

# Reserve 1 core for host OS if we have > 2 cores
if [ "$CPUS" -gt 2 ]; then
   if [ "$JOBS" -eq "$CPUS" ]; then
       JOBS=$((JOBS - 1))
   fi
fi

echo "$JOBS"
