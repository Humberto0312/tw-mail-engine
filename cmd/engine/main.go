package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tw-mail-engine/internal/api"
	"tw-mail-engine/internal/config"
	"tw-mail-engine/internal/core"
	"tw-mail-engine/internal/dkim"
	"tw-mail-engine/internal/domain"
	"tw-mail-engine/internal/queue"
	"tw-mail-engine/internal/sender"
	"tw-mail-engine/internal/store"
)

const banner = `
╔════════════════════════════════════════════╗
║        tw-mail-engine v0.1.0 (Go)          ║
║   Motor propio de envío de email (MTA)     ║
╚════════════════════════════════════════════╝`

func main() {
	// 1. Config
	cfg, err := config.Load()
	if err != nil {
		println("ERROR FATAL config:", err.Error())
		os.Exit(1)
	}

	// 2. Logger raíz
	core.InitLogger(cfg.LogLevel)
	log := core.Root().With("engine")
	log.Info(banner)
	log.Info("Iniciando — hostname=%s port=%s ips=%v workers=%d",
		cfg.Hostname, cfg.Port, cfg.SendIPs, cfg.WorkerPoolSize)

	// 3. Contexto con cancel por señales
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 4. Mongo (misma DB que api-matrix) — NO fatal: si no está, el motor
	// igual levanta y responde /health (modo salud, sin procesar envíos).
	var mongoClient *core.MongoClient
	if cfg.MongoURI != "" {
		mongoClient, err = core.ConnectMongo(ctx, cfg.MongoURI, cfg.MongoDB)
		if err != nil {
			log.Warn("Mongo no disponible (%v) — arranco en modo salud, sin procesar envíos", err)
			mongoClient = nil
		}
	} else {
		log.Warn("MONGO_URI vacío — modo salud (solo /health), sin procesar envíos")
	}
	defer mongoClient.Close(context.Background())

	// 5. Firma DKIM (opcional). Si no hay llave, se envía sin firma.
	var signer *dkim.Signer
	if cfg.DKIMDomain != "" && cfg.DKIMKeyPath != "" {
		pemBytes, rerr := os.ReadFile(cfg.DKIMKeyPath)
		if rerr != nil {
			log.Warn("DKIM: no pude leer la llave en %s (%v) — envío SIN firma", cfg.DKIMKeyPath, rerr)
		} else if sg, serr := dkim.NewSigner(cfg.DKIMDomain, cfg.DKIMSelector, string(pemBytes)); serr != nil {
			log.Warn("DKIM: %v — envío SIN firma", serr)
		} else {
			signer = sg
			log.Info("DKIM activo — dominio=%s selector=%s", cfg.DKIMDomain, cfg.DKIMSelector)
		}
	} else {
		log.Warn("DKIM no configurado (DKIM_DOMAIN/DKIM_PRIVATE_KEY_PATH) — envío SIN firma")
	}

	// 6. Mailer (entrega SMTP por puerto 25)
	mailer := sender.NewMailer(cfg.Hostname, cfg.SendIPs)

	// 7. Store + servicio de dominios + cola (multi-tenant) — requieren Mongo.
	var st *store.Store
	var domainSvc *domain.Service
	var q *queue.Queue
	if mongoClient != nil {
		st = store.New(mongoClient)
		domainSvc = domain.NewService(st, cfg.DKIMSelector, cfg.PublicIP)
		q = queue.New(st, mailer, signer, cfg.DKIMDomain, cfg.Hostname, cfg.MaxDeliveryRetries, cfg.WarmupEnabled, cfg.PublicIP)
		q.Start(ctx)
		log.Info("multi-dominio + cola con reintentos + warm-up(%v) activos", cfg.WarmupEnabled)
	} else {
		log.Warn("sin Mongo — multi-dominio, cola y supresión deshabilitados (envío síncrono con dominio del .env)")
	}

	// 8. HTTP API
	srv := api.NewServer(cfg, mongoClient, st, domainSvc, signer, mailer, q)
	if err := srv.Start(); err != nil {
		log.Error("arrancando HTTP: %v", err)
		os.Exit(1)
	}
	log.Info("tw-mail-engine listo — esperando órdenes de api-matrix")

	// 8. Esperar apagado
	<-ctx.Done()
	log.Info("señal de apagado recibida — cerrando con grace")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := srv.Stop(shutdownCtx); err != nil {
		log.Warn("apagando HTTP: %v", err)
	}
	log.Info("tw-mail-engine detenido. Hasta la próxima.")
}
