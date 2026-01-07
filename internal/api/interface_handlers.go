package api

import (
	"encoding/json"
	"net/http"

	"grimm.is/glacic/internal/ctlplane"
)

// --- Interface Management Handlers ---

func (s *Server) handleInterfaces(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	interfaces, err := s.client.GetInterfaces()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, interfaces)
}

func (s *Server) handleAvailableInterfaces(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	interfaces, err := s.client.GetAvailableInterfaces()
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, interfaces)
}

func (s *Server) handleUpdateInterface(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteErrorCtx(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var args ctlplane.UpdateInterfaceArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if args.Name == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Interface name required")
		return
	}

	reply, err := s.client.UpdateInterface(&args)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteErrorCtx(w, r, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleCreateVLAN creates a new VLAN interface
func (s *Server) handleCreateVLAN(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var args ctlplane.CreateVLANArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	reply, err := s.client.CreateVLAN(&args)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteErrorCtx(w, r, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleDeleteVLAN deletes a VLAN interface
func (s *Server) handleDeleteVLAN(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	ifaceName := r.URL.Query().Get("name")
	if ifaceName == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "name parameter required")
		return
	}

	reply, err := s.client.DeleteVLAN(ifaceName)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteErrorCtx(w, r, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleCreateBond creates a new bond interface
func (s *Server) handleCreateBond(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	var args ctlplane.CreateBondArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		WriteErrorCtx(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	reply, err := s.client.CreateBond(&args)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteErrorCtx(w, r, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}

// handleDeleteBond deletes a bond interface
func (s *Server) handleDeleteBond(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		WriteErrorCtx(w, r, http.StatusServiceUnavailable, "Control plane not connected")
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		WriteErrorCtx(w, r, http.StatusBadRequest, "name parameter required")
		return
	}

	reply, err := s.client.DeleteBond(name)
	if err != nil {
		WriteErrorCtx(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !reply.Success {
		WriteErrorCtx(w, r, http.StatusBadRequest, reply.Error)
		return
	}

	WriteJSON(w, http.StatusOK, reply)
}
