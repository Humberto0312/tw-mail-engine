package core

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoClient — wrapper liviano sobre el cliente Mongo oficial.
type MongoClient struct {
	Client *mongo.Client
	DB     *mongo.Database
}

// ConnectMongo abre conexión a Mongo con timeout y verifica con ping.
func ConnectMongo(ctx context.Context, uri, dbName string) (*MongoClient, error) {
	log := Root().With("mongo")
	log.Info("conectando a Mongo (db=%s)", dbName)

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	clientOpts := options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second).
		SetSocketTimeout(120 * time.Second)

	client, err := mongo.Connect(connectCtx, clientOpts)
	if err != nil {
		return nil, err
	}

	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPing()
	if err := client.Ping(pingCtx, nil); err != nil {
		return nil, err
	}

	log.Info("conexión a Mongo OK")
	return &MongoClient{Client: client, DB: client.Database(dbName)}, nil
}

// Close cierra la conexión a Mongo.
func (m *MongoClient) Close(ctx context.Context) {
	if m == nil || m.Client == nil {
		return
	}
	disconnectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = m.Client.Disconnect(disconnectCtx)
}

// Collection — atajo para acceder a una colección.
func (m *MongoClient) Collection(name string) *mongo.Collection {
	return m.DB.Collection(name)
}
