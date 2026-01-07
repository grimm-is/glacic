package dns

import (
	"grimm.is/glacic/internal/config"
	"testing"

	"github.com/miekg/dns"
)

func TestService_CreateRR_ExtendedTypes(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name     string
		qtype    uint16
		rec      config.DNSRecord
		validate func(dns.RR) bool
	}{
		{
			name:  "TXT Record",
			qtype: dns.TypeTXT,
			rec:   config.DNSRecord{Type: "TXT", Value: "v=spf1 -all"},
			validate: func(rr dns.RR) bool {
				txt, ok := rr.(*dns.TXT)
				return ok && len(txt.Txt) == 1 && txt.Txt[0] == "v=spf1 -all"
			},
		},
		{
			name:  "MX Record",
			qtype: dns.TypeMX,
			rec:   config.DNSRecord{Type: "MX", Value: "10 mail.example.com"},
			validate: func(rr dns.RR) bool {
				mx, ok := rr.(*dns.MX)
				return ok && mx.Preference == 10 && mx.Mx == "mail.example.com."
			},
		},
		{
			name:  "SRV Record",
			qtype: dns.TypeSRV,
			rec:   config.DNSRecord{Type: "SRV", Value: "10 60 5060 sip.example.com"},
			validate: func(rr dns.RR) bool {
				srv, ok := rr.(*dns.SRV)
				return ok && srv.Priority == 10 && srv.Weight == 60 && srv.Port == 5060 && srv.Target == "sip.example.com."
			},
		},
		{
			name:  "PTR Record",
			qtype: dns.TypePTR,
			rec:   config.DNSRecord{Type: "PTR", Value: "host.example.com"},
			validate: func(rr dns.RR) bool {
				ptr, ok := rr.(*dns.PTR)
				return ok && ptr.Ptr == "host.example.com."
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := dns.Question{Name: "example.com.", Qtype: tt.qtype, Qclass: dns.ClassINET}
			rr := s.createRR(q, tt.rec)
			if rr == nil {
				t.Fatal("createRR returned nil")
			}
			if !tt.validate(rr) {
				t.Errorf("Validation failed for %s", tt.name)
			}
		})
	}
}
