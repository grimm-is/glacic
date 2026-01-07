#!/bin/bash
#
# Proxmox LXC Installation Script for Glacic
#
# Creates a privileged LXC container configured for firewall/router duties:
# - Full network stack access (nftables, routing, NAT)
# - Multiple network interfaces support
# - Persistent storage for config and logs
#
# Usage:
#   ./install-proxmox-lxc.sh [OPTIONS]
#
# Options:
#   --vmid ID          Container ID (default: next available)
#   --name NAME        Container name (default: firewall)
#   --storage STORE    Storage pool (default: local-lvm)
#   --memory MB        Memory in MB (default: 512)
#   --disk GB          Disk size in GB (default: 4)
#   --template FILE    Path to template (default: downloads Alpine)
#   --wan BRIDGE       WAN bridge, supports VLAN: vmbr0 or vmbr0.100 (default: vmbr0)
#   --lan BRIDGE       LAN bridge, supports VLAN: vmbr1 or vmbr0.200 (default: vmbr1)
#   --mgmt-ip IP/CIDR  Management IP (default: DHCP on WAN)
#   --dry-run          Show commands without executing
#
# Examples:
#   # Basic setup with separate bridges
#   ./install-proxmox-lxc.sh --vmid 100 --name firewall --wan vmbr0 --lan vmbr1
#
#   # Isolated testing with VLANs on same bridge
#   ./install-proxmox-lxc.sh --name fw-test --wan vmbr0.100 --lan vmbr0.200
#

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${BLUE}[INFO]${NC} $*" >&2; }
log_ok()    { echo -e "${GREEN}[OK]${NC} $*" >&2; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $*" >&2; }
log_error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# Default configuration
VMID=""
CT_NAME="glacic" # Changed default name
STORAGE="local-lvm"
MEMORY=512
DISK=4
TEMPLATE=""
WAN_BRIDGE="vmbr0"
LAN_BRIDGE="vmbr1"
MGMT_IP=""
DRY_RUN=false
ALPINE_VERSION="3.19"
INSTALL_PREFIX=""
STATE_DIR="/var/lib/glacic"
LOG_DIR="/var/log/glacic"
SSH_KEY="" # New option
CUSTOM_NETS_LIST=""

# Parse arguments
while [ $# -gt 0 ]; do
    case $1 in
        --vmid)      VMID="$2"; shift 2 ;;
        --name)      CT_NAME="$2"; shift 2 ;;
        --storage)   STORAGE="$2"; shift 2 ;;
        --memory)    MEMORY="$2"; shift 2 ;;
        --disk)      DISK="$2"; shift 2 ;;
        --template)  TEMPLATE="$2"; shift 2 ;;
        --wan)       WAN_BRIDGE="$2"; shift 2 ;;
        --lan)       LAN_BRIDGE="$2"; shift 2 ;;
        --net)       CUSTOM_NETS_LIST="${CUSTOM_NETS_LIST} $2"; shift 2 ;; # Include custom net
        --mgmt-ip)   MGMT_IP="$2"; shift 2 ;;
        --prefix)    INSTALL_PREFIX="$2"; shift 2 ;;
        --ssh-key)   SSH_KEY="$2"; shift 2 ;;
        --dry-run)   DRY_RUN=true; shift ;;
        -h|--help)
            head -35 "$0" | tail -30
            echo "   --net \"name=eth0,bridge=vmbr0,ip=dhcp\"   Define network interface using Proxmox syntax"
            echo "                                            Supported keys: name,bridge,tag,ip,ip6,gw,gw6,etc."
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Helper to extract a value from a comma-separated key=value string
# Usage: extract_param "key" "string"
extract_param() {
    local key=$1
    local string=$2
    # Add newline to match end of string
    # grep for key=value, then cut value
    # Handle optional comma at end
    # Use || true to prevent pipefail from exiting if grep finds nothing
    echo "$string" | tr ',' '\n' | grep "^${key}=" | cut -d= -f2 || true
}

# Check if running on Proxmox
check_proxmox() {
    if ! command -v pveversion >/dev/null 2>&1; then
        log_error "This script must be run on a Proxmox VE host."
        exit 1
    fi
}

# Get next available VMID
get_next_vmid() {
    if [ -n "$VMID" ]; then
        echo "$VMID"
        return
    fi
    # Robust extraction from pvesh
    local nextid
    nextid=$(pvesh get /cluster/nextid --output-format json | grep "nextid" | sed 's/.*"nextid": "\([0-9]*\)".*/\1/')
    if [ -z "$nextid" ]; then
        # Fallback if json parsing fails (e.g. integer vs string)
        nextid=$(pvesh get /cluster/nextid --output-format json | grep "nextid" | grep -oE "[0-9]+")
    fi
    echo "$nextid"
}

# Ensure Alpine template is available
ensure_template() {
    if [ -n "$TEMPLATE" ]; then
        if [ -f "$TEMPLATE" ]; then
            echo "$TEMPLATE"
            return
        fi
        log_error "Template file not found: $TEMPLATE"
        exit 1
    fi

    # Update template list
    pveam update >/dev/null 2>&1

    # Look for Alpine template (matching version)
    local template_name
    # Try finding exact match for our version
    template_name=$(pveam available --section system | grep "alpine-${ALPINE_VERSION}" | grep "default" | sort -r | head -n 1 | awk '{print $2}')

    if [ -z "$template_name" ]; then
        # Fallback to any alpine
        template_name=$(pveam available --section system | grep "alpine" | grep "default" | sort -r | head -n 1 | awk '{print $2}')
    fi

    if [ -z "$template_name" ]; then
        log_error "Could not find an Alpine Linux template."
        exit 1
    fi

    # Determine storage for template
    # Default to 'local' which usually supports 'vztmpl', as 'local-lvm' usually doesn't.
    local tmpl_storage="local"

    # Check if we already have it downloading
    if ! pveam list "$tmpl_storage" 2>/dev/null | grep -q "$template_name"; then
        log_info "Downloading template: $template_name to $tmpl_storage..."
        pveam download "$tmpl_storage" "$template_name" >/dev/null
    fi

    # Return full path if possible, or usually just the volumenamespaced name "local:vztmpl/..."
    # 'pveam list' output: local:vztmpl/alpine-...
    # 'pct create' expects the volid.

    echo "${tmpl_storage}:vztmpl/${template_name}"
}

# Create the LXC container
create_container() {
    local vmid=$1
    local template=$2

    log_info "Creating LXC container ${vmid} (${CT_NAME})..."

    local cmd=(
        pct create "$vmid" "$template"
        --hostname "$CT_NAME"
        --memory "$MEMORY"
        --swap 0
        --cores 2
        --rootfs "${STORAGE}:${DISK}"
        --unprivileged 0
        --features "nesting=1"
        --onboot 1
        --start 0
    )

    # Handle Network Interfaces
    if [ -n "$CUSTOM_NETS_LIST" ]; then
        log_info "Using custom network configuration:"
        local idx=0
        for net_def in $CUSTOM_NETS_LIST; do
            # Sanitize net_def: Remove 'table=X' as it's not a valid Proxmox key
            # We use sed to remove it. Three cases: start, middle, end.
            # E.g. "table=200,name=eth0" or "name=eth0,table=200,ip=dchp" or "name=eth0,table=200"
            # Regex: (table=[^,]*)(,|$) -> replace with nothing if at end, or handle comma.

            local clean_net_def
            # Remove "table=..." and cleanup commas
            clean_net_def=$(echo "$net_def" | sed -E 's/table=[^,]*//g' | sed -E 's/,,/,/g' | sed -E 's/^,//' | sed -E 's/,$//')

            log_info "  net${idx}: $net_def (Passed to Proxmox as: $clean_net_def)"
            cmd+=( "--net${idx}" "$clean_net_def" )
            idx=$((idx + 1))
        done
    else
        # Default behavior (WAN/LAN)
        # ... (standard paths)
        local wan_ip="dhcp"
        if [ -n "$MGMT_IP" ]; then
            wan_ip="$MGMT_IP"
        fi
        local net0="name=eth0,bridge=${WAN_BRIDGE},ip=${wan_ip}"
        local net1="name=eth1,bridge=${LAN_BRIDGE},ip=manual"
        log_info "WAN config: ${net0}"
        log_info "LAN config: ${net1}"
        cmd+=( "--net0" "$net0" "--net1" "$net1" )
    fi

    if $DRY_RUN; then
        log_info "Would run: ${cmd[*]}"
    else
        "${cmd[@]}"
        log_ok "Container created"
    fi
}

# Configure all interfaces to manual to allow Glacic full control
configure_interfaces_manual() {
    local vmid=$1
    log_info "Configuring network interfaces to manual mode..."

    # 1. Create .pve-ignore.interfaces to prevent PVE from overwriting it
    pct exec "$vmid" -- touch /etc/network/.pve-ignore.interfaces

    # 2. Reset /etc/network/interfaces to minimal (loopback only)
    pct exec "$vmid" -- sh -c "echo 'auto lo' > /etc/network/interfaces"
    pct exec "$vmid" -- sh -c "echo 'iface lo inet loopback' >> /etc/network/interfaces"

    # 3. Update Proxmox container config to 'manual' to reflect reality in UI
    # We need to find how many nets we have.
    # We can iterate until failure or just do net0 to net9
    log_info "Updating Proxmox container configuration to ip=manual..."
    for i in $(seq 0 9); do
        # Check if net$i exists in config
        if pct config "$vmid" | grep -q "^net$i:"; then
            # Set ip=manual, ip6=manual (and clear gw if present to avoid errors, though manual usually ignores gw)
            # pct set updates the config.
            # We want to keep mac, bridge, etc. but change ip/gw.
            # pct set vmid -netX ... replaces the whole entry? No, it usually updates keys if passed as string?
            # Actually pct set vmid -net0 name=eth0,... replaces it.
            # IMPORTANT: 'pct set' with -netX argument replaces the *entire* net config line if not careful,
            # OR we can pass just the new string if we reconstruct it.
            # A safer way might be to get current config, modify string, set it back.

            local current_net
            current_net=$(pct config "$vmid" | grep "^net$i:" | cut -d' ' -f2-)

            # Replace ip=... with ip=manual
            # Replace gw=... with nothing (remove it)
            # Replace ip6=... with ip6=manual

            # Use sed to construct new string? Difficult in bash.
            # Alternatively, Proxmox 'pct set' might support partial updates if we knew the syntax?
            # Re-reading docs: 'pct set <vmid> -net0 name=eth0,bridge=vmbr0,ip=manual'
            # If we omit other fields, do they stay? Usually NO, it expects full spec definition for that property.

            # So we must parse `current_net`, modify ip/gw parts, and re-apply.
            # Format: bridge=vmbr0,hwaddr=BC:24:11:...,ip=dhcp,name=eth0,type=veth

            local new_net="$current_net"
            # Replace ip=... with ip=manual
            new_net=$(echo "$new_net" | sed -E 's/ip=[^,]+/ip=manual/')
            if ! echo "$new_net" | grep -q "ip=manual"; then
                 # If ip= wasn't there (maybe omitted?), add it? Or leave it?
                 # If it was manual already, fine.
                 new_net="$new_net,ip=manual"
            fi

            # Replace ip6=... with ip6=manual
            if echo "$new_net" | grep -q "ip6="; then
                new_net=$(echo "$new_net" | sed -E 's/ip6=[^,]+/ip6=manual/')
            else
                 # Optional: add ip6=manual just in case
                 new_net="$new_net,ip6=manual"
            fi

            # Remove gw=... (gateway) if present
            new_net=$(echo "$new_net" | sed -E 's/gw=[^,]+(,|$)//')
            # Remove gw6=...
            new_net=$(echo "$new_net" | sed -E 's/gw6=[^,]+(,|$)//')

            # Clean up trailing/double commas
            new_net=$(echo "$new_net" | sed -E 's/,,/,/g' | sed -E 's/,$//')

            log_info "  Updating net$i to: $new_net"
            pct set "$vmid" "-net$i" "$new_net"
        fi
    done
}

# Helper: Retry command with backoff
# Usage: retry_cmd "description" command args...
retry_cmd() {
    local desc=$1
    shift
    local max_attempts=5
    local attempt=1
    local delay=2

    while [ $attempt -le $max_attempts ]; do
        if "$@"; then
            return 0
        fi

        log_warn "Command '$desc' failed (attempt $attempt/$max_attempts). Retrying in ${delay}s..."
        sleep $delay

        attempt=$((attempt + 1))
        delay=$((delay * 2)) # Exponential backoff
    done

    log_error "Command '$desc' failed after $max_attempts attempts."
    return 1
}

# Configure container basics
configure_container() {
    local vmid=$1
    log_info "Configuring container settings..."

    # Enable IP forwarding
    pct exec "$vmid" -- sysctl -w net.ipv4.ip_forward=1 >/dev/null
    pct exec "$vmid" -- sh -c "echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf"

    # Install minimal packages with retry
    log_info "Installing dependencies..."

    # Retry apk update
    retry_cmd "apk update" pct exec "$vmid" -- apk update

    # Retry apk add - core dependencies + debugging utilities
    retry_cmd "apk add dependencies" pct exec "$vmid" -- apk add \
        bash curl ipset nftables openssh-server iptables iproute2 ethtool \
        tcpdump conntrack-tools strace lsof net-tools bind-tools

    # Ensure sshd is enabled
    pct exec "$vmid" -- rc-update add sshd default
}

# Install Glacic binary
install_binary() {
    local vmid=$1
    log_info "Installing Glacic binary..."

    # Check locations:
    # 1. Current directory (from tarball)
    # 2. build/glacic (dev mode)
    if [ -f "./glacic" ]; then
        pct push "$vmid" ./glacic "${INSTALL_PREFIX:-/usr/sbin}/glacic"
        pct exec "$vmid" -- chmod +x "${INSTALL_PREFIX:-/usr/sbin}/glacic"
    elif [ -f "build/glacic" ]; then
        pct push "$vmid" build/glacic "${INSTALL_PREFIX:-/usr/sbin}/glacic"
        pct exec "$vmid" -- chmod +x "${INSTALL_PREFIX:-/usr/sbin}/glacic"
    else
        log_warn "Binary 'glacic' not found in . or build/. Skipping binary upload."
    fi
}

# Install Glacic services
install_glacic() {
    local vmid=$1
    log_info "Setting up Glacic..."

    # Create directories
    pct exec "$vmid" -- mkdir -p "$STATE_DIR" "$LOG_DIR" /etc/glacic
}

# Inject SSH key
inject_ssh_key() {
    local vmid=$1
    if [ -n "$SSH_KEY" ]; then
        log_info "Injecting SSH key..."
        pct exec "$vmid" -- mkdir -p /root/.ssh
        pct exec "$vmid" -- sh -c "echo '$SSH_KEY' >> /root/.ssh/authorized_keys"
        pct exec "$vmid" -- chmod 700 /root/.ssh
        pct exec "$vmid" -- chmod 600 /root/.ssh/authorized_keys
    elif [ -f "$HOME/.ssh/id_rsa.pub" ]; then
        log_info "Injecting local SSH key..."
        pct exec "$vmid" -- mkdir -p /root/.ssh
        # Read local key and push
        local key
        key=$(cat "$HOME/.ssh/id_rsa.pub")
        pct exec "$vmid" -- sh -c "echo '$key' >> /root/.ssh/authorized_keys"
        pct exec "$vmid" -- chmod 700 /root/.ssh
        pct exec "$vmid" -- chmod 600 /root/.ssh/authorized_keys
    fi
}



# Create Main Service (OpenRC)
create_service() {
    local vmid=$1
    log_info "Creating Glacic Control Plane service..."

    local svc_content='#!/sbin/openrc-run

name="glacic"
description="Glacic Firewall"
command="'"${INSTALL_PREFIX:-/usr/sbin}"'/glacic"
command_args="start --config /etc/glacic/glacic.hcl"
pidfile="/var/run/glacic.pid"

depend() {
    need net
    provide firewall
}

stop() {
    ebegin "Stopping Glacic"
    # Use the stop command which handles graceful shutdown
    '"${INSTALL_PREFIX:-/usr/sbin}"'/glacic stop
    eend $?
}
'
    pct exec "$vmid" -- sh -c "cat > /etc/init.d/glacic" <<EOF
$svc_content
EOF
    pct exec "$vmid" -- chmod +x /etc/init.d/glacic
    pct exec "$vmid" -- rc-update add glacic default
}

# Create default configuration - Dynamic based on interfaces
create_default_config() {
    local vmid=$1

    log_info "Creating default configuration..."

    # Start config header
    local config='# Glacic Configuration
# Generated by install-proxmox-lxc.sh

schema_version = "1.0"
ip_forwarding = true
'

    # Generate DHCP Server block dynamically
    local dhcp_scopes=""

    # If using custom nets, generate interfaces block dynamically
    if [ -n "$CUSTOM_NETS_LIST" ]; then
        for net_def in $CUSTOM_NETS_LIST; do
            # Parse params
            local iface
            iface=$(extract_param "name" "$net_def")
            local ip
            ip=$(extract_param "ip" "$net_def")
            local ip6
            ip6=$(extract_param "ip6" "$net_def")
            local table
            table=$(extract_param "table" "$net_def")

            if [ -z "$iface" ]; then
                log_warn "Network definition missing 'name' parameter: $net_def. Skipping config generation for it."
                continue
            fi

            if [ "$iface" = "eth0" ]; then
                config+="
interface \"$iface\" {
  description = \"WAN Interface\"
  zone        = \"wan\"
  dhcp        = true
"
                if [ -n "$table" ]; then
                     config+="  table       = ${table}
"
                fi
                if [ "$ip" != "dhcp" ] && [ "$ip" != "manual" ] && [ -n "$ip" ]; then
                    config+="  # Static IP from Proxmox config
  # ipv4 = [\"${ip}\"]
"
                fi
                config+="}
"
            else
                config+="
interface \"$iface\" {
  description = \"Interface $iface\"
  zone        = \"lan\"
"
                if [ -n "$table" ]; then
                     config+="  table       = ${table}
"
                fi
                if [ "$ip" == "dhcp" ]; then
                     config+="  dhcp        = true
"
                elif [ "$ip" != "manual" ] && [ -n "$ip" ]; then
                     config+="  ipv4 = [\"${ip}\"]
"
                     # Generate DHCP scope for this custom LAN interface
                     if [[ "$ip" == 192.168.* ]]; then
                        local subnet_base="${ip%.*}"
                        dhcp_scopes+="
  scope \"${iface}_scope\" {
    interface   = \"${iface}\"
    range_start = \"${subnet_base}.100\"
    range_end   = \"${subnet_base}.200\"
    router      = \"${ip%/*}\"
    dns         = [\"8.8.8.8\", \"8.8.4.4\"]
    lease_time  = \"24h\"
  }
"
                     fi
                else
                     config+="  # ipv4 = [\"192.168.X.1/24\"]
"
                fi

                if [ "$ip6" != "dhcp" ] && [ "$ip6" != "manual" ] && [ "$ip6" != "auto" ] && [ -n "$ip6" ]; then
                     config+="  ipv6 = [\"${ip6}\"]
"
                fi
                config+="}
"
            fi
        done

        # Add basic zone/policy assuming eth0=wan, others=lan
        config+="
zone \"wan\" {
  description = \"External/Internet\"
  interfaces  = [\"eth0\"]
  action = \"drop\"
}

zone \"lan\" {
  description = \"Internal Network\"
  # Match all other interfaces
  interfaces  = ["
        for net_def in $CUSTOM_NETS_LIST; do
            local iface
            iface=$(extract_param "name" "$net_def")
            if [ "$iface" != "eth0" ] && [ -n "$iface" ]; then
                config+="\"$iface\", "
            fi
        done
  config+="]
  action = \"accept\"

  services {
    # Enabled in firewall rules
    dhcp = true
    dns   = true
  }

  management {
    web_ui = true
    ssh    = true
    icmp   = true
  }
}
"
    else
        # Standard Default Config (copied from before for fallback)
        config+='
# WAN Interface (eth0)
interface "eth0" {
  description = "WAN Interface"
  zone         = "wan"
  dhcp        = true
}

# LAN Interface (eth1)
interface "eth1" {
  description = "LAN Interface"
  zone        = "lan"
  ipv4        = ["192.168.1.1/24"]
}

zone "wan" {
  description = "External/Internet"
  interfaces  = ["eth0"]
  action = "drop"
}

zone "lan" {
  description = "Internal Network"
  interfaces  = ["eth1"]
  action = "accept"

  services {
    dhcp = true
    dns  = true
  }

  management {
    web_ui = true
    ssh    = true
    icmp   = true
  }
}
'
        # Add default scope for eth1 if not using custom nets
        dhcp_scopes+='
  scope "lan_default" {
    interface   = "eth1"
    range_start = "192.168.1.100"
    range_end   = "192.168.1.200"
    router      = "192.168.1.1"
    dns         = ["8.8.8.8", "8.8.4.4"]
    lease_time  = "24h"
  }
'
    fi

    # DHCP Server Block
    if [ -n "$dhcp_scopes" ]; then
        config+="
dhcp {
  enabled = true
  $dhcp_scopes
}
"
    fi

    # Common policy/NAT/DNS (updated HCL schema)
    config+='
policy "lan_to_wan" {
  from           = "lan"
  to             = "wan"
  action = "accept"
}

nat "lan_masquerade" {
  type          = "masquerade"
  out_interface = "eth0"
}

dns_server {
  enabled    = true
  listen_on  = ["0.0.0.0"]
  forwarders = ["8.8.8.8", "8.8.4.4"]
}
'

    if $DRY_RUN; then
        log_info "Would create /etc/glacic/glacic.hcl"
        echo "$config"
        return
    fi

    echo "$config" | pct exec "$vmid" -- tee /etc/glacic/glacic.hcl > /dev/null

    log_ok "Default configuration created"
}

# Print summary - Updated paths
print_summary() {
    local vmid=$1

    echo ""
    echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  Glacic LXC Container Created Successfully${NC}"
    echo -e "${GREEN}════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo "  Container ID:   ${vmid}"
    echo "  Name:           ${CT_NAME}"
    echo ""
    echo "  Network:"

    if [ -n "$CUSTOM_NETS_LIST" ]; then
        for net_def in $CUSTOM_NETS_LIST; do
             echo "    $net_def"
        done
    else
        # Parse and display WAN config
        local wan_parsed wan_bridge wan_tag
        wan_parsed=$(parse_bridge_spec "$WAN_BRIDGE")
        wan_bridge="${wan_parsed%,*}"
        wan_tag="${wan_parsed#*,}"
        if [ -n "$wan_tag" ]; then
            echo "    eth0 (WAN):   ${wan_bridge} (VLAN ${wan_tag})"
        else
            echo "    eth0 (WAN):   ${wan_bridge}"
        fi

        # Parse and display LAN config
        local lan_parsed lan_bridge lan_tag
        lan_parsed=$(parse_bridge_spec "$LAN_BRIDGE")
        lan_bridge="${lan_parsed%,*}"
        lan_tag="${lan_parsed#*,}"
        if [ -n "$lan_tag" ]; then
            echo "    eth1 (LAN):   ${lan_bridge} (VLAN ${lan_tag})"
        else
            echo "    eth1 (LAN):   ${lan_bridge}"
        fi
    fi
    echo ""
    echo "  Configuration:  /etc/glacic/glacic.hcl"
    echo "  Logs:           /var/log/glacic/"
    echo ""
    echo "  Inside container:"
    echo "    Start service:  rc-service glacic start"
    echo "    View status:    glacic ctl /etc/glacic/glacic.hcl"
    echo ""
}

# Main
main() {
    echo ""
    echo -e "${BLUE}Proxmox LXC Glacic Installer${NC}"
    echo ""

    check_proxmox

    local vmid
    vmid=$(get_next_vmid)
    log_info "Using VMID: ${vmid}"

    local template
    template=$(ensure_template)
    log_info "Using template: ${template}"

    create_container "$vmid" "$template"

    # Start container for configuration
    if ! $DRY_RUN; then
        log_info "Starting container to configure..."
        pct start "$vmid"

        # Wait for container to be running
        log_info "Waiting for container to start..."
        local retries=0
        while ! pct status "$vmid" | grep -q "running"; do
            sleep 1
            retries=$((retries + 1))
            if [ $retries -gt 30 ]; then
                 log_error "Timeout waiting for container to start."
                 exit 1
            fi
        done
        # Give it a few more seconds for init
        sleep 5
    fi

    configure_container "$vmid"

    if ! $DRY_RUN; then
        install_glacic "$vmid"
        install_binary "$vmid"
        inject_ssh_key "$vmid"  # Added injection
        create_service "$vmid"
        create_default_config "$vmid"

        # Switch interfaces to manual now that install is done (requires running container)
        configure_interfaces_manual "$vmid"

        # Reboot container to ensure clean state and start Glacic services
        log_info "Rebooting container to start Glacic..."
        pct stop "$vmid" 2>/dev/null || true
        # Wait for stop
        sleep 2
        pct start "$vmid"

        log_ok "Container started. Glacic should be running."
    fi

    print_summary "$vmid"
}

main "$@"
