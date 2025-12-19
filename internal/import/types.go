package imports

// ImportResult contains the parsed configuration with migration notes.
type ImportResult struct {
	// Extracted configuration
	Hostname     string
	Domain       string
	Interfaces   []ImportedInterface
	Aliases      []ImportedAlias
	FilterRules  []ImportedFilterRule
	NATRules     []ImportedNATRule
	PortForwards []ImportedPortForward
	DHCPScopes   []ImportedDHCPScope
	StaticHosts  []ImportedStaticHost

	// Migration guidance
	Warnings     []string
	ManualSteps  []string
	Incompatible []string
}

// ImportedInterface represents an interface ready for migration.
type ImportedInterface struct {
	OriginalName string // pfSense name (wan, lan, opt1)
	OriginalIf   string // FreeBSD interface (em0, igb0)
	Description  string
	IPAddress    string
	Subnet       string
	IsDHCP       bool
	Zone         string // Suggested zone name
	BlockPrivate bool
	BlockBogons  bool

	// Migration notes
	NeedsMapping bool   // User must map to Linux interface
	SuggestedIf  string // Suggested Linux interface
}

// ImportedAlias represents a firewall alias.
type ImportedAlias struct {
	Name        string
	Type        string   // "host", "network", "port"
	Values      []string // IP addresses, networks, or ports
	Description string

	// For import
	CanImport   bool
	ConvertType string // "ipset", "service", etc.
}

// ImportedFilterRule represents a firewall rule ready for migration.
type ImportedFilterRule struct {
	Description string
	Action      string // "accept", "drop", "reject"
	Interface   string
	Direction   string
	Protocol    string
	Source      string
	SourcePort  string
	Destination string
	DestPort    string
	Log         bool
	Disabled    bool

	// Migration notes
	CanImport       bool
	ImportNotes     []string
	SuggestedPolicy string // Suggested policy name
}

// ImportedNATRule represents a NAT rule.
type ImportedNATRule struct {
	Type        string // "masquerade", "snat", "dnat"
	Interface   string
	Source      string
	Destination string
	Target      string
	Description string
	Disabled    bool

	CanImport   bool
	ImportNotes []string
}

// ImportedPortForward represents a port forward rule.
type ImportedPortForward struct {
	Interface    string
	Protocol     string
	ExternalPort string
	InternalIP   string
	InternalPort string
	Description  string
	Disabled     bool

	CanImport   bool
	ImportNotes []string
}

// ImportedDHCPScope represents a DHCP scope.
type ImportedDHCPScope struct {
	Interface    string
	RangeStart   string
	RangeEnd     string
	Gateway      string
	DNS          []string
	Domain       string
	Reservations []ImportedDHCPReservation
}

// ImportedDHCPReservation represents a static DHCP mapping.
type ImportedDHCPReservation struct {
	MAC         string
	IP          string
	Hostname    string
	Description string
}

// ImportedStaticHost represents a DNS host entry.
type ImportedStaticHost struct {
	Hostname string
	Domain   string
	IP       string
}
