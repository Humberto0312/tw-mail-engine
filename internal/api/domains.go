package api

import (
	"encoding/json"
	"net/http"
)

type domainRequest struct {
	Domain   string `json:"domain"`
	TenantID string `json:"tenantId"`
}

// POST /v1/domains — registra un dominio y devuelve los registros DNS a publicar.
func (s *Server) handleRegisterDomain(w http.ResponseWriter, r *http.Request) {
	if s.domainSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "verificación de dominios no disponible (sin Mongo)")
		return
	}
	var req domainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		writeError(w, http.StatusBadRequest, "domain es obligatorio")
		return
	}
	records, err := s.domainSvc.Register(r.Context(), req.Domain, req.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":  req.Domain,
		"status":  "pending",
		"records": records,
	})
}

// POST /v1/domains/verify — comprueba el DNS y marca verificado si está bien.
func (s *Server) handleVerifyDomain(w http.ResponseWriter, r *http.Request) {
	if s.domainSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "verificación de dominios no disponible (sin Mongo)")
		return
	}
	var req domainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		writeError(w, http.StatusBadRequest, "domain es obligatorio")
		return
	}
	ok, problems, err := s.domainSvc.Verify(r.Context(), req.Domain)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"domain": req.Domain, "status": "pending", "problems": problems})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": req.Domain, "status": "verified"})
}

type suppressRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// POST /v1/suppress — agrega un correo a la lista de supresión.
func (s *Server) handleSuppress(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "supresión no disponible (sin Mongo)")
		return
	}
	var req suppressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "email es obligatorio")
		return
	}
	if req.Reason == "" {
		req.Reason = "manual"
	}
	if err := s.store.Suppress(r.Context(), req.Email, req.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "suppressed", "email": req.Email})
}
