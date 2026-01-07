package ntp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/beevik/ntp"

	"grimm.is/glacic/internal/config"
	"grimm.is/glacic/internal/logging"
	"grimm.is/glacic/internal/services"
	"grimm.is/glacic/internal/upgrade"
)

const (
	NTPVersion  = 4
	ModeClient  = 3
	ModeServer  = 4
	Offset      = 2208988800 // Seconds from 1900 to 1970
	DefaultPort = 123
)

// packet represents an NTP packet
type packet struct {
	Settings       uint8 // Li | Vn | Mode
	Stratum        uint8
	Poll           uint8
	Precision      int8
	RootDelay      uint32
	RootDispersion uint32
	ReferenceID    uint32
	RefTimeSec     uint32
	RefTimeFrac    uint32
	OrigTimeSec    uint32
	OrigTimeFrac   uint32
	RxTimeSec      uint32
	RxTimeFrac     uint32
	TxTimeSec      uint32
	TxTimeFrac     uint32
}

// Service implements the NTP service
type Service struct {
	mu         sync.RWMutex
	logger     *logging.Logger
	upgradeMgr *upgrade.Manager
	conn       net.PacketConn
	cancel     context.CancelFunc
	running    bool
	cfg        *config.NTPConfig
	wg         sync.WaitGroup
}

// NewService creates a new NTP service
func NewService(logger *logging.Logger) *Service {
	return &Service{
		logger: logger,
	}
}

// SetUpgradeManager sets the upgrade manager
func (s *Service) SetUpgradeManager(mgr *upgrade.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upgradeMgr = mgr
}

func (s *Service) Name() string {
	return "NTP"
}

// Start starts the service
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true

	// Client Sync Loop
	if s.cfg != nil && len(s.cfg.Servers) > 0 {
		s.wg.Add(1)
		go s.runClientLoop(ctx)
	}

	// Server Listener
	if err := s.startListener(); err != nil {
		s.logger.Error("Failed to start NTP listener", "error", err)
		// Don't fail matching if client mode works?
		// But usually we want listener for internal clients.
		// Return error?
		cancel()
		s.running = false
		return err
	}

	return nil
}

// startListener binds to UDP port 123 (with handoff support)
func (s *Service) startListener() error {
	linkName := "ntp-udp"
	var conn net.PacketConn

	if s.upgradeMgr != nil {
		if existing, ok := s.upgradeMgr.GetPacketConn(linkName); ok {
			conn = existing
			s.logger.Info("Inherited NTP socket", "name", linkName)
		}
	}

	if conn == nil {
		var err error
		// Listen on all interfaces
		conn, err = net.ListenPacket("udp", ":123")
		if err != nil {
			return fmt.Errorf("failed to bind :123: %w", err)
		}
		if s.upgradeMgr != nil {
			s.upgradeMgr.RegisterPacketConn(linkName, conn)
		}
	}
	s.conn = conn

	s.wg.Add(1)
	go s.serve(conn)
	return nil
}

// serve handles incoming NTP packets
func (s *Service) serve(conn net.PacketConn) {
	defer s.wg.Done()
	buf := make([]byte, 48) // NTP packet size

	for {
		if !s.running {
			return
		}

		// Set read deadline? No, blocking read is fine controlled by Close()
		// conn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			// Check for closed
			select {
			case <-s.canCancel():
				return
			default:
				if s.running {
					s.logger.Error("Read error", "error", err)
				}
				return
			}
		}

		if n < 48 {
			continue
		}

		// Handle packet asynchronously? Or sync for speed? Sync is fine for UDP.
		s.handlePacket(conn, addr, buf[:n])
	}
}

func (s *Service) canCancel() <-chan struct{} {
	// Helper to check context done?
	// Actually Start creates ctx which is canceled by Stop logic?
	// My Stop logic calls cancel().
	// But conn.Close() causes ReadFrom to return error.
	return nil
}

func (s *Service) handlePacket(conn net.PacketConn, peer net.Addr, data []byte) {
	// Fill info from req (Transmit Timestamp becomes Originate Timestamp)
	// We need to parse TxTime from request (bytes 40-48)
	mode := data[0] & 0x07
	if mode != ModeClient {
		return
	}

	ReceiveTime := time.Now()

	// Construct response
	resp := packet{
		Settings:       (0 << 6) | (NTPVersion << 3) | ModeServer, // LI=0, VN=4, Mode=4
		Stratum:        2,                                         // Secondary server
		Poll:           4,                                         // 16s min
		Precision:      -20,                                       // ~1us
		RootDelay:      0,
		RootDispersion: 0,
		ReferenceID:    0x4C4F434C, // LOCL
	}

	// Copy RxTime from Request to OrigTime in Response
	// The request's Transmit Timestamp (bytes 40-47) -> Response Originate Timestamp (24-31)
	reqTxTime := data[40:48]

	copy(data[24:32], reqTxTime)

	// RefTime = Now
	refTime := toNtpTime(ReceiveTime)

	// RecvTime = Now
	recvTime := toNtpTime(ReceiveTime)

	// TxTime = Now
	txTime := toNtpTime(time.Now())

	// We'll write directly to buffer to avoid struct encoding overhead/complexity
	// Re-use input buffer? No, better use new buffer
	out := make([]byte, 48)

	out[0] = resp.Settings
	out[1] = resp.Stratum
	out[2] = resp.Poll
	out[3] = byte(resp.Precision)
	// Root delay/disp zero
	binary.BigEndian.PutUint32(out[12:16], resp.ReferenceID)

	binary.BigEndian.PutUint32(out[16:20], refTime.Sec)
	binary.BigEndian.PutUint32(out[20:24], refTime.Frac)

	// Originate Timestamp = copied from Request Tx
	copy(out[24:32], reqTxTime)

	binary.BigEndian.PutUint32(out[32:36], recvTime.Sec)
	binary.BigEndian.PutUint32(out[36:40], recvTime.Frac)

	binary.BigEndian.PutUint32(out[40:44], txTime.Sec)
	binary.BigEndian.PutUint32(out[44:48], txTime.Frac)

	conn.WriteTo(out, peer)
}

type ntpTime struct {
	Sec  uint32
	Frac uint32
}

func toNtpTime(t time.Time) ntpTime {
	sec := uint32(t.Unix() + int64(Offset))
	frac := uint64(t.Nanosecond()) * (1 << 32) / 1000000000
	return ntpTime{Sec: sec, Frac: uint32(frac)}
}

// Client Loop
func (s *Service) runClientLoop(ctx context.Context) {
	defer s.wg.Done()

	interval := 4 * time.Hour
	if s.cfg.Interval != "" {
		if d, err := time.ParseDuration(s.cfg.Interval); err == nil {
			interval = d
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial sync
	s.syncTime()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncTime()
		}
	}
}

func (s *Service) syncTime() {
	if len(s.cfg.Servers) == 0 {
		return
	}

	server := s.cfg.Servers[0] // Just pick first for simplicity

	// Query NTP server using pure Go beevik/ntp library
	resp, err := ntp.Query(server)
	if err != nil {
		s.logger.Warn("Failed to query NTP server", "server", server, "error", err)
		return
	}

	// Calculate new time
	now := time.Now().Add(resp.ClockOffset)

	// Set system time using settimeofday syscall
	if err := setSystemTime(now); err != nil {
		s.logger.Warn("Failed to set system time", "error", err, "offset", resp.ClockOffset)
	} else {
		s.logger.Info("Time synced", "server", server, "offset", resp.ClockOffset.String(), "stratum", resp.Stratum)
	}
}

// Stop stops the service
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.cancel()
	if s.conn != nil {
		s.conn.Close()
	}
	s.wg.Wait()
	s.running = false
	return nil
}

// Reload reloads the service
func (s *Service) Reload(cfg *config.Config) (bool, error) {
	s.mu.Lock()

	// Stop existing
	if s.running {
		s.cancel() // Cancel context
		if s.conn != nil {
			s.conn.Close()
		}
		s.wg.Wait()
		s.running = false
	}

	// Update config
	if cfg.NTP == nil || !cfg.NTP.Enabled {
		s.mu.Unlock()
		return true, nil
	}
	s.cfg = cfg.NTP
	s.mu.Unlock() // Unlock before calling Start

	// Start
	return true, s.Start(context.Background())
}

// Status returns status
func (s *Service) Status() services.ServiceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return services.ServiceStatus{
		Name:    s.Name(),
		Running: s.running,
	}
}
