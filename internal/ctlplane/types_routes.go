package ctlplane

// --- Routing ---

// Route represents a kernel routing table entry
type Route struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Interface   string `json:"interface"`
	Protocol    string `json:"protocol"` // e.g. "kernel", "boot", "static", "dhcp"
	Metric      int    `json:"metric"`
	Scope       string `json:"scope"`
	Src         string `json:"src"`
	Table       int    `json:"table"`
}

// GetRoutesReply is the response for GetRoutes
type GetRoutesReply struct {
	Routes []Route `json:"routes"`
	Error  string  `json:"error,omitempty"`
}
