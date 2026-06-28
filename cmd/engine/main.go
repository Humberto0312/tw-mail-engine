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

	// 5. HTTP API
	srv := api.NewServer(cfg, mongoClient)
	if err := srv.Start(); err != nil {
		log.Error("arrancando HTTP: %v", err)
		os.Exit(1)
	}
	log.Info("tw-mail-engine listo — esperando órdenes de api-matrix")

	// 6. Esperar apagado
	<-ctx.Done()
	log.Info("señal de apagado recibida — cerrando con grace")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := srv.Stop(shutdownCtx); err != nil {
		log.Warn("apagando HTTP: %v", err)
	}
	log.Info("tw-mail-engine detenido. Hasta la próxima.")
}
