package store

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tw-mail-engine/internal/core"
)

// Store — persistencia del motor: dominios, supresiones y cola de mensajes.
type Store struct{ m *core.MongoClient }

func New(m *core.MongoClient) *Store { return &Store{m: m} }

func (s *Store) col(name string) *mongo.Collection { return s.m.Collection(name) }

// ---------- Dominios ----------

type Domain struct {
	Domain      string    `bson:"domain"`
	TenantID    string    `bson:"tenantId"`
	Selector    string    `bson:"selector"`
	DKIMPrivate string    `bson:"dkimPrivate"`
	DKIMPublic  string    `bson:"dkimPublic"`
	Status      string    `bson:"status"` // pending | verified
	CreatedAt   time.Time `bson:"createdAt"`
	VerifiedAt  time.Time `bson:"verifiedAt,omitempty"`
}

func (s *Store) SaveDomain(ctx context.Context, d Domain) error {
	d.Domain = strings.ToLower(d.Domain)
	_, err := s.col("mail_domains").UpdateOne(ctx,
		bson.M{"domain": d.Domain},
		bson.M{"$set": d},
		options.Update().SetUpsert(true),
	)
	return err
}

func (s *Store) GetDomain(ctx context.Context, domain string) (*Domain, error) {
	var d Domain
	err := s.col("mail_domains").FindOne(ctx, bson.M{"domain": strings.ToLower(domain)}).Decode(&d)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) MarkVerified(ctx context.Context, domain string) error {
	_, err := s.col("mail_domains").UpdateOne(ctx,
		bson.M{"domain": strings.ToLower(domain)},
		bson.M{"$set": bson.M{"status": "verified", "verifiedAt": time.Now()}},
	)
	return err
}

// ---------- Supresiones ----------

func (s *Store) IsSuppressed(ctx context.Context, email string) (bool, error) {
	n, err := s.col("mail_suppressions").CountDocuments(ctx, bson.M{"email": strings.ToLower(email)})
	return n > 0, err
}

func (s *Store) Suppress(ctx context.Context, email, reason string) error {
	email = strings.ToLower(email)
	_, err := s.col("mail_suppressions").UpdateOne(ctx,
		bson.M{"email": email},
		bson.M{"$set": bson.M{"email": email, "reason": reason, "createdAt": time.Now()}},
		options.Update().SetUpsert(true),
	)
	return err
}

// ---------- Cola de mensajes ----------

type Message struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	MessageID   string             `bson:"messageId"`
	TenantID    string             `bson:"tenantId"`
	CampaignID  string             `bson:"campaignId,omitempty"`
	FromName    string             `bson:"fromName"`
	FromEmail   string             `bson:"fromEmail"`
	ReplyTo     string             `bson:"replyTo,omitempty"`
	To          string             `bson:"to"`
	Subject     string             `bson:"subject"`
	HTML        string             `bson:"html"`
	Text        string             `bson:"text,omitempty"`
	Headers     map[string]string  `bson:"headers,omitempty"`
	DailyLimit  int                `bson:"dailyLimit,omitempty"` // tope diario de la empresa (0 = sin tope)
	Status      string             `bson:"status"`               // queued|sending|sent|failed
	Attempts    int                `bson:"attempts"`
	NextAttempt time.Time          `bson:"nextAttempt"`
	LastError   string             `bson:"lastError,omitempty"`
	CreatedAt   time.Time          `bson:"createdAt"`
	SentAt      time.Time          `bson:"sentAt,omitempty"`
}

func (s *Store) Enqueue(ctx context.Context, m Message) (string, error) {
	// Idempotencia: si ya existe un mensaje con este messageId, NO re-encolar.
	// Esto hace seguros los reintentos de api-matrix (un timeout de red no duplica).
	if m.MessageID != "" {
		var existing struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		if s.col("mail_messages").FindOne(ctx, bson.M{"messageId": m.MessageID}).Decode(&existing) == nil {
			return existing.ID.Hex(), nil
		}
	}
	m.Status = "queued"
	m.CreatedAt = time.Now()
	if m.NextAttempt.IsZero() {
		m.NextAttempt = time.Now()
	}
	res, err := s.col("mail_messages").InsertOne(ctx, m)
	if err != nil {
		return "", err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}
	return "", nil
}

// ClaimDue reclama (atómicamente) hasta `limit` mensajes vencidos, marcándolos
// "sending" para que un solo worker los procese.
func (s *Store) ClaimDue(ctx context.Context, limit int) ([]Message, error) {
	var out []Message
	opts := options.FindOneAndUpdate().
		SetSort(bson.M{"nextAttempt": 1}).
		SetReturnDocument(options.After)
	for i := 0; i < limit; i++ {
		var m Message
		err := s.col("mail_messages").FindOneAndUpdate(ctx,
			bson.M{"status": "queued", "nextAttempt": bson.M{"$lte": time.Now()}},
			bson.M{"$set": bson.M{"status": "sending"}},
			opts,
		).Decode(&m)
		if err == mongo.ErrNoDocuments {
			break
		}
		if err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *Store) MarkSent(ctx context.Context, id primitive.ObjectID) error {
	_, err := s.col("mail_messages").UpdateByID(ctx, id,
		bson.M{"$set": bson.M{"status": "sent", "sentAt": time.Now()}})
	return err
}

func (s *Store) Reschedule(ctx context.Context, id primitive.ObjectID, next time.Time, lastErr string, attempts int) error {
	_, err := s.col("mail_messages").UpdateByID(ctx, id,
		bson.M{"$set": bson.M{"status": "queued", "nextAttempt": next, "lastError": lastErr, "attempts": attempts}})
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id primitive.ObjectID, lastErr string, attempts int) error {
	_, err := s.col("mail_messages").UpdateByID(ctx, id,
		bson.M{"$set": bson.M{"status": "failed", "lastError": lastErr, "attempts": attempts}})
	return err
}

// ---------- Warm-up (topes diarios por IP) ----------

// WarmupToday devuelve la fecha de arranque de la IP y su conteo de envíos de
// HOY (reseteando el conteo si cambió el día). Crea el registro si no existe.
func (s *Store) WarmupToday(ctx context.Context, ip string) (time.Time, int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var doc struct {
		StartedAt time.Time `bson:"startedAt"`
		Day       string    `bson:"day"`
		Count     int       `bson:"count"`
	}
	err := s.col("mail_warmup").FindOne(ctx, bson.M{"ip": ip}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		now := time.Now()
		_, e := s.col("mail_warmup").InsertOne(ctx, bson.M{"ip": ip, "startedAt": now, "day": today, "count": 0})
		return now, 0, e
	}
	if err != nil {
		return time.Time{}, 0, err
	}
	if doc.Day != today {
		_, _ = s.col("mail_warmup").UpdateOne(ctx, bson.M{"ip": ip},
			bson.M{"$set": bson.M{"day": today, "count": 0}})
		return doc.StartedAt, 0, nil
	}
	return doc.StartedAt, doc.Count, nil
}

// WarmupInc suma 1 al conteo de hoy de la IP.
func (s *Store) WarmupInc(ctx context.Context, ip string) error {
	today := time.Now().UTC().Format("2006-01-02")
	_, err := s.col("mail_warmup").UpdateOne(ctx,
		bson.M{"ip": ip},
		bson.M{"$inc": bson.M{"count": 1}, "$set": bson.M{"day": today}},
		options.Update().SetUpsert(true),
	)
	return err
}

// ---------- Guardrails por empresa (tenant): límite diario + auto-pausa ----------

// TenantDaily devuelve (pausada, enviados hoy, rebotados hoy), reseteando los
// contadores diarios si cambió el día.
func (s *Store) TenantDaily(ctx context.Context, tenantID string) (bool, int, int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var doc struct {
		Paused  bool   `bson:"paused"`
		Day     string `bson:"day"`
		Sent    int    `bson:"sentToday"`
		Bounced int    `bson:"bouncedToday"`
	}
	e := s.col("mail_tenants").FindOne(ctx, bson.M{"tenantId": tenantID}).Decode(&doc)
	if e == mongo.ErrNoDocuments {
		_, ie := s.col("mail_tenants").InsertOne(ctx, bson.M{"tenantId": tenantID, "paused": false, "day": today, "sentToday": 0, "bouncedToday": 0})
		return false, 0, 0, ie
	}
	if e != nil {
		return false, 0, 0, e
	}
	if doc.Day != today {
		_, _ = s.col("mail_tenants").UpdateOne(ctx, bson.M{"tenantId": tenantID},
			bson.M{"$set": bson.M{"day": today, "sentToday": 0, "bouncedToday": 0}})
		return doc.Paused, 0, 0, nil
	}
	return doc.Paused, doc.Sent, doc.Bounced, nil
}

func (s *Store) TenantIncSent(ctx context.Context, tenantID string) error {
	today := time.Now().UTC().Format("2006-01-02")
	_, err := s.col("mail_tenants").UpdateOne(ctx, bson.M{"tenantId": tenantID},
		bson.M{"$inc": bson.M{"sentToday": 1}, "$set": bson.M{"day": today}},
		options.Update().SetUpsert(true))
	return err
}

func (s *Store) TenantIncBounced(ctx context.Context, tenantID string) error {
	today := time.Now().UTC().Format("2006-01-02")
	_, err := s.col("mail_tenants").UpdateOne(ctx, bson.M{"tenantId": tenantID},
		bson.M{"$inc": bson.M{"bouncedToday": 1}, "$set": bson.M{"day": today}},
		options.Update().SetUpsert(true))
	return err
}

func (s *Store) TenantPause(ctx context.Context, tenantID, reason string) error {
	_, err := s.col("mail_tenants").UpdateOne(ctx, bson.M{"tenantId": tenantID},
		bson.M{"$set": bson.M{"paused": true, "pausedReason": reason, "pausedAt": time.Now()}},
		options.Update().SetUpsert(true))
	return err
}

// ---------- Pool de IPs (IP dedicada por empresa) ----------

// TenantIP devuelve la IP de salida asignada a la empresa (IP dedicada) o "" si
// usa el pool compartido. Base para vender IP dedicada por cliente.
func (s *Store) TenantIP(ctx context.Context, tenantID string) (string, error) {
	var doc struct {
		IP string `bson:"ip"`
	}
	err := s.col("mail_ippool").FindOne(ctx, bson.M{"tenantId": tenantID}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return doc.IP, nil
}
