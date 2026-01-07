package ctlplane

import (
	"fmt"
)

// GetUplinkGroups returns all uplink groups and their status
func (s *Server) GetUplinkGroups(args *Empty, reply *GetUplinkGroupsReply) error {
	if s.uplinkManager == nil {
		reply.Groups = []UplinkGroupStatus{}
		return nil
	}

	groups := s.uplinkManager.GetAllGroups()
	reply.Groups = make([]UplinkGroupStatus, len(groups))

	for i, g := range groups {
		activeUplinks := g.GetActiveUplinks()
		activeNames := make([]string, len(activeUplinks))
		for j, u := range activeUplinks {
			activeNames[j] = u.Name
		}

		allUplinks := g.GetUplinks()
		statusUplinks := make([]UplinkStatus, len(allUplinks))
		for j, u := range allUplinks {
			statusUplinks[j] = UplinkStatus{
				Name:      u.Name,
				Type:      string(u.Type),
				Interface: u.Interface,
				Gateway:   u.Gateway,
				// LocalIP:       u.LocalIP, // mapped to PublicIP
				// The struct in types.go has PublicIP but Uplink has LocalIP.
				// Let's assume PublicIP is the intention for LocalIP (SNAT IP).
				PublicIP:      u.LocalIP,
				Healthy:       u.Healthy,
				Enabled:       u.Enabled,
				Latency:       u.Latency.String(),
				PacketLoss:    u.PacketLoss, // Not in Uplink struct? Uplink has PacketLoss float64.
				Throughput:    u.Throughput,
				Tier:          u.Tier,
				Weight:        u.Weight,
				DynamicWeight: u.DynamicWeight,
				Tags:          u.Tags,
			}
		}

		reply.Groups[i] = UplinkGroupStatus{
			Name:            g.Name,
			Uplinks:         statusUplinks,
			ActiveUplinks:   activeNames,
			ActiveTier:      g.GetActiveTier(),
			FailoverMode:    string(g.GetFailoverMode()),
			LoadBalanceMode: string(g.GetLoadBalanceMode()),
		}
	}
	return nil
}

// SwitchUplink forces a switch to a specific uplink or best available
func (s *Server) SwitchUplink(args *SwitchUplinkArgs, reply *SwitchUplinkReply) error {
	if s.uplinkManager == nil {
		return fmt.Errorf("uplink manager not initialized")
	}

	group := s.uplinkManager.GetGroup(args.GroupName)
	if group == nil {
		return fmt.Errorf("uplink group %s not found", args.GroupName)
	}

	var err error
	if args.UplinkName == "" {
		err = group.SwitchToBest()
	} else {
		uplink := group.GetUplink(args.UplinkName)
		if uplink == nil {
			return fmt.Errorf("uplink %s not found in group %s", args.UplinkName, args.GroupName)
		}
		err = group.SwitchTo(uplink)
	}

	if err != nil {
		reply.Error = err.Error()
		return nil // Return nil error for RPC content? Standard pattern here seems to be return nil and fill reply.Error?
		// Looking at server.go, most return nil and fill reply.Error. Some return err.
		// best practice: if it's a "logical" error (e.g. not found), fill reply. if "system" error (rpc fail), return error.
		// "no healthy uplinks" is logical.
	}

	reply.Success = true
	if args.UplinkName == "" {
		reply.Message = fmt.Sprintf("Switched group %s to best uplink", args.GroupName)
	} else {
		reply.Message = fmt.Sprintf("Switched group %s to uplink %s", args.GroupName, args.UplinkName)
	}
	return nil
}

// ToggleUplink enables or disables an uplink
func (s *Server) ToggleUplink(args *ToggleUplinkArgs, reply *ToggleUplinkReply) error {
	if s.uplinkManager == nil {
		return fmt.Errorf("uplink manager not initialized")
	}

	group := s.uplinkManager.GetGroup(args.GroupName)
	if group == nil {
		return fmt.Errorf("uplink group %s not found", args.GroupName)
	}

	if found := group.SetUplinkEnabled(args.UplinkName, args.Enabled); !found {
		return fmt.Errorf("uplink %s not found in group %s", args.UplinkName, args.GroupName)
	}

	reply.Success = true
	return nil
}
