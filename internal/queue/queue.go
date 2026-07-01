package queue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tw-mail-engine/internal/core"
	"tw-mail-engine/internal/dkim"
	"tw-mail-engine/internal/message"
	"tw-mail-engine/internal/sender"
	"tw-mail-engine/internal/store"
)

// Queue — cola de envíos con reintentos. Un worker reclama mensajes vencidos,
// los entrega y reprograma los fallos temporales (greylist/4xx/red).
type Queue struct {
	st          *store.Store
	mailer      *sender.Mailer
	envSigner   *dkim.Signer
	envDomain   string
	hostname    string
	maxAttempts int
	warmup      bool
	warmupKey   string
	log         *core.Logger
}

func New(st *store.Store, mailer *sender.Mailer, envSigner *dkim.Signer, envDomain, hostname string, maxAttempts int, warmup bool, warmupKey string) *Queue {
	if maxAttempts < 1 {
		maxAttempts = 4
	}
	if warmupKey == "" {
		warmupKey = "default"
	}
	return &Queue{
		st: st, mailer: mailer, envSigner: envSigner, envDomain: envDomain,
		hostname: hostname, maxAttempts: maxAttempts, warmup: warmup, warmupKey: warmupKey,
		log: core.Root().With("queue"),
	}
}

func (q *Queue) Start(ctx context.Context) {
	q.log.Info("cola de envíos iniciada (maxAttempts=%d)", q.maxAttempts)
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				q.tick(ctx)
			}
		}
	}()
}

func (q *Queue) tick(ctx context.Context) {
	msgs, err := q.st.ClaimDue(ctx, 20)
	if err != nil {
		q.log.Warn("reclamando mensajes: %v", err)
		return
	}
	for _, m := range msgs {
		q.process(ctx, m)
	}
}

func (q *Queue) process(ctx context.Context, m store.Message) {
	attempts := m.Attempts + 1

	if sup, _ := q.st.IsSuppressed(ctx, m.To); sup {
		_ = q.st.MarkFailed(ctx, m.ID, "destinatario suprimido", attempts)
		return
	}

	// Warm-up: respetar el tope diario de la IP (difiere a mañana si se alcanzó).
	if q.warmup {
		startedAt, used, werr := q.st.WarmupToday(ctx, q.warmupKey)
		if werr == nil {
			limit := warmupCap(daysSince(startedAt))
			if used >= limit {
				_ = q.st.Reschedule(ctx, m.ID, nextDayUTC(), "warm-up: tope diario alcanzado", m.Attempts)
				q.log.Info("warm-up: tope %d/día alcanzado (IP %s) — %s diferido a mañana", limit, q.warmupKey, m.To)
				return
			}
		}
	}

	signer, err := q.resolveSigner(ctx, m.FromEmail)
	if err != nil {
		_ = q.st.MarkFailed(ctx, m.ID, err.Error(), attempts)
		q.log.Warn("firma to=%s: %v", m.To, err)
		return
	}

	raw := message.Build(message.Outgoing{
		FromName: m.FromName, FromEmail: m.FromEmail, ReplyTo: m.ReplyTo,
		To: m.To, Subject: m.Subject, HTML: m.HTML, Text: m.Text, Headers: m.Headers,
	}, time.Now(), m.MessageID)
	if signer != nil {
		if signed, e := signer.Sign(raw); e == nil {
			raw = signed
		}
	}

	de := q.mailer.Deliver(ctx, m.FromEmail, m.To, raw)
	if de == nil {
		_ = q.st.MarkSent(ctx, m.ID)
		if q.warmup {
			_ = q.st.WarmupInc(ctx, q.warmupKey)
		}
		q.log.Info("enviado to=%s intentos=%d", m.To, attempts)
		return
	}

	if de.Permanent {
		_ = q.st.MarkFailed(ctx, m.ID, de.Msg, attempts)
		_ = q.st.Suppress(ctx, m.To, "hard_bounce")
		q.log.Warn("rebote duro to=%s: %s (suprimido)", m.To, de.Msg)
		return
	}
	if attempts >= q.maxAttempts {
		_ = q.st.MarkFailed(ctx, m.ID, "agotados reintentos: "+de.Msg, attempts)
		q.log.Warn("agotado to=%s tras %d intentos: %s", m.To, attempts, de.Msg)
		return
	}

	delay := backoff(attempts)
	_ = q.st.Reschedule(ctx, m.ID, time.Now().Add(delay), de.Msg, attempts)
	q.log.Info("reintento to=%s intento=%d en %v (%s)", m.To, attempts, delay, de.Msg)
}

// backoff — reintentos espaciados (arregla greylisting: reintenta a los minutos).
func backoff(attempts int) time.Duration {
	mins := []time.Duration{2, 8, 20, 60, 120}
	i := attempts - 1
	if i < 0 {
		i = 0
	}
	if i >= len(mins) {
		i = len(mins) - 1
	}
	return mins[i] * time.Minute
}

// warmupCap — tope de envíos para el día N de calentamiento (rampa estilo industria).
func warmupCap(day int) int {
	caps := []int{50, 100, 250, 500, 1000, 2000, 4000, 7500, 12000, 20000, 30000, 45000, 65000, 90000}
	if day < 0 {
		day = 0
	}
	if day >= len(caps) {
		return 150000
	}
	return caps[day]
}

func daysSince(t time.Time) int {
	d := int(time.Since(t).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return d
}

// nextDayUTC — mañana 00:05 UTC (cuando el contador diario ya se reinició).
func nextDayUTC() time.Time {
	t := time.Now().UTC().Add(24 * time.Hour)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 5, 0, 0, time.UTC)
}

func (q *Queue) resolveSigner(ctx context.Context, fromEmail string) (*dkim.Signer, error) {
	dom := domainOf(fromEmail)
	if dom == "" {
		return nil, fmt.Errorf("fromEmail inválido")
	}
	d, err := q.st.GetDomain(ctx, dom)
	if err == nil && d != nil {
		if d.Status != "verified" {
			return nil, fmt.Errorf("el dominio %s no está verificado", dom)
		}
		return dkim.NewSigner(d.Domain, d.Selector, d.DKIMPrivate)
	}
	if q.envSigner != nil && dom == q.envDomain {
		return q.envSigner, nil
	}
	return nil, fmt.Errorf("el dominio %s no está registrado", dom)
}

func domainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
