package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config — toda la configuración del motor de envíos en un solo struct.
type Config struct {
	// Mongo (misma DB que api-matrix)
	MongoURI string
	MongoDB  string

	// HTTP API
	Port     string
	LogLevel string

	// Identidad del nodo (debe coincidir con el rDNS de la IP de salida)
	Hostname string

	// Token compartido api-matrix → motor (Authorization: Bearer)
	APIToken string

	// IPs de salida del MTA. Vacío = IP por defecto del servidor.
	// Diseñado para crecer a pool multi-IP (IP dedicada por cliente).
	SendIPs []string

	// Entrega
	WorkerPoolSize     int
	MaxDeliveryRetries int

	// Webhook de vuelta a api-matrix (entregas/rebotes/quejas)
	BounceWebhookURL string

	// DKIM (firma del correo). Por ahora un solo dominio vía env; multi-dominio
	// por tenant (leído de Mongo) vendrá después.
	DKIMSelector string
	DKIMDomain   string // dominio que firma por defecto (fallback del .env)
	DKIMKeyPath  string // ruta al PEM de la llave privada (montado en el VPS)

	// IP pública usada para generar los registros SPF de los dominios de clientes.
	PublicIP string

	// Warm-up automático: topes diarios por IP que suben día a día.
	WarmupEnabled bool
}

// Load carga la config desde .env (si existe) + variables de entorno.
// Las env vars siempre ganan sobre .env.
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		MongoURI:           getEnv("MONGO_URI", ""),
		MongoDB:            getEnv("MONGO_DB", ""),
		Port:               getEnv("PORT", "8080"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		Hostname:           getEnv("ENGINE_HOSTNAME", "mta1.twilbox.com"),
		APIToken:           getEnv("ENGINE_API_TOKEN", ""),
		SendIPs:            getEnvList("SEND_IPS"),
		WorkerPoolSize:     getEnvInt("WORKER_POOL_SIZE", 8),
		MaxDeliveryRetries: getEnvInt("MAX_DELIVERY_RETRIES", 4),
		BounceWebhookURL:   getEnv("BOUNCE_WEBHOOK_URL", ""),
		DKIMSelector:       getEnv("DKIM_SELECTOR", "twmail"),
		DKIMDomain:         getEnv("DKIM_DOMAIN", ""),
		DKIMKeyPath:        getEnv("DKIM_PRIVATE_KEY_PATH", ""),
		PublicIP:           getEnv("PUBLIC_IP", ""),
		WarmupEnabled:      getEnvBool("WARMUP_ENABLED", true),
	}

	if cfg.APIToken == "" {
		return nil, fmt.Errorf("ENGINE_API_TOKEN requerido (sin él nadie puede pedir envíos)")
	}

	// Mongo es OPCIONAL al arrancar: el motor levanta y responde /health aunque
	// Mongo no esté configurado/disponible. Sin Mongo no procesa envíos (solo
	// salud); en cuanto haya URI válida, opera normal.
	if cfg.MongoURI != "" && cfg.MongoDB == "" {
		db, err := extractDBFromURI(cfg.MongoURI)
		if err != nil {
			return nil, fmt.Errorf("MONGO_DB no definido y no se pudo extraer del URI: %w", err)
		}
		cfg.MongoDB = db
	}

	return cfg, nil
}

// extractDBFromURI — extrae el nombre de la DB del path de un URI Mongo.
func extractDBFromURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		return "", fmt.Errorf("el URI no contiene nombre de DB en el path")
	}
	return db, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// getEnvList — lee una lista separada por comas, ignorando espacios y vacíos.
func getEnvList(key string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
