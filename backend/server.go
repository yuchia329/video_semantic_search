package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/yuchia329/video_semantic_search/backend/graph"
	"github.com/yuchia329/video_semantic_search/backend/internal/kafka"
	"github.com/yuchia329/video_semantic_search/backend/internal/storage"
	"github.com/yuchia329/video_semantic_search/backend/internal/worker"
)

const defaultPort = "8080"

func main() {
	ctx := context.Background()

	// ── Config from environment ─────────────────────────────────────────────
	port := envOr("PORT", defaultPort)
	dbURL := envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/videosearch?sslmode=disable")
	kafkaBrokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:9092"), ",")
	kafkaTopic := envOr("KAFKA_TOPIC", "video-processing")
	s3Bucket := envOr("S3_BUCKET", "video-semantic-search")
	s3Region := envOr("AWS_REGION", "us-east-1")
	s3Endpoint := os.Getenv("S3_ENDPOINT") // optional, for local MinIO

	// ── Storage ─────────────────────────────────────────────────────────────
	db, err := storage.NewDB(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(ctx); err != nil {
		log.Fatalf("Failed to initialize schema: %v", err)
	}
	log.Println("Database schema initialized")

	s3, err := storage.NewS3Client(ctx, s3Bucket, s3Region, s3Endpoint)
	if err != nil {
		log.Fatalf("Failed to create S3 client: %v", err)
	}

	// ── Kafka ───────────────────────────────────────────────────────────────
	producer := kafka.NewProducer(kafkaBrokers, kafkaTopic)
	consumer := kafka.NewConsumer(kafkaBrokers, "video-processor-group", kafkaTopic)

	// ── Worker processor (runs in background) ────────────────────────────────
	proc := worker.NewProcessor(consumer, db, s3)
	go proc.Start(ctx)
	log.Println("Worker processor started")

	// ── GraphQL server ──────────────────────────────────────────────────────
	resolver := &graph.Resolver{DB: db, Producer: producer}
	srv := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})

	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	http.Handle("/", playground.Handler("Video Chat — GraphQL Playground", "/query"))
	http.Handle("/query", srv)

	log.Printf("Server ready at http://localhost:%s/ — GraphQL playground available", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
