package upnp

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"grimm.is/glacic/internal/clock"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/upgrade"
)

const (
	SSDPPort       = 1900
	SSDPAddr       = "239.255.255.250"
	IGDServiceType = "urn:schemas-upnp-org:service:WANIPConnection:1"
	RootDeviceType = "urn:schemas-upnp-org:device:InternetGatewayDevice:1"
)

// Config holds UPnP configuration
type Config struct {
	Enabled       bool
	ExternalIntf  string
	InternalIntfs []string
	SecureMode    bool // Only allow clients to map ports to themselves
}

// FirewallManager defines the methods required from the firewall manager
type FirewallManager interface {
	AddDynamicNATRule(rule config.NATRule) error
}

// Service manages the UPnP IGD service
type Service struct {
	config     Config
	fwMgr      FirewallManager
	httpServer *http.Server
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mappings   map[string]PortMapping // Maps external port -> mapping info
	mu         sync.Mutex

	wanIP string // Cached WAN IP

	upgradeMgr *upgrade.Manager
}

// SetUpgradeManager sets the upgrade manager for socket handoff.
func (s *Service) SetUpgradeManager(mgr *upgrade.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upgradeMgr = mgr
}

type PortMapping struct {
	ExternalPort   int
	InternalClient string
	InternalPort   int
	Protocol       string
	Description    string
	Expiration     time.Time
}

// NewService creates a new UPnP service
func NewService(cfg Config, fwMgr FirewallManager) *Service {
	return &Service{
		config:   cfg,
		fwMgr:    fwMgr,
		mappings: make(map[string]PortMapping),
	}
}

// Start starts the UPnP service
func (s *Service) Start(ctx context.Context) error {
	if !s.config.Enabled {
		return nil
	}

	ctx, s.cancel = context.WithCancel(ctx)

	// Start SSDP listeners on internal interfaces
	for _, ifaceName := range s.config.InternalIntfs {
		s.wg.Add(1)
		go s.runSSDP(ctx, ifaceName)
	}

	// Start SOAP HTTP Server
	// Bind to the IP of the first internal interface to prevent WAN exposure.
	// NOTE: This currently only supports UPnP on the primary internal interface.
	// To support multiple, we would need multiple listeners/servers.
	bindIP := "127.0.0.1" // Fallback
	if len(s.config.InternalIntfs) > 0 {
		ifaceName := s.config.InternalIntfs[0]
		if iface, err := net.InterfaceByName(ifaceName); err == nil {
			if addrs, err := iface.Addrs(); err == nil {
				for _, a := range addrs {
					if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
						bindIP = ipnet.IP.String()
						break
					}
				}
			}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/rootDesc.xml", s.handleRootDesc)
	mux.HandleFunc("/ctl/IPConn", s.handleIPConnControl)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:5555", bindIP),
		Handler: mux,
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Printf("[UPnP] SOAP server listening on %s:5555", bindIP)

		var listener net.Listener
		listName := "upnp-soap-tcp"

		if s.upgradeMgr != nil {
			if existing, ok := s.upgradeMgr.GetListener(listName); ok {
				listener = existing
				log.Printf("[UPnP] Inherited SOAP listener %s", listName)
			}
		}

		if listener == nil {
			var err error
			listener, err = net.Listen("tcp", fmt.Sprintf("%s:5555", bindIP))
			if err != nil {
				log.Printf("[UPnP] SOAP server error: failed to listen: %v", err)
				return
			}
			if s.upgradeMgr != nil {
				s.upgradeMgr.RegisterListener(listName, listener)
			}
		}

		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[UPnP] SOAP server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the UPnP service
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
	s.wg.Wait()
}

// runSSDP listens for M-SEARCH packets
func (s *Service) runSSDP(ctx context.Context, ifaceName string) {
	defer s.wg.Done()

	// Join Multicast group
	addr, err := net.ResolveUDPAddr("udp", SSDPAddr+":1900")
	if err != nil {
		log.Printf("[UPnP] Failed to resolve SSDP addr: %v", err)
		return
	}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		log.Printf("[UPnP] Interface %s not found", ifaceName)
		return
	}

	connName := fmt.Sprintf("upnp-ssdp-%s", ifaceName)
	var conn *net.UDPConn

	if s.upgradeMgr != nil {
		if existing, ok := s.upgradeMgr.GetPacketConn(connName); ok {
			if udp, ok := existing.(*net.UDPConn); ok {
				conn = udp
				log.Printf("[UPnP] Inherited SSDP socket %s", connName)
			}
		}
	}

	if conn == nil {
		var err error
		conn, err = net.ListenMulticastUDP("udp", iface, addr)
		if err != nil {
			log.Printf("[UPnP] Failed to listen multicast on %s: %v", ifaceName, err)
			return
		}
		if s.upgradeMgr != nil {
			s.upgradeMgr.RegisterPacketConn(connName, conn)
		}
	}

	defer conn.Close()

	buf := make([]byte, 2048)
	log.Printf("[UPnP] SSDP listening on %s", ifaceName)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(clock.Now().Add(1 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}

			msg := string(buf[:n])
			if strings.HasPrefix(msg, "M-SEARCH") && strings.Contains(msg, IGDServiceType) {
				// Send NOTIFY/Response
				s.sendSSDPResponse(ctx, src, iface, ifaceName)
			}
		}
	}
}

func (s *Service) sendSSDPResponse(ctx context.Context, dest *net.UDPAddr, iface *net.Interface, ifaceName string) {
	// Construct location URL
	// Need interface IP
	addrs, _ := iface.Addrs()
	var myIP string
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			myIP = ipnet.IP.String()
			break
		}
	}

	location := fmt.Sprintf("http://%s:5555/rootDesc.xml", myIP)

	resp := fmt.Sprintf("HTTP/1.1 200 OK\r\n"+
		"CACHE-CONTROL: max-age=1800\r\n"+
		"EXT:\r\n"+
		"LOCATION: %s\r\n"+
		"SERVER: Glacic/1.0 UPnP/1.1\r\n"+
		"ST: %s\r\n"+
		"USN: uuid:glacic-igd-1::%s\r\n"+
		"\r\n", location, IGDServiceType, IGDServiceType)

	// Send unicast response back to searcher? Or multicast?
	// SSDP M-SEARCH response is unicast UDP to sender.

	// We need a socket to send from.
	// Just use a random ephemeral port?
	c, err := net.DialUDP("udp", nil, dest)
	if err == nil {
		defer c.Close()
		c.Write([]byte(resp))
	}
}

// NOTE: SOAP handlers (handleRootDesc, handleIPConnControl) omitted for brevity in draft.
// They require XML parsing/marshaling.

// SOAP Request Structures
type Envelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    Body     `xml:"Body"`
}

type Body struct {
	AddPortMapping       *AddPortMapping       `xml:"AddPortMapping"`
	DeletePortMapping    *DeletePortMapping    `xml:"DeletePortMapping"`
	GetExternalIPAddress *GetExternalIPAddress `xml:"GetExternalIPAddress"`
}

type AddPortMapping struct {
	NewExternalPort    int    `xml:"NewExternalPort"`
	NewProtocol        string `xml:"NewProtocol"`
	NewInternalPort    int    `xml:"NewInternalPort"`
	NewInternalClient  string `xml:"NewInternalClient"`
	NewEnabled         bool   `xml:"NewEnabled"`
	NewPortMappingDesc string `xml:"NewPortMappingDescription"`
	NewLeaseDuration   int    `xml:"NewLeaseDuration"`
}

type DeletePortMapping struct {
	NewExternalPort int    `xml:"NewExternalPort"`
	NewProtocol     string `xml:"NewProtocol"`
}

type GetExternalIPAddress struct{}

// Handle Root Device Description
func (s *Service) handleRootDesc(w http.ResponseWriter, r *http.Request) {
	// Simple minimal IGD description
	// We need to inject the presentation URL or IP
	// For now, hardcode friendly name
	w.Header().Set("Content-Type", "text/xml")

	// Determine the interface address the request came in on
	// Simplification: just pick one

	tmpl := `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <friendlyName>Glacic Firewall</friendlyName>
    <manufacturer>Glacic</manufacturer>
    <manufacturerURL>https://github.com/glacic/glacic</manufacturerURL>
    <modelDescription>Glacic High Performance Firewall</modelDescription>
    <modelName>Glacic Router</modelName>
    <modelNumber>1.0</modelNumber>
    <UDN>uuid:glacic-igd-1</UDN>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:WANIPConnection.1</serviceId>
        <controlURL>/ctl/IPConn</controlURL>
        <eventSubURL>/evt/IPConn</eventSubURL>
        <SCPDURL>/scpd.xml</SCPDURL>
      </service>
    </serviceList>
  </device>
</root>`
	w.Write([]byte(tmpl))
}

func (s *Service) handleIPConnControl(w http.ResponseWriter, r *http.Request) {
	// Parse SOAP Action
	soapAction := r.Header.Get("SOAPACTION")
	// Verify action matches body if needed, or just log
	if soapAction != "" {
		// e.g. "urn:schemas-upnp-org:service:WANIPConnection:1#AddPortMapping"
	}

	var env Envelope
	if err := xml.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "Invalid XML", http.StatusBadRequest)
		return
	}

	if env.Body.AddPortMapping != nil {
		s.handleAddPortMapping(w, r, env.Body.AddPortMapping)
	} else if env.Body.DeletePortMapping != nil {
		s.handleDeletePortMapping(w, r, env.Body.DeletePortMapping)
	} else if env.Body.GetExternalIPAddress != nil {
		s.handleGetExternalIPAddress(w, r)
	} else {
		// Unknown action
		http.Error(w, "Unknown Action", http.StatusBadRequest)
	}
}

func (s *Service) handleAddPortMapping(w http.ResponseWriter, r *http.Request, args *AddPortMapping) {
	// 1. Security Check
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if s.config.SecureMode && host != args.NewInternalClient {
		log.Printf("[UPnP] Security: Denying map request for %s from %s", args.NewInternalClient, host)
		s.sendSOAPError(w, 718, "ConflictInMappingEntry") // Or generic security error
		return
	}

	// 2. Validate Port
	if args.NewExternalPort == 0 || args.NewInternalPort == 0 {
		s.sendSOAPError(w, 402, "Invalid Args")
		return
	}

	// 3. Add Rule to Firewall
	// We map external_port -> internal_client:internal_port (DNAT)
	// Currently Manager.AddDynamicNATRule takes config.NATRule.

	// We need to import "grimm.is/glacic/internal/config" to construct the rule.
	// Since we can't easily add import via ReplaceFileContent efficiently without context,
	// We will rely on existing imports or add it if missing in a separate step.
	// Assuming `firewall/internal/config` is NOT imported yet.
	// I will just construct the struct using explicit package if possible, or assume it's available.

	// WAIT: Manager is in `firewall` package. `AddDynamicNATRule` takes `config.NATRule`.
	// `config` is `firewall/internal/config`.

	// I'll skip the struct construction here and do it in next step with imports.
	// Just logging for now to clear the lint error on soapAction.

	// 3. Add Rule to Firewall
	// We map external_port -> internal_client:internal_port
	rule := config.NATRule{
		Type:        "dnat",
		Protocol:    strings.ToLower(args.NewProtocol),
		InInterface: s.config.ExternalIntf,
		DestPort:    fmt.Sprintf("%d", args.NewExternalPort),
		ToIP:        args.NewInternalClient,
		ToPort:      fmt.Sprintf("%d", args.NewInternalPort),
		Description: fmt.Sprintf("UPnP: %s", args.NewPortMappingDesc),
		// Comment: "Managed by UPnP",
	}

	if err := s.fwMgr.AddDynamicNATRule(rule); err != nil {
		log.Printf("[UPnP] Failed to add firewall rule: %v", err)
		s.sendSOAPError(w, 501, "ActionFailed")
		return
	}

	s.mu.Lock()
	key := fmt.Sprintf("%d/%s", args.NewExternalPort, args.NewProtocol)
	s.mappings[key] = PortMapping{
		ExternalPort:   args.NewExternalPort,
		InternalClient: args.NewInternalClient,
		InternalPort:   args.NewInternalPort,
		Protocol:       args.NewProtocol,
		Description:    args.NewPortMappingDesc,
		Expiration:     clock.Now().Add(time.Duration(args.NewLeaseDuration) * time.Second),
	}
	s.mu.Unlock()

	log.Printf("[UPnP] MAPPED %s:%d -> %s:%d (%s)",
		s.config.ExternalIntf, args.NewExternalPort,
		args.NewInternalClient, args.NewInternalPort, args.NewProtocol)

	// Response
	resp := `<u:AddPortMappingResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"></u:AddPortMappingResponse>`
	s.sendSOAPResponse(w, resp)
}

func (s *Service) handleDeletePortMapping(w http.ResponseWriter, r *http.Request, args *DeletePortMapping) {
	s.mu.Lock()
	key := fmt.Sprintf("%d/%s", args.NewExternalPort, args.NewProtocol)
	delete(s.mappings, key)
	s.mu.Unlock()

	log.Printf("[UPnP] UNMAPPED %d/%s", args.NewExternalPort, args.NewProtocol)

	resp := `<u:DeletePortMappingResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1"></u:DeletePortMappingResponse>`
	s.sendSOAPResponse(w, resp)
}

func (s *Service) handleGetExternalIPAddress(w http.ResponseWriter, r *http.Request) {
	// In MVP, just return 0.0.0.0 or try to fetch from interface
	if s.wanIP == "" {
		if iface, err := net.InterfaceByName(s.config.ExternalIntf); err == nil {
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
					s.wanIP = ipnet.IP.String()
					break
				}
			}
		}
	}

	resp := fmt.Sprintf(`<u:GetExternalIPAddressResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewExternalIPAddress>%s</NewExternalIPAddress>
</u:GetExternalIPAddressResponse>`, s.wanIP)
	s.sendSOAPResponse(w, resp)
}

func (s *Service) sendSOAPResponse(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/xml")
	resp := fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body>%s</s:Body>
</s:Envelope>`, body)
	w.Write([]byte(resp))
}

func (s *Service) sendSOAPError(w http.ResponseWriter, code int, desc string) {
	w.WriteHeader(http.StatusInternalServerError)
	body := fmt.Sprintf(`<s:Fault>
<faultcode>s:Client</faultcode>
<faultstring>UPnPError</faultstring>
<detail>
<UPnPError xmlns="urn:schemas-upnp-org:control-1-0">
<errorCode>%d</errorCode>
<errorDescription>%s</errorDescription>
</UPnPError>
</detail>
</s:Fault>`, code, desc)
	s.sendSOAPResponse(w, body)
}
