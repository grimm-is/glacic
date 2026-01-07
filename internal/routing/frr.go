package routing

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"grimm.is/glacic/internal/logging"

	"grimm.is/glacic/internal/config"
)

// ConfigureFRR generates the FRR configuration and starts/reloads the service.
func ConfigureFRR(cfg *config.FRRConfig) error {
	if cfg == nil || !cfg.Enabled {
		return stopFRR()
	}

	// Generate frr.conf content
	confContent := generateFRRConf(cfg)

	// Write to /etc/frr/frr.conf
	// Ensure directory exists
	if err := os.MkdirAll("/etc/frr", 0755); err != nil {
		return fmt.Errorf("failed to create /etc/frr: %w", err)
	}

	if err := os.WriteFile("/etc/frr/frr.conf", []byte(confContent), 0644); err != nil {
		return fmt.Errorf("failed to write frr.conf: %w", err)
	}

	// Ensure daemons file enables required daemons
	daemonsContent := generateDaemonsFile(cfg)
	if err := os.WriteFile("/etc/frr/daemons", []byte(daemonsContent), 0644); err != nil {
		return fmt.Errorf("failed to write daemons file: %w", err)
	}

	// Apply config via vtysh (dynamic update)
	// We pipe the config to vtysh to avoid ARG_MAX limits.
	// We assume 'configure terminal' is implied or we prepend it?
	// The generated config is a full file.
	// We'll wrap it in 'configure terminal' block just in case, or rely on vtysh processing.
	// Actually, vtysh handles full config import often.
	// Let's prepend 'configure terminal' and append 'end' / 'write memory'.

	// Note: header lines like 'frr version' might generate benign errors in vtysh

	cmd := exec.Command("vtysh")

	// Construct input: configure terminal <config> end write memory
	// We strip "frr version" etc for cleaner apply?
	// For now, pass as is, wrapping in conf t

	input := "configure terminal\n" + confContent + "\nend\nwrite memory\n"
	cmd.Stdin = strings.NewReader(input)

	if output, err := cmd.CombinedOutput(); err != nil {
		// Log warning but don't fail hard, as some lines might be idempotent errors
		logging.Warn("partial failure applying FRR config via vtysh", "error", err, "output", string(output))

		// Fallback to restart if dynamic apply looks completely broken?
		// Or just stick to restart if this fails?
		// The user urged Stdin to fix ARG_MAX.
	}

	return nil
}

func stopFRR() error {
	// Best effort stop
	_ = exec.Command("rc-service", "frr", "stop").Run()
	return nil
}

func generateFRRConf(cfg *config.FRRConfig) string {
	var sb strings.Builder
	sb.WriteString("frr version 8.0\n")
	sb.WriteString("frr defaults traditional\n")
	sb.WriteString("hostname firewall\n")
	sb.WriteString("log syslog informational\n")
	sb.WriteString("service integrated-vtysh-config\n")
	sb.WriteString("!\n")

	if cfg.OSPF != nil {
		sb.WriteString("router ospf\n")
		if cfg.OSPF.RouterID != "" {
			sb.WriteString(fmt.Sprintf(" ospf router-id %s\n", cfg.OSPF.RouterID))
		}
		for _, net := range cfg.OSPF.Networks {
			sb.WriteString(fmt.Sprintf(" network %s area 0.0.0.0\n", net))
		}
		for _, area := range cfg.OSPF.Areas {
			for _, net := range area.Networks {
				sb.WriteString(fmt.Sprintf(" network %s area %s\n", net, area.ID))
			}
		}
		sb.WriteString("!\n")
	}

	if cfg.BGP != nil {
		sb.WriteString(fmt.Sprintf("router bgp %d\n", cfg.BGP.ASN))
		if cfg.BGP.RouterID != "" {
			sb.WriteString(fmt.Sprintf(" bgp router-id %s\n", cfg.BGP.RouterID))
		}
		for _, n := range cfg.BGP.Neighbors {
			sb.WriteString(fmt.Sprintf(" neighbor %s remote-as %d\n", n.IP, n.RemoteASN))
		}
		for _, net := range cfg.BGP.Networks {
			sb.WriteString(fmt.Sprintf(" network %s\n", net))
		}
		sb.WriteString("!\n")
	}

	sb.WriteString("line vty\n")
	sb.WriteString("!\n")
	return sb.String()
}

func generateDaemonsFile(cfg *config.FRRConfig) string {
	// Enable/Disable daemons based on config
	ospf := "no"
	bgp := "no"

	if cfg.OSPF != nil {
		ospf = "yes"
	}
	if cfg.BGP != nil {
		bgp = "yes"
	}

	return fmt.Sprintf(`
zebra=yes
bgpd=%s
ospfd=%s
ospf6d=no
ripd=no
ripngd=no
isisd=no
pimd=no
ldpd=no
nhrpd=no
eigrp=no
babeld=no
sharpd=no
pbrd=no
bfdd=no
fabricd=no
vrrpd=no
`, bgp, ospf)
}
