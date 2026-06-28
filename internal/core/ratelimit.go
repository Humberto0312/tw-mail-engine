package core

import (
	"context"
	"sync"
	"time"
)

// RateLimiter — token bucket simple por clave (típicamente IP de salida o tenant).
// Cada clave tiene su propio bucket → un cliente ruidoso no afecta al resto.
// Es la base del warm-up: el cupo por IP se sube gradualmente día a día.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rpm     int // requests (envíos) por minuto permitidas por clave
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
}

// NewRateLimiter crea un limitador con un cupo de N envíos por minuto por clave.
func NewRateLimiter(perMinute int) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		rpm:     perMinute,
	}
}

// Wait bloquea hasta que haya 1 token disponible para esta clave.
func (r *RateLimiter) Wait(ctx context.Context, key string) error {
	for {
		if r.tryTake(key) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (r *RateLimiter) tryTake(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(r.rpm), lastRefill: now}
		r.buckets[key] = b
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	refill := elapsed * float64(r.rpm) / 60.0
	b.tokens += refill
	if b.tokens > float64(r.rpm) {
		b.tokens = float64(r.rpm)
	}
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
