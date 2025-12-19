package ddns

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider is the interface for DDNS providers.
type Provider interface {
	Update(hostname, ip string) error
	Name() string
}

// Config holds DDNS configuration.
type Config struct {
	Enabled   bool   `hcl:"enabled,optional" json:"enabled"`
	Provider  string `hcl:"provider" json:"provider"`                      // duckdns, cloudflare, noip
	Hostname  string `hcl:"hostname" json:"hostname"`                      // Hostname to update
	Token     string `hcl:"token,optional" json:"token,omitempty"`         // API token/password
	Username  string `hcl:"username,optional" json:"username,omitempty"`   // For providers requiring username
	ZoneID    string `hcl:"zone_id,optional" json:"zone_id,omitempty"`     // For Cloudflare
	RecordID  string `hcl:"record_id,optional" json:"record_id,omitempty"` // For Cloudflare
	Interface string `hcl:"interface,optional" json:"interface,omitempty"` // Interface to get IP from
	Interval  int    `hcl:"interval,optional" json:"interval,omitempty"`   // Update interval in minutes (default: 5)
}

// NewProvider creates a provider based on config.
func NewProvider(cfg Config) (Provider, error) {
	switch strings.ToLower(cfg.Provider) {
	case "duckdns":
		return &DuckDNS{token: cfg.Token, hostname: cfg.Hostname}, nil
	case "cloudflare":
		return &Cloudflare{
			token:    cfg.Token,
			zoneID:   cfg.ZoneID,
			recordID: cfg.RecordID,
			hostname: cfg.Hostname,
		}, nil
	case "noip", "no-ip":
		return &NoIP{
			username: cfg.Username,
			password: cfg.Token,
			hostname: cfg.Hostname,
		}, nil
	default:
		return nil, fmt.Errorf("unknown DDNS provider: %s", cfg.Provider)
	}
}

// --- DuckDNS Provider ---

type DuckDNS struct {
	token    string
	hostname string
}

func (d *DuckDNS) Name() string { return "DuckDNS" }

func (d *DuckDNS) Update(hostname, ip string) error {
	// DuckDNS API: https://www.duckdns.org/update?domains=HOSTNAME&token=TOKEN&ip=IP
	u := fmt.Sprintf("https://www.duckdns.org/update?domains=%s&token=%s&ip=%s",
		url.QueryEscape(hostname), url.QueryEscape(d.token), url.QueryEscape(ip))

	resp, err := http.Get(u)
	if err != nil {
		return fmt.Errorf("duckdns update failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "OK" {
		return fmt.Errorf("duckdns update failed: %s", string(body))
	}
	return nil
}

// --- Cloudflare Provider ---

type Cloudflare struct {
	token    string
	zoneID   string
	recordID string
	hostname string
}

func (c *Cloudflare) Name() string { return "Cloudflare" }

func (c *Cloudflare) Update(hostname, ip string) error {
	// Cloudflare API: PATCH /zones/{zone_id}/dns_records/{record_id}
	u := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s",
		c.zoneID, c.recordID)

	payload := map[string]interface{}{
		"type":    "A",
		"name":    hostname,
		"content": ip,
		"ttl":     300,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("PATCH", u, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare update failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare update failed: %s", string(respBody))
	}
	return nil
}

// --- No-IP Provider ---

type NoIP struct {
	username string
	password string
	hostname string
}

func (n *NoIP) Name() string { return "No-IP" }

func (n *NoIP) Update(hostname, ip string) error {
	// No-IP API: GET https://dynupdate.no-ip.com/nic/update?hostname=HOSTNAME&myip=IP
	u := fmt.Sprintf("https://dynupdate.no-ip.com/nic/update?hostname=%s&myip=%s",
		url.QueryEscape(hostname), url.QueryEscape(ip))

	req, _ := http.NewRequest("GET", u, nil)
	req.SetBasicAuth(n.username, n.password)
	req.Header.Set("User-Agent", "glacic/1.0 admin@localhost")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("noip update failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	response := string(body)

	// No-IP returns "good IP" or "nochg IP" on success
	if !strings.HasPrefix(response, "good") && !strings.HasPrefix(response, "nochg") {
		return fmt.Errorf("noip update failed: %s", response)
	}
	return nil
}

// --- Utilities ---

// GetPublicIP fetches the current public IP address.
func GetPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// GetInterfaceIP gets the IP address of a specific interface.
func GetInterfaceIP(ifaceName string) (string, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return "", err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv4 address found on %s", ifaceName)
}
