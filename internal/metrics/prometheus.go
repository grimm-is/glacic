package metrics

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	once     sync.Once
	registry *Registry
)

// Registry holds all firewall metrics.
type Registry struct {
	// Firewall metrics
	PacketsTotal    *prometheus.CounterVec
	BytesTotal      *prometheus.CounterVec
	RuleMatches     *prometheus.CounterVec
	DroppedPackets  *prometheus.CounterVec
	AcceptedPackets *prometheus.CounterVec

	// IPSet metrics
	IPSetSize        *prometheus.GaugeVec
	IPSetLastUpdate  *prometheus.GaugeVec
	IPSetUpdateTotal *prometheus.CounterVec
	IPSetErrors      *prometheus.CounterVec
	IPSetBlocked     *prometheus.CounterVec

	// Connection tracking
	ConntrackCount   prometheus.Gauge
	ConntrackMax     prometheus.Gauge
	ConntrackNew     prometheus.Counter
	ConntrackDestroy prometheus.Counter

	// NAT metrics
	NATTranslations *prometheus.CounterVec
	NATErrors       *prometheus.CounterVec

	// Interface metrics
	InterfaceRxBytes   *prometheus.GaugeVec
	InterfaceTxBytes   *prometheus.GaugeVec
	InterfaceRxPackets *prometheus.GaugeVec
	InterfaceTxPackets *prometheus.GaugeVec
	InterfaceErrors    *prometheus.GaugeVec

	// DHCP metrics
	DHCPLeases   *prometheus.GaugeVec
	DHCPRequests *prometheus.CounterVec
	DHCPAcks     *prometheus.CounterVec
	DHCPNaks     *prometheus.CounterVec

	// DNS metrics
	DNSQueries     *prometheus.CounterVec
	DNSCacheHits   prometheus.Counter
	DNSCacheMisses prometheus.Counter
	DNSBlocked     prometheus.Counter

	// System metrics
	Uptime       prometheus.Gauge
	ConfigReload *prometheus.CounterVec
	APIRequests  *prometheus.CounterVec
	APILatency   *prometheus.HistogramVec
}

// Get returns the global metrics registry, creating it if necessary.
func Get() *Registry {
	once.Do(func() {
		registry = newRegistry()
	})
	return registry
}

func newRegistry() *Registry {
	r := &Registry{}

	// Firewall metrics
	r.PacketsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_packets_total",
		Help: "Total packets processed by the firewall",
	}, []string{"interface", "direction", "zone"})

	r.BytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_bytes_total",
		Help: "Total bytes processed by the firewall",
	}, []string{"interface", "direction", "zone"})

	r.RuleMatches = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_rule_matches_total",
		Help: "Number of times each rule matched",
	}, []string{"chain", "rule", "action"})

	r.DroppedPackets = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_dropped_packets_total",
		Help: "Total packets dropped by the firewall",
	}, []string{"chain", "reason"})

	r.AcceptedPackets = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_accepted_packets_total",
		Help: "Total packets accepted by the firewall",
	}, []string{"chain", "zone"})

	// IPSet metrics
	r.IPSetSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_ipset_size",
		Help: "Number of entries in each IPSet",
	}, []string{"name", "type"})

	r.IPSetLastUpdate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_ipset_last_update_timestamp",
		Help: "Unix timestamp of last IPSet update",
	}, []string{"name"})

	r.IPSetUpdateTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_ipset_updates_total",
		Help: "Total number of IPSet updates",
	}, []string{"name", "source"})

	r.IPSetErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_ipset_errors_total",
		Help: "Total number of IPSet update errors",
	}, []string{"name", "error_type"})

	r.IPSetBlocked = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_ipset_blocked_total",
		Help: "Total packets blocked by IPSet rules",
	}, []string{"name", "direction"})

	// Connection tracking
	r.ConntrackCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "firewall_conntrack_entries",
		Help: "Current number of connection tracking entries",
	})

	r.ConntrackMax = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "firewall_conntrack_max",
		Help: "Maximum connection tracking entries",
	})

	r.ConntrackNew = promauto.NewCounter(prometheus.CounterOpts{
		Name: "firewall_conntrack_new_total",
		Help: "Total new connections tracked",
	})

	r.ConntrackDestroy = promauto.NewCounter(prometheus.CounterOpts{
		Name: "firewall_conntrack_destroy_total",
		Help: "Total connections removed from tracking",
	})

	// NAT metrics
	r.NATTranslations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_nat_translations_total",
		Help: "Total NAT translations performed",
	}, []string{"type", "interface"})

	r.NATErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_nat_errors_total",
		Help: "Total NAT translation errors",
	}, []string{"type", "error"})

	// Interface metrics
	r.InterfaceRxBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_interface_rx_bytes",
		Help: "Received bytes per interface",
	}, []string{"interface", "zone"})

	r.InterfaceTxBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_interface_tx_bytes",
		Help: "Transmitted bytes per interface",
	}, []string{"interface", "zone"})

	r.InterfaceRxPackets = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_interface_rx_packets",
		Help: "Received packets per interface",
	}, []string{"interface", "zone"})

	r.InterfaceTxPackets = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_interface_tx_packets",
		Help: "Transmitted packets per interface",
	}, []string{"interface", "zone"})

	r.InterfaceErrors = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_interface_errors",
		Help: "Interface errors",
	}, []string{"interface", "type"})

	// DHCP metrics
	r.DHCPLeases = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "firewall_dhcp_leases",
		Help: "Current DHCP leases",
	}, []string{"interface", "pool"})

	r.DHCPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_dhcp_requests_total",
		Help: "Total DHCP requests received",
	}, []string{"interface", "type"})

	r.DHCPAcks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_dhcp_acks_total",
		Help: "Total DHCP ACKs sent",
	}, []string{"interface"})

	r.DHCPNaks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_dhcp_naks_total",
		Help: "Total DHCP NAKs sent",
	}, []string{"interface", "reason"})

	// DNS metrics
	r.DNSQueries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_dns_queries_total",
		Help: "Total DNS queries",
	}, []string{"type", "status"})

	r.DNSCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "firewall_dns_cache_hits_total",
		Help: "Total DNS cache hits",
	})

	r.DNSCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "firewall_dns_cache_misses_total",
		Help: "Total DNS cache misses",
	})

	r.DNSBlocked = promauto.NewCounter(prometheus.CounterOpts{
		Name: "firewall_dns_blocked_total",
		Help: "Total DNS queries blocked",
	})

	// System metrics
	r.Uptime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "firewall_uptime_seconds",
		Help: "Firewall uptime in seconds",
	})

	r.ConfigReload = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_config_reloads_total",
		Help: "Total configuration reloads",
	}, []string{"status"})

	r.APIRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "firewall_api_requests_total",
		Help: "Total API requests",
	}, []string{"method", "path", "status"})

	r.APILatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "firewall_api_request_duration_seconds",
		Help:    "API request latency",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	return r
}

// RecordIPSetUpdate records an IPSet update event.
func (r *Registry) RecordIPSetUpdate(name, source string, size int, err error) {
	r.IPSetUpdateTotal.WithLabelValues(name, source).Inc()
	if err != nil {
		r.IPSetErrors.WithLabelValues(name, "update_failed").Inc()
	} else {
		r.IPSetSize.WithLabelValues(name, "ipv4_addr").Set(float64(size))
	}
}

// RecordRuleMatch records a firewall rule match.
func (r *Registry) RecordRuleMatch(chain, rule, action string) {
	r.RuleMatches.WithLabelValues(chain, rule, action).Inc()
}

// RecordAPIRequest records an API request.
func (r *Registry) RecordAPIRequest(method, path string, status int, duration float64) {
	r.APIRequests.WithLabelValues(method, path, statusString(status)).Inc()
	r.APILatency.WithLabelValues(method, path).Observe(duration)
}

// UpdateConntrack updates connection tracking metrics.
func (r *Registry) UpdateConntrack(count, max int) {
	r.ConntrackCount.Set(float64(count))
	r.ConntrackMax.Set(float64(max))
}

// statusString converts an HTTP status code to string.
func statusString(status int) string {
	return fmt.Sprintf("%d", status)
}
