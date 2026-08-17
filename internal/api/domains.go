package api

import (
	"encoding/json"
	"net/http"
	"strconv"
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

// GET /v1/suppressions?query=&limit= — quién está bloqueado y por qué.
func (s *Server) handleListSuppressions(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "supresión no disponible (sin Mongo)")
		return
	}
	limit, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	items, err := s.store.ListSuppressions(r.Context(), r.URL.Query().Get("query"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, _ := s.store.CountSuppressions(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// DELETE /v1/suppress — saca una dirección de la lista.
//
// Es una decisión con consecuencias: si la dirección rebotó de verdad, volver
// a escribirle daña la reputación del dominio y acaba perjudicando a todos los
// envíos. Por eso lo hace una persona a mano y queda anotado en el registro,
// nunca un proceso automático.
func (s *Server) handleUnsuppress(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "supresión no disponible (sin Mongo)")
		return
	}
	var req suppressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "email es obligatorio")
		return
	}
	removed, err := s.store.Unsuppress(r.Context(), req.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		writeError(w, http.StatusNotFound, "esa dirección no estaba en la lista")
		return
	}
	s.log.Info("supresión liberada email=%s", req.Email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "released", "email": req.Email})
}
