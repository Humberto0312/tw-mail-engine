package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"tw-mail-engine/internal/config"
	"tw-mail-engine/internal/core"
	"tw-mail-engine/internal/dkim"
	"tw-mail-engine/internal/domain"
	"tw-mail-engine/internal/queue"
	"tw-mail-engine/internal/sender"
	"tw-mail-engine/internal/store"
)

// Server — HTTP API del motor de envíos.
type Server struct {
	cfg       *config.Config
	mongo     *core.MongoClient
	store     *store.Store    // puede ser nil (sin Mongo)
	domainSvc *domain.Service // puede ser nil (sin Mongo)
	signer    *dkim.Signer    // firma por defecto del .env (fallback)
	mailer    *sender.Mailer
	q         *queue.Queue // cola (puede ser nil → envío síncrono)
	log       *core.Logger
	srv       *http.Server
}

func NewServer(cfg *config.Config, mongo *core.MongoClient, st *store.Store, domainSvc *domain.Service, signer *dkim.Signer, mailer *sender.Mailer, q *queue.Queue) *Server {
	return &Server{
		cfg:       cfg,
		mongo:     mongo,
		store:     st,
		domainSvc: domainSvc,
		signer:    signer,
		mailer:    mailer,
		q:         q,
		log:       core.Root().With("http-api"),
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /v1/send", s.auth(s.handleSend))
	mux.HandleFunc("POST /v1/domains", s.auth(s.handleRegisterDomain))
	mux.HandleFunc("POST /v1/domains/verify", s.auth(s.handleVerifyDomain))
	mux.HandleFunc("POST /v1/suppress", s.auth(s.handleSuppress))

	s.srv = &http.Server{
		Addr:         ":" + s.cfg.Port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	s.log.Info("HTTP escuchando en :%s", s.cfg.Port)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("HTTP server: %v", err)
		}
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || token != s.cfg.APIToken {
			writeError(w, http.StatusUnauthorized, "token inválido o ausente")
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func domainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
