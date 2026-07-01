package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"tw-mail-engine/internal/dkim"
	"tw-mail-engine/internal/message"
)

// SendRequest — contrato que api-matrix manda al motor para entregar un correo.
type SendRequest struct {
	TenantID   string            `json:"tenantId"`
	CampaignID string            `json:"campaignId,omitempty"`
	MessageID  string            `json:"messageId,omitempty"`
	FromName   string            `json:"fromName"`
	FromEmail  string            `json:"fromEmail"`
	ReplyTo    string            `json:"replyTo,omitempty"`
	To         string            `json:"to"`
	Subject    string            `json:"subject"`
	HTML       string            `json:"html"`
	Text       string            `json:"text,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

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

	ctx := r.Context()

	// Supresión: no enviar a rebotes duros / quejas / bajas.
	if s.store != nil {
		if sup, _ := s.store.IsSuppressed(ctx, req.To); sup {
			writeError(w, http.StatusForbidden, "destinatario en lista de supresión")
			return
		}
	}

	// Firma DKIM del dominio del remitente (multi-dominio).
	signer, err := s.resolveSigner(ctx, req.FromEmail)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now()
	messageID := req.MessageID
	if messageID == "" {
		messageID = fmt.Sprintf("<%d@%s>", now.UnixNano(), s.cfg.Hostname)
	}

	raw := message.Build(message.Outgoing{
		FromName:  req.FromName,
		FromEmail: req.FromEmail,
		ReplyTo:   req.ReplyTo,
		To:        req.To,
		Subject:   req.Subject,
		HTML:      req.HTML,
		Text:      req.Text,
		Headers:   req.Headers,
	}, now, messageID)

	if signer != nil {
		if signed, serr := signer.Sign(raw); serr != nil {
			s.log.Warn("DKIM no pudo firmar (%v) — envío sin firma", serr)
		} else {
			raw = signed
		}
	}

	if err := s.mailer.Deliver(ctx, req.FromEmail, req.To, raw); err != nil {
		s.log.Warn("entrega fallida to=%s: %v", req.To, err)
		writeError(w, http.StatusBadGateway, "fallo en la entrega: "+err.Error())
		return
	}

	s.log.Info("entregado to=%s tenant=%s msgId=%s", req.To, req.TenantID, messageID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "messageId": messageID})
}

// resolveSigner devuelve el firmante DKIM del dominio del remitente.
// 1) dominio registrado y verificado en el store → su llave.
// 2) fallback: dominio del .env (mta1...) con la llave montada.
func (s *Server) resolveSigner(ctx context.Context, fromEmail string) (*dkim.Signer, error) {
	dom := domainOf(fromEmail)
	if dom == "" {
		return nil, fmt.Errorf("fromEmail inválido")
	}
	if s.store != nil {
		d, err := s.store.GetDomain(ctx, dom)
		if err == nil && d != nil {
			if d.Status != "verified" {
				return nil, fmt.Errorf("el dominio %s no está verificado", dom)
			}
			return dkim.NewSigner(d.Domain, d.Selector, d.DKIMPrivate)
		}
	}
	if s.signer != nil && dom == s.cfg.DKIMDomain {
		return s.signer, nil
	}
	return nil, fmt.Errorf("el dominio %s no está registrado; regístralo y verifícalo primero", dom)
}
