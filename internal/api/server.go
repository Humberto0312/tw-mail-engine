package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"tw-mail-engine/internal/config"
	"tw-mail-engine/internal/core"
)

// Server — HTTP API del motor de envíos.
// Rutas públicas: /health.
// Rutas protegidas (Bearer token): /v1/*  (las llama api-matrix).
type Server struct {
	cfg   *config.Config
	mongo *core.MongoClient
	log   *core.Logger
	srv   *http.Server
}

func NewServer(cfg *config.Config, mongo *core.MongoClient) *Server {
	return &Server{
		cfg:   cfg,
		mongo: mongo,
		log:   core.Root().With("http-api"),
	}
}

// Start — arranca el server HTTP. No bloquea.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Público
	mux.HandleFunc("GET /health", s.handleHealth)

	// Protegido (Bearer token)
	mux.HandleFunc("POST /v1/send", s.auth(s.handleSend))

	s.srv = &http.Server{
		Addr:         ":" + s.cfg.Port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	s.log.Info("HTTP escuchando en :%s", s.cfg.Port)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("HTTP server: %v", err)
		}
	}()
	return nil
}

// Stop — apaga el server con grace period.
func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// auth — middleware que exige Authorization: Bearer <ENGINE_API_TOKEN>.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
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
