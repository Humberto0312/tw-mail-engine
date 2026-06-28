package api

import "net/http"

// handleHealth — sonda pública para Coolify / load balancer.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"service":  "tw-mail-engine",
		"hostname": s.cfg.Hostname,
	})
}
