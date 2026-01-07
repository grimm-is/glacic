package dns

import (
	"github.com/miekg/dns"
)

// setDO sets the DNSSEC OK (DO) bit in the OPT record (EDNS0)
func setDO(r *dns.Msg) {
	o := r.IsEdns0()
	if o != nil {
		o.SetDo()
		return
	}

	r.SetEdns0(4096, true)
}

// validateResponse checks if the response is secure (AD bit) if requested
func validateResponse(r *dns.Msg) bool {
	// Simple check: if Authenticated Data (AD) bit is set, we trust the upstream
	return r.AuthenticatedData
}

// Note: Full local validation requires recursive logic and walking the chain of trust
// which is beyond the scope of a stub resolver/forwarder without heavier dependencies.
// We rely on the upstream resolver (Google, Cloudflare, Quad9) to do validation.
