package main

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Logger interface {
	Init() error
	Log(event, jobID, message string)
}

type MongoLogger struct {
	client     *mongo.Client
	database   string
	collection string
}

type MongoConfig struct {
	URI        string
	Database   string
	Collection string
}

func NewMongoLogger(config MongoConfig) (*MongoLogger, error) {
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(config.URI))
	if err != nil {
		return nil, err
	}
	logger := &MongoLogger{client: client, database: config.Database, collection: config.Collection}
	if err := logger.Init(); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return logger, nil
}

func (l *MongoLogger) Init() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return l.client.Ping(ctx, nil)
}

func (l *MongoLogger) Log(event, jobID, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = l.client.Database(l.database).Collection(l.collection).InsertOne(ctx, bson.M{
		"event":      event,
		"job_id":     jobID,
		"message":    message,
		"created_at": time.Now().UTC(),
	})
}

func mongoConfig() MongoConfig {
	return MongoConfig{
		URI:        envOr("MONGO_URI", "mongodb://localhost:27017"),
		Database:   envOr("MONGO_DATABASE", "video_processor_logs"),
		Collection: envOr("MONGO_COLLECTION", "application_logs"),
	}
}
