package ctlplane

import (
	"log"

	"grimm.is/glacic/internal/learning/flowdb"
)

// GetFlows returns learned flows based on filter criteria
func (s *Server) GetFlows(args *GetFlowsArgs, reply *GetFlowsReply) error {
	log.Printf("[CTL] RPC GetFlows called state=%s limit=%d", args.State, args.Limit)
	if s.learningEngine == nil {
		log.Printf("[CTL] RPC GetFlows: learning engine not initialized")
		reply.Error = "learning engine not initialized"
		return nil
	}

	var state flowdb.FlowState
	switch args.State {
	case "pending":
		state = flowdb.StatePending
	case "allowed":
		state = flowdb.StateAllowed
	case "denied":
		state = flowdb.StateDenied
	}

	opts := flowdb.ListOptions{
		State:  state,
		Limit:  args.Limit,
		Offset: args.Offset,
	}

	log.Printf("[CTL] RPC GetFlows: calling ListFlows opts=%v", opts)
	flows, err := s.learningEngine.ListFlows(opts)
	if err != nil {
		log.Printf("[CTL] RPC GetFlows: ListFlows failed error=%v", err)
		reply.Error = err.Error()
		return nil
	}
	log.Printf("[CTL] RPC GetFlows: ListFlows returned count=%d", len(flows))

	if s.deviceManager != nil {
		for i := range flows {
			info := s.deviceManager.GetDevice(flows[i].SrcMAC)
			if info.Device != nil {
				flows[i].DeviceID = info.Device.ID
			}
		}
	}

	reply.Flows = flows

	stats, err := s.learningEngine.GetStats()
	if err == nil {
		reply.TotalCounts = stats
	}

	return nil
}

// ApproveFlow approves a pending flow
func (s *Server) ApproveFlow(args *FlowActionArgs, reply *FlowActionReply) error {
	if s.learningEngine == nil {
		reply.Error = "learning engine not initialized"
		return nil
	}

	err := s.learningEngine.AllowFlow(args.ID)
	if err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
	}
	return nil
}

// DenyFlow denies a pending flow
func (s *Server) DenyFlow(args *FlowActionArgs, reply *FlowActionReply) error {
	if s.learningEngine == nil {
		reply.Error = "learning engine not initialized"
		return nil
	}

	err := s.learningEngine.DenyFlow(args.ID)
	if err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
	}
	return nil
}

// DeleteFlow deletes a flow
func (s *Server) DeleteFlow(args *FlowActionArgs, reply *FlowActionReply) error {
	if s.learningEngine == nil {
		reply.Error = "learning engine not initialized"
		return nil
	}

	err := s.learningEngine.DeleteFlow(args.ID)
	if err != nil {
		reply.Error = err.Error()
	} else {
		reply.Success = true
	}
	return nil
}
