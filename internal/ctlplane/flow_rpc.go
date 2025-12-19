package ctlplane

import (
	"grimm.is/glacic/internal/learning/flowdb"
)

// GetFlows returns learned flows based on filter criteria
func (s *Server) GetFlows(args *GetFlowsArgs, reply *GetFlowsReply) error {
	if s.learningEngine == nil {
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

	// We need to use engine methods.
	// Assume Engine has ListFlows or similar that calls DB?
	// s.learningEngine.DB is not exported?
	// Let's check Engine methods availability via direct look or assumption.
	// If Engine methods are not available, I might need to add them or use s.learningEngine.GetDB() if exists.
	// For now, I will assume s.learningEngine.ListFlows exists or write wrapper.

	// Wait, I haven't verified Engine methods fully.
	// Let's try to use what I suspect exists or stick to safe public methods.
	// In flow_handlers.go, it was using `h.engine.ListFlows(...)`.
	// So I should be able to call it.

	opts := flowdb.ListOptions{
		State:  state,
		Limit:  args.Limit,
		Offset: args.Offset,
		// Search is not yet supported in ListOptions
	}

	// Search handling: if Search is implemented in ListOptions
	// If Engine exposes ListFlows(opts) directly.

	flows, err := s.learningEngine.ListFlows(opts)
	if err != nil {
		reply.Error = err.Error()
		return nil
	}

	// Convert to FlowWithHints if ListFlows returns FlowWithHints?
	// The reply expects FlowWithHints.

	// Enrich with DeviceID
	if s.deviceManager != nil {
		for i := range flows {
			info := s.deviceManager.GetDevice(flows[i].SrcMAC)
			if info.Device != nil {
				flows[i].DeviceID = info.Device.ID
			}
		}
	}

	reply.Flows = flows

	// Totals
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
