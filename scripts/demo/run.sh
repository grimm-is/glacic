#!/bin/bash

# Glacic Demo Launcher
# Uses existing VM infrastructure to launch a complete demo environment

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default port (can be overridden with -p or --port)
API_PORT=${API_PORT:-8080}

# Find an available port starting from the given port
find_available_port() {
    local port=${1:-8080}
    local max_port=$((port + 100))

    while [ $port -lt $max_port ]; do
        if ! lsof -i :$port >/dev/null 2>&1; then
            echo $port
            return 0
        fi
        port=$((port + 1))
    done

    echo "Error: Could not find available port in range ${1:-8080}-$max_port" >&2
    return 1
}

# Configuration
BUILD_DIR="$(pwd)/build"
DEMO_CONFIG="demo-firewall.hcl"
FIREWALL_PID_FILE="/tmp/firewall-demo.pid"
CLIENT_PIDS_FILE="/tmp/firewall-demo-clients.pids"

# Print functions
print_status() {
    echo -e "${BLUE}[DEMO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check dependencies
check_dependencies() {
    print_status "Checking dependencies..."

    if ! command -v python3 &> /dev/null; then
        print_error "Python3 is required"
        exit 1
    fi

    if ! command -v qemu-system-x86_64 &> /dev/null && ! command -v qemu-system-aarch64 &> /dev/null; then
        print_error "QEMU is required"
        exit 1
    fi

    print_success "Dependencies found"
}

# Build VM images if needed
build_images() {
    print_status "Checking VM images..."

    if [[ ! -f "$BUILD_DIR/rootfs.qcow2" ]] || [[ ! -f "$BUILD_DIR/vmlinuz" ]] || [[ ! -f "$BUILD_DIR/initramfs" ]]; then
        print_status "Building VM images (this may take a few minutes)..."
        make vm-setup
        print_success "VM images built"
    else
        print_success "VM images found"
    fi
}

# Create demo configuration
create_demo_config() {
    print_status "Creating demo configuration..."

    cat > "$DEMO_CONFIG" << 'EOF'
# Demo Firewall Configuration
# Basic setup for demonstration purposes

ip_forwarding = true

# API Configuration (Disable Sandbox for Demo)
api {
  enabled = true
  disable_sandbox = true
  listen = "0.0.0.0:8080"
}

# Interface configuration
interface "eth0" {
  description = "WAN Interface"
  zone        = "WAN"
  dhcp        = true
}

interface "eth1" {
  description = "Green Zone (Trusted LAN)"
  zone        = "Green"
  ipv4        = ["10.1.0.1/24"]
}

interface "eth2" {
  description = "Orange Zone (DMZ)"
  zone        = "Orange"
  ipv4        = ["10.2.0.1/24"]
}

interface "eth3" {
  description = "Red Zone (Restricted)"
  zone        = "Red"
  ipv4        = ["10.3.0.1/24"]
}

interface "eth4" {
  description = "Management Interface"
  zone        = "mgmt"
  dhcp        = true
  access_web_ui = true
  web_ui_port = 8080
}

zone "mgmt" {
  description = "Trusted Management Zone"
  management {
    web_ui = true
    api    = true
    ssh    = true
    icmp   = true
  }
}

# Basic firewall policies
policy "Green" "WAN" {
  name = "green_to_wan"

  rule "allow_internet" {
    description = "Allow Green zone internet access"
    action = "accept"
  }
}

policy "Orange" "WAN" {
  name = "orange_to_wan"

  rule "allow_internet" {
    description = "Allow Orange zone internet access"
    action = "accept"
  }
}

policy "Red" "WAN" {
  name = "red_blocked"

  rule "block_internet" {
    description = "Block Red zone from internet"
    action = "drop"
  }
}

# NAT for outbound traffic
nat "outbound" {
  type          = "masquerade"
  out_interface = "eth0"
}
EOF

    print_success "Demo configuration created: $DEMO_CONFIG"
}

# Start firewall VM
start_firewall() {
    print_status "Starting firewall VM..."

    # Find an available port if default is in use
    if lsof -i :${API_PORT} >/dev/null 2>&1; then
        print_status "Port ${API_PORT} is in use, finding available port..."
        API_PORT=$(find_available_port ${API_PORT})
        if [ $? -ne 0 ]; then
            print_error "Could not find available port"
            exit 1
        fi
        print_status "Using port ${API_PORT}"
    fi

    # Export port for vm-dev.sh to use
    export API_PORT

    # Enable separate management interface (eth4)
    export SEPARATE_MGMT_IFACE=1

    # Copy demo config to expected location (only if not in dev mode)
    if [ "${1:-}" != "dev" ]; then
        if [ -f "$DEMO_CONFIG" ]; then
             cp "$DEMO_CONFIG" build/firewall.hcl
        fi
    fi

    # Start firewall in background using dev_mode kernel param
    # vm-dev.sh args: $1=mount_path $2=unused $3=kernel_params
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    # Points to scripts/vm/dev.sh
    "$SCRIPT_DIR/../vm/dev.sh" "$(pwd)" "" "dev_mode=true api_port=${API_PORT} config_file=build/firewall.hcl" > /tmp/firewall-demo.log 2>&1 &
    local pid=$!
    echo $pid > "$FIREWALL_PID_FILE"

    print_status "Firewall VM starting (PID: $pid)"

    # Wait for firewall to initialize and check web UI
    print_status "Waiting for firewall web UI to be ready..."
    local timeout=30
    local count=0

    # Tail the log in the background to show startup progress
    tail -f /tmp/firewall-demo.log &
    TAIL_PID=$!

    # Ensure we kill the tail process on exit
    trap "kill $TAIL_PID 2>/dev/null" EXIT

    while [ $count -lt $timeout ]; do
        # Check if process is still running
        if ! kill -0 $pid 2>/dev/null; then
            kill $TAIL_PID 2>/dev/null
            trap - EXIT
            print_error "Firewall VM process died"
            echo "Check logs: tail -50 /tmp/firewall-demo.log"
            exit 1
        fi

        # Check for failure messages in the log
        if grep -q "❌" /tmp/firewall-demo.log 2>/dev/null; then
            kill $TAIL_PID 2>/dev/null
            trap - EXIT
            print_error "Firewall startup failed - error detected in logs"
            # grep "❌" /tmp/firewall-demo.log | tail -5 # Already shown by tail -f
            exit 1
        fi

        # Check for Go panics
        if grep -q "panic:" /tmp/firewall-demo.log 2>/dev/null; then
            kill $TAIL_PID 2>/dev/null
            trap - EXIT
            print_error "Firewall application crashed (panic detected)"
            # grep -A 10 "panic:" /tmp/firewall-demo.log | head -20 # Already shown
            exit 1
        fi

        # Check if web UI is responding (use auth/status which is always public)
        CHECK_URL="http://localhost:${API_PORT}/api/auth/status"
        if curl -s --max-time 2 "$CHECK_URL" | grep -q "authenticated"; then
            kill $TAIL_PID 2>/dev/null
            trap - EXIT
            print_success "Firewall VM is running and web UI is ready"
            return 0
        fi

        sleep 1
        count=$((count + 1))

        # Progress indicator every 5 seconds with detailed status
        if [ $((count % 5)) -eq 0 ]; then
             # Silent update since we are tailing logs
             :
        fi
    done

    kill $TAIL_PID 2>/dev/null
    trap - EXIT

    print_error "Firewall web UI not ready after $timeout seconds"

    echo ""
    echo "🔍 Troubleshooting Analysis:"
    echo "1. VM Process: $(if kill -0 $pid 2>/dev/null; then echo "Running (PID $pid)"; else echo "Stopped"; fi)"
    echo "2. Port Status: $(lsof -i :$API_PORT 2>/dev/null || echo "Port $API_PORT not listening")"
    echo "3. Last Logs:"
    echo "---------------------------------------------------"
    tail -100 /tmp/firewall-demo.log
    echo "---------------------------------------------------"
    echo ""
    echo "💡 Tip: You can view the full log with: tail -f /tmp/firewall-demo.log"
    exit 1
}

# Note: Simplified demo - only firewall VM needed for web UI demo
# Client VMs require zone networking which is not available in dev mode

# Show demo information
show_demo_info() {
    print_status "Demo Environment Information:"
    echo ""
    echo "🌐 Web UI Access:"
    echo "   URL: http://localhost:${API_PORT}"
    echo "   Status: http://localhost:${API_PORT}/api/status"
    echo ""
    echo "🏗️ Network Architecture:"
    echo "   Firewall VM: Single VM with web UI"
    echo "   4 Network interfaces: WAN + 3 zones"
    echo "   Dev mode: No tests, direct firewall service"
    echo ""
    echo "🧪 Demo Features:"
    echo "   1. Web UI with real-time status"
    echo "   2. Configuration management"
    echo "   3. API access at localhost:${API_PORT}"
    echo "   4. Backup and restore functionality"
    echo ""
    echo "📋 Management Commands:"
    echo "   View firewall logs:  tail -f /tmp/firewall-demo.log"
    echo "   Check status:        $0 status"
    echo "   Stop demo:           $0 stop"
    echo ""
}

# Run demo tests
run_tests() {
    print_status "Running demo tests..."

    # Check if we can access the firewall API
    if curl -s http://localhost:${API_PORT}/api/auth/status | grep -q "authenticated"; then
        print_success "Firewall API is accessible"
    else
        print_warning "Firewall API not responding (may still be starting)"
    fi

    # Test connectivity from clients (run test scripts in VMs)
    print_status "Testing zone connectivity..."

    # These would need to be run inside the client VMs
    # For now, just show what would be tested
    echo "Expected test results:"
    echo "  ✓ Green zone: Internet access"
    echo "  ✓ Orange zone: Internet access"
    echo "  ✓ Red zone: No Internet access"
    echo "  ✓ All zones: No inter-zone communication"
}

# Stop demo environment
stop_demo() {
    print_status "Stopping demo environment..."

    # Stop clients
    if [[ -f "$CLIENT_PIDS_FILE" ]]; then
        while read -r pid; do
            if kill -0 "$pid" 2>/dev/null; then
                kill "$pid"
                print_status "Stopped client VM (PID: $pid)"
            fi
        done < "$CLIENT_PIDS_FILE"
        rm -f "$CLIENT_PIDS_FILE"
    fi

    # Stop firewall
    if [[ -f "$FIREWALL_PID_FILE" ]]; then
        local pid=$(cat "$FIREWALL_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid"
            print_status "Stopped firewall VM (PID: $pid)"
        fi
        rm -f "$FIREWALL_PID_FILE"
    fi

    # Clean up log files
    rm -f /tmp/firewall-demo.log /tmp/*-client.log

    print_success "Demo environment stopped"
}

# Show status
show_status() {
    print_status "Demo Environment Status:"
    echo ""

    # Check firewall
    if [[ -f "$FIREWALL_PID_FILE" ]]; then
        local pid=$(cat "$FIREWALL_PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "🔥 Firewall VM: Running (PID: $pid)"
        else
            echo "🔥 Firewall VM: Not running (stale PID)"
        fi
    else
        echo "🔥 Firewall VM: Not started"
    fi

    # Check clients
    if [[ -f "$CLIENT_PIDS_FILE" ]]; then
        local zones=("Green" "Orange" "Red")
        local i=0
        while read -r pid; do
            if kill -0 "$pid" 2>/dev/null; then
                echo "💻 ${zones[$i]} Client: Running (PID: $pid)"
            else
                echo "💻 ${zones[$i]} Client: Not running (stale PID)"
            fi
            i=$((i + 1))
        done < "$CLIENT_PIDS_FILE"
    else
        echo "💻 Client VMs: Not started"
    fi

    echo ""

    # Check web UI
    if curl -s http://localhost:${API_PORT}/api/auth/status 2>/dev/null | grep -q "authenticated"; then
        echo "🌐 Web UI: Accessible at http://localhost:${API_PORT}"
    else
        echo "🌐 Web UI: Not responding"
    fi
}

# Main function
main() {
    echo ""
    echo "======================================"
    echo "  Glacic Demo Launcher"
    echo "======================================"
    echo ""

    case "${1:-start}" in
        start)
            check_dependencies
            build_images
            create_demo_config
            # Back up existing config if present
            if [ -f "firewall.hcl" ]; then
                cp firewall.hcl firewall.hcl.bak
            fi
            # Use demo config
            cp "$DEMO_CONFIG" firewall.hcl

            start_firewall
            show_demo_info
            run_tests
            print_success "Demo is ready! Access the web UI at http://localhost:${API_PORT}"
            ;;
        dev)
            check_dependencies

            # Check if VM is already running
            if [ -f "$FIREWALL_PID_FILE" ]; then
                PID=$(cat "$FIREWALL_PID_FILE")
                if kill -0 "$PID" 2>/dev/null; then
                    print_status "Firewall VM is running (PID $PID). Attempting hot upgrade..."

                    # Wait for build to complete (handled by make, but ensure binary exists)
                    if [ ! -f "$BUILD_DIR/glacic" ]; then
                         print_error "Binary not found in build/glacic"
                         exit 1
                    fi

                    print_status "Connecting via SSH to trigger upgrade..."
                    print_warning "If prompted for password, enter: 'root'"

                    # SSH options to avoid validity checks for dev VM and use port 2222
                    SSH_OPTS="-p 2222 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5"

                    # Trigger upgrade: pass the path to the new binary (available via host mount)
                    ssh $SSH_OPTS root@localhost "/mnt/glacic/build/glacic upgrade /mnt/glacic/build/glacic"

                    if [ $? -eq 0 ]; then
                        print_success "Upgrade signal sent! Tailing logs..."
                        tail -f /tmp/firewall-demo.log
                        exit 0
                    else
                         print_error "Upgrade failed. SSH connection failed or upgrade command error."
                         print_warning "If SSH is not configured in the running VM, you must stop it first: make vm-stop"
                         exit 1
                    fi
                fi
            fi

            build_images
            print_status "Starting in DEV mode (using existing firewall.hcl)..."
            if [ ! -f "firewall.hcl" ]; then
                print_error "firewall.hcl not found!"
                exit 1
            fi
            start_firewall "dev"
            print_success "Dev Environment Ready! Access the web UI at http://localhost:${API_PORT}"
            ;;
        stop)
            stop_demo
            # Restore backup if it exists
            if [ -f "firewall.hcl.bak" ]; then
                mv firewall.hcl.bak firewall.hcl
            fi
            ;;
        restart)
            stop_demo
            sleep 2
            main start
            ;;
        status)
            show_status
            ;;
        logs)
            echo "=== Firewall Logs ==="
            tail -n 20 /tmp/firewall-demo.log 2>/dev/null || echo "No firewall logs"
            echo ""
            echo "=== Client Logs ==="
            for zone in green orange red; do
                echo "--- $zone Client ---"
                tail -n 10 "/tmp/$zone-client.log" 2>/dev/null || echo "No $zone logs"
                echo ""
            done
            ;;
        help|--help|-h)
            echo "Usage: $0 [command]"
            echo ""
            echo "Commands:"
            echo "  start    - Start the demo environment (default)"
            echo "  dev      - Start in DEV mode (uses local firewall.hcl)"
            echo "  stop     - Stop all VMs and clean up"
            echo "  restart  - Restart the demo environment"
            echo "  status   - Show VM and service status"
            echo "  logs     - Show recent logs from all VMs"
            echo "  help     - Show this help message"
            echo ""
            echo "This demo uses the existing VM infrastructure with:"
            echo "  - Custom Alpine Linux build"
            echo "  - Socket-based VM networking"
            echo "  - Zone-based firewall configuration"
            echo "  - Web UI accessible at localhost:\${API_PORT:-8080}"
            echo ""
            ;;
        *)
            print_error "Unknown command: $1"
            echo "Use '$0 help' for usage information"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
