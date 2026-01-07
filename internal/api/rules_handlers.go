package api

import (
	"net/http"
	"strconv"

	"grimm.is/glacic/internal/firewall"
	"grimm.is/glacic/internal/stats"
)

// RulesHandler provides enriched rule data for the ClearPath Policy Editor.
type RulesHandler struct {
	server    *Server
	collector *stats.Collector
	device    DeviceLookup // Optional device lookup for IP resolution
}

// NewRulesHandler creates a new rules handler.
func NewRulesHandler(s *Server, collector *stats.Collector, device DeviceLookup) *RulesHandler {
	return &RulesHandler{
		server:    s,
		collector: collector,
		device:    device,
	}
}

// HandleGetRules returns all policies with their rules.
// If ?with_stats=true, rules are enriched with runtime statistics and alias resolution.
func (h *RulesHandler) HandleGetRules(w http.ResponseWriter, r *http.Request) {
	if h.server.Config == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Configuration not loaded")
		return
	}

	withStats := r.URL.Query().Get("with_stats") == "true"
	resolver := NewAliasResolver(h.server.Config, h.device)

	// Collect all policies with enriched rules
	var response []PolicyWithStats

	for _, pol := range h.server.Config.Policies {
		polWithStats := PolicyWithStats{
			Policy: pol,
			Rules:  make([]RuleWithStats, 0, len(pol.Rules)),
		}

		for _, rule := range pol.Rules {
			enriched := RuleWithStats{
				PolicyRule: rule,
				PolicyFrom: pol.From,
				PolicyTo:   pol.To,
			}

			// Resolve aliases for UI pills
			enriched.ResolvedSrc = resolver.ResolveSource(rule)
			enriched.ResolvedDest = resolver.ResolveDest(rule)

			// Add stats if requested
			if withStats && h.collector != nil {
				ruleID := rule.ID
				if ruleID == "" {
					ruleID = rule.Name
				}

				if ruleID != "" {
					enriched.Stats.SparklineData = h.collector.GetSparkline(ruleID)
					enriched.Stats.Bytes = h.collector.GetTotalBytes(ruleID)
				}
			}

			// Generate nft syntax for power users
			if nftSyntax, err := firewall.BuildRuleExpression(rule); err == nil {
				enriched.GeneratedSyntax = nftSyntax
			}

			polWithStats.Rules = append(polWithStats.Rules, enriched)
		}

		response = append(response, polWithStats)
	}

	WriteJSON(w, http.StatusOK, response)
}

// HandleGetFlatRules returns all rules flattened (without policy grouping).
// Useful for the unified rule table view.
// Query params:
//   - with_stats=true: Include sparkline data
//   - group=string: Filter by rule GroupTag
//   - limit=int: Max rules to return
func (h *RulesHandler) HandleGetFlatRules(w http.ResponseWriter, r *http.Request) {
	if h.server.Config == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Configuration not loaded")
		return
	}

	withStats := r.URL.Query().Get("with_stats") == "true"
	groupFilter := r.URL.Query().Get("group")
	limitStr := r.URL.Query().Get("limit")
	limit := 0
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	resolver := NewAliasResolver(h.server.Config, h.device)
	var response []RuleWithStats
	count := 0

	for _, pol := range h.server.Config.Policies {
		for _, rule := range pol.Rules {
			// Apply group filter
			if groupFilter != "" && rule.GroupTag != groupFilter {
				continue
			}

			enriched := RuleWithStats{
				PolicyRule: rule,
				PolicyFrom: pol.From,
				PolicyTo:   pol.To,
			}

			// Resolve aliases
			enriched.ResolvedSrc = resolver.ResolveSource(rule)
			enriched.ResolvedDest = resolver.ResolveDest(rule)

			// Add stats if requested
			if withStats && h.collector != nil {
				ruleID := rule.ID
				if ruleID == "" {
					ruleID = rule.Name
				}

				if ruleID != "" {
					enriched.Stats.SparklineData = h.collector.GetSparkline(ruleID)
					enriched.Stats.Bytes = h.collector.GetTotalBytes(ruleID)
				}
			}

			// Generate nft syntax
			if nftSyntax, err := firewall.BuildRuleExpression(rule); err == nil {
				enriched.GeneratedSyntax = nftSyntax
			}

			response = append(response, enriched)
			count++

			if limit > 0 && count >= limit {
				break
			}
		}
		if limit > 0 && count >= limit {
			break
		}
	}

	WriteJSON(w, http.StatusOK, response)
}

// HandleGetRuleGroups returns a list of unique GroupTag values for filtering.
func (h *RulesHandler) HandleGetRuleGroups(w http.ResponseWriter, r *http.Request) {
	if h.server.Config == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Configuration not loaded")
		return
	}

	groups := make(map[string]int)
	for _, pol := range h.server.Config.Policies {
		for _, rule := range pol.Rules {
			if rule.GroupTag != "" {
				groups[rule.GroupTag]++
			}
		}
	}

	type GroupInfo struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	response := make([]GroupInfo, 0, len(groups))
	for name, count := range groups {
		response = append(response, GroupInfo{Name: name, Count: count})
	}

	WriteJSON(w, http.StatusOK, response)
}

// RegisterRoutes registers the rules API routes.
func (h *RulesHandler) RegisterRoutes(mux *http.ServeMux, require func(perm string, h http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/rules", require("read:firewall", http.HandlerFunc(h.HandleGetRules)))
	mux.Handle("GET /api/rules/flat", require("read:firewall", http.HandlerFunc(h.HandleGetFlatRules)))
	mux.Handle("GET /api/rules/groups", require("read:firewall", http.HandlerFunc(h.HandleGetRuleGroups)))
}

// RegisterRoutesNoAuth registers routes without authentication (for dev/test mode).
func (h *RulesHandler) RegisterRoutesNoAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rules", h.HandleGetRules)
	mux.HandleFunc("GET /api/rules/flat", h.HandleGetFlatRules)
	mux.HandleFunc("GET /api/rules/groups", h.HandleGetRuleGroups)
}
