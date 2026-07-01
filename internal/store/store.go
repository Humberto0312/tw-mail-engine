package store

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"tw-mail-engine/internal/core"
)

// Store — persistencia del motor (dominios, supresiones; mensajes/cola después).
type Store struct{ m *core.MongoClient }

func New(m *core.MongoClient) *Store { return &Store{m: m} }

func (s *Store) col(name string) *mongo.Collection { return s.m.Collection(name) }

// ---------- Dominios ----------

// Domain — un dominio de envío de un tenant, con su propia llave DKIM.
type Domain struct {
	Domain      string    `bson:"domain"`
	TenantID    string    `bson:"tenantId"`
	Selector    string    `bson:"selector"`
	DKIMPrivate string    `bson:"dkimPrivate"` // PEM
	DKIMPublic  string    `bson:"dkimPublic"`  // base64 DER
	Status      string    `bson:"status"`      // pending | verified
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

// ---------- Supresiones (rebotes duros, quejas, bajas) ----------

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
