//go:build !linux
// +build !linux

package network

import "fmt"

// NFTManager abstracts nftables operations for uplink management.
type NFTManager interface {
	AddMarkRule(chain, srcNet string, ctState string, mark uint32, comment string) error
	AddNumgenMarkRule(chain, srcNet string, weights []NumgenWeight, comment string) error
	AddConnmarkRestore(chain, iface string) error
	AddSNAT(chain string, mark uint32, oif, snatIP string) error
	DeleteRulesByComment(chain, commentPrefix string) error
	Flush() error
}

// NumgenWeight represents a weight entry for numgen load balancing
type NumgenWeight struct {
	Mark   uint32
	Weight int
}

// StubNFTManager is a no-op implementation for non-Linux systems
type StubNFTManager struct{}

// NewStubNFTManager creates a stub NFT manager
func NewStubNFTManager() *StubNFTManager {
	return &StubNFTManager{}
}

func (m *StubNFTManager) AddMarkRule(chain, srcNet string, ctState string, mark uint32, comment string) error {
	return fmt.Errorf("nftables not supported on this platform")
}

func (m *StubNFTManager) AddNumgenMarkRule(chain, srcNet string, weights []NumgenWeight, comment string) error {
	return fmt.Errorf("nftables not supported on this platform")
}

func (m *StubNFTManager) AddConnmarkRestore(chain, iface string) error {
	return fmt.Errorf("nftables not supported on this platform")
}

func (m *StubNFTManager) AddSNAT(chain string, mark uint32, oif, snatIP string) error {
	return fmt.Errorf("nftables not supported on this platform")
}

func (m *StubNFTManager) DeleteRulesByComment(chain, commentPrefix string) error {
	return fmt.Errorf("nftables not supported on this platform")
}

func (m *StubNFTManager) Flush() error {
	return nil
}
