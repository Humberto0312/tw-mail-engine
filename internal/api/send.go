package api

import (
	"encoding/json"
	"net/http"
)

// SendRequest — contrato que api-matrix manda al motor para entregar un correo.
// El FromEmail DEBE pertenecer a un dominio verificado del tenant (lo valida el
// motor antes de entregar); api-matrix ya compiló el HTML autoritativo.
type SendRequest struct {
	TenantID   string            `json:"tenantId"`             // instituto dueño del envío
	CampaignID string            `json:"campaignId,omitempty"` // campaña/secuencia origen
	MessageID  string            `json:"messageId,omitempty"`  // id idempotente (evita doble envío)
	FromName   string            `json:"fromName"`
	FromEmail  string            `json:"fromEmail"` // remitente, de un dominio verificado
	ReplyTo    string            `json:"replyTo,omitempty"`
	To         string            `json:"to"`
	Subject    string            `json:"subject"`
	HTML       string            `json:"html"`
	Text       string            `json:"text,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"` // List-Unsubscribe, etc.
}

// handleSend — recibe la orden de envío de api-matrix.
//
// Pipeline previsto (siguiente módulo): validar dominio verificado del tenant →
// elegir IP del pool (warm-up) → firmar DKIM con la llave del dominio →
// resolver MX del destinatario → entregar por SMTP puerto 25 → registrar estado
// y reportar rebotes/quejas al webhook de api-matrix.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	if req.FromEmail == "" || req.To == "" || req.Subject == "" {
		writeError(w, http.StatusBadRequest, "fromEmail, to y subject son obligatorios")
		return
	}

	s.log.Info("recibido /v1/send tenant=%s to=%s (pipeline pendiente)", req.TenantID, req.To)
	writeError(w, http.StatusNotImplemented, "pipeline de entrega aún no montado (siguiente módulo)")
}
