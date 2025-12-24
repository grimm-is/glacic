#!/bin/bash

# Demo Launcher for Glacic
# Sets up a complete network environment with clients and web UI

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
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

# Check if docker and docker-compose are installed
check_dependencies() {
    print_status "Checking dependencies..."

    if ! command -v docker &> /dev/null; then
        print_error "Docker is not installed. Please install Docker first."
        exit 1
    fi

    if ! command -v docker-compose &> /dev/null; then
        print_error "Docker Compose is not installed. Please install Docker Compose first."
        exit 1
    fi

    print_success "Dependencies found"
}

# Create demo config directory
create_configs() {
    print_status "Creating demo configuration..."

    mkdir -p configs

    # Create a basic firewall config for demo
    cat > configs/firewall.hcl << 'EOF'
# Demo Firewall Configuration
# This sets up basic routing and firewall rules

# Basic settings
ip_forwarding = true

# Interfaces configuration
interface "eth0" {
  description = "WAN Interface (Internet)"
  zone = "wan"
}

interface "eth1" {
  description = "DMZ Interface (Management)"
  zone = "dmz"
  ip_address = "192.168.100.10/24"
  gateway = "192.168.100.1"
}

interface "eth2" {
  description = "LAN Interface (Internal Network)"
  zone = "lan"
  ip_address = "192.168.1.1/24"
}

# DHCP for LAN clients
dhcp "lan" {
  interface = "eth2"
  subnet = "192.168.1.0/24"
  range_start = "192.168.1.100"
  range_end = "192.168.1.200"
  lease_time = "12h"
  dns_servers = ["8.8.8.8", "8.8.4.4"]
}

# Basic firewall policies
policy "lan-to-wan" {
  description = "Allow LAN to access Internet"
  from_zone = "lan"
  to_zone = "wan"
  action = "accept"
}

policy "wan-to-lan" {
  description = "Block unsolicited WAN to LAN"
  from_zone = "wan"
  to_zone = "lan"
  action = "drop"
}

policy "dmz-access" {
  description = "Allow Web UI access from DMZ"
  from_zone = "dmz"
  to_zone = "self"
  action = "accept"
  rules {
    protocol = "tcp"
    destination_port = "80,443"
    action = "accept"
  }
}

# NAT for outbound traffic
nat "outbound" {
  description = "Masquerade LAN traffic to WAN"
  type = "masquerade"
  source_zone = "lan"
  destination_zone = "wan"
}

# DNS forwarding
dns {
  listen_zones = ["lan", "dmz"]
  upstream_servers = ["8.8.8.8", "8.8.4.4"]
}
EOF

    print_success "Demo configuration created"
}

# Build and start containers
start_demo() {
    print_status "Building and starting demo environment..."

    # Stop any existing demo
    docker-compose -f docker-compose.demo.yml down 2>/dev/null || true

    # Build the firewall image
    print_status "Building firewall image..."
    docker build -t firewall-router:demo .

    # Start all services
    print_status "Starting all services..."
    docker-compose -f docker-compose.demo.yml up -d

    print_success "Demo environment started"
}

# Wait for services to be ready
wait_for_services() {
    print_status "Waiting for services to initialize..."

    # Wait for firewall to start
    for i in {1..30}; do
        if docker logs firewall-demo 2>&1 | grep -q "Server starting" 2>/dev/null; then
            break
        fi
        if [ $i -eq 30 ]; then
            print_warning "Firewall may still be starting up..."
        fi
        sleep 2
    done

    # Wait a bit more for full initialization
    sleep 5
    print_success "Services initialized"
}

# Show network information
show_network_info() {
    print_status "Network Information:"
    echo ""
    echo "Web UI Access:"
    echo "  URL: http://localhost:8080"
    echo "  HTTPS: https://localhost:8443"
    echo ""
    echo "Network Layout:"
    echo "  WAN (203.0.113.0/24):"
    echo "    - Firewall: 203.0.113.2"
    echo "    - Web Server: 203.0.113.3"
    echo "    - iPerf Server: 203.0.113.4"
    echo ""
    echo "  DMZ (192.168.100.0/24):"
    echo "    - Firewall: 192.168.100.10"
    echo "    - Gateway: 192.168.100.1"
    echo ""
    echo "  LAN (192.168.1.0/24):"
    echo "    - Firewall: 192.168.1.1 (DHCP server)"
    echo "    - Client 1: 192.168.1.100"
    echo "    - Client 2: 192.168.1.101"
    echo ""
}

# Show test commands
show_test_commands() {
    print_status "Test Commands:"
    echo ""
    echo "# Access web UI"
    echo "curl http://localhost:8080"
    echo ""
    echo "# Test connectivity from clients"
    echo "docker exec demo-client1 ping -c 3 8.8.8.8"
    echo "docker exec demo-client1 curl http://203.0.113.3"
    echo ""
    echo "# Test firewall rules"
    echo "docker exec demo-client1 ping -c 3 203.0.113.3  # Should work"
    echo "docker exec demo-web-server ping -c 3 192.168.1.100  # Should fail"
    echo ""
    echo "# Performance testing"
    echo "docker exec demo-client1 iperf3 -c 203.0.113.4"
    echo ""
    echo "# View firewall logs"
    echo "docker logs -f firewall-demo"
    echo ""
    echo "# Access firewall shell (for debugging)"
    echo "docker exec -it firewall-demo /bin/sh"
    echo ""
}

# Cleanup function
cleanup() {
    print_status "Cleaning up demo environment..."
    docker-compose -f docker-compose.demo.yml down
    print_success "Demo environment stopped"
}

# Main function
main() {
    echo ""
    echo "======================================"
    echo "  Glacic Demo Launcher"
    echo "======================================"
    echo ""

    # Handle command line arguments
    case "${1:-start}" in
        start)
            check_dependencies
            create_configs
            start_demo
            wait_for_services
            show_network_info
            show_test_commands
            print_success "Demo is ready! Access the web UI at http://localhost:8080"
            ;;
        stop|cleanup)
            cleanup
            ;;
        restart)
            cleanup
            sleep 2
            main start
            ;;
        status)
            docker-compose -f docker-compose.demo.yml ps
            ;;
        logs)
            docker-compose -f docker-compose.demo.yml logs -f
            ;;
        help|--help|-h)
            echo "Usage: $0 [command]"
            echo ""
            echo "Commands:"
            echo "  start    - Start the demo environment (default)"
            echo "  stop     - Stop and remove all containers"
            echo "  restart  - Restart the demo environment"
            echo "  status   - Show container status"
            echo "  logs     - Show logs from all services"
            echo "  help     - Show this help message"
            echo ""
            ;;
        *)
            print_error "Unknown command: $1"
            echo "Use '$0 help' for usage information"
            exit 1
            ;;
    esac
}

# Trap Ctrl+C to cleanup
trap cleanup EXIT

# Run main function
main "$@"
