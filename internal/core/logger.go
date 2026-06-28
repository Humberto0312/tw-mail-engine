package core

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Niveles de log
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func parseLevel(s string) LogLevel {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l LogLevel) label() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO "
	case LevelWarn:
		return "WARN "
	case LevelError:
		return "ERROR"
	}
	return "?????"
}

// Logger — logger simple, legible, en español plano.
// Formato: 2026-06-28 15:00:00.123 INFO  [sender] dominio.com: mensaje
type Logger struct {
	mu     sync.Mutex
	w      io.Writer
	level  LogLevel
	prefix string // ej: "sender"
	scope  string // ej: "empresa.com"
}

var rootLogger *Logger
var rootOnce sync.Once

// InitLogger configura el logger raíz. Llamar 1x al iniciar.
func InitLogger(level string) {
	rootOnce.Do(func() {
		rootLogger = &Logger{
			w:     os.Stdout,
			level: parseLevel(level),
		}
	})
}

// Root devuelve el logger raíz.
func Root() *Logger {
	if rootLogger == nil {
		InitLogger("info")
	}
	return rootLogger
}

// With devuelve un logger derivado con un prefijo de subsistema.
func (l *Logger) With(prefix string) *Logger {
	return &Logger{w: l.w, level: l.level, prefix: prefix, scope: l.scope}
}

// For devuelve un logger con contexto de scope (dominio, tenant, etc.).
func (l *Logger) For(scope string) *Logger {
	return &Logger{w: l.w, level: l.level, prefix: l.prefix, scope: scope}
}

func (l *Logger) log(lvl LogLevel, msg string, args ...any) {
	if lvl < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	ts := time.Now().Format("2006-01-02 15:04:05.000")
	var prefix string
	if l.prefix != "" {
		prefix = fmt.Sprintf("[%s] ", l.prefix)
	}
	var scope string
	if l.scope != "" {
		scope = l.scope + ": "
	}

	formatted := fmt.Sprintf(msg, args...)
	fmt.Fprintf(l.w, "%s %s %s%s%s\n", ts, lvl.label(), prefix, scope, formatted)
}

func (l *Logger) Debug(msg string, args ...any) { l.log(LevelDebug, msg, args...) }
func (l *Logger) Info(msg string, args ...any)  { l.log(LevelInfo, msg, args...) }
func (l *Logger) Warn(msg string, args ...any)  { l.log(LevelWarn, msg, args...) }
func (l *Logger) Error(msg string, args ...any) { l.log(LevelError, msg, args...) }
