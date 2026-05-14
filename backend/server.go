package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/rs/cors"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/yuchia329/video_semantic_search/backend/graph"
	"github.com/yuchia329/video_semantic_search/backend/internal/kafka"
	"github.com/yuchia329/video_semantic_search/backend/internal/storage"
	"github.com/yuchia329/video_semantic_search/backend/internal/worker"
)

const defaultPort = "8080"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	ctx := context.Background()

	// Initialize Database (pgvector)
	dbConnStr := "postgres://video_user:video_password@localhost:5432/video_db?sslmode=disable"
	db, err := storage.NewDB(ctx, dbConnStr)
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	if err := db.InitSchema(ctx); err != nil {
		log.Fatalf("failed to init db schema: %v", err)
	}

	// Initialize S3 (MinIO)
	s3Config := storage.S3Config{
		Region:          "us-east-1",
		AccessKeyID:     "admin",
		SecretAccessKey: "password",
		EndpointURL:     "http://localhost:9000",
		BucketName:      "videos",
	}
	s3Client, err := storage.NewS3Client(ctx, s3Config)
	if err != nil {
		log.Fatalf("failed to create S3 client: %v", err)
	}

	// if err := s3Client.InitBucket(ctx); err != nil {
	// 	log.Fatalf("failed to init S3 bucket: %v", err)
	// }

	// Initialize Kafka (Redpanda)
	brokers := []string{"localhost:9092"}
	topic := "video_processing"
	producer := kafka.NewProducer(brokers, topic)
	defer producer.Close()

	consumer := kafka.NewConsumer(brokers, "video-worker-group", topic)
	defer consumer.Close()

	// Start Background Worker
	processor := worker.NewProcessor(consumer, db, s3Client)
	go processor.Start(ctx)

	// Set up GraphQL Resolver with Dependencies
	resolver := &graph.Resolver{
		DB:       db,
		S3Client: s3Client,
		Producer: producer,
	}

	srv := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})

	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	// Add CORS middleware so Next.js frontend can communicate with it
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowCredentials: true,
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
	})

	http.Handle("/", playground.Handler("GraphQL playground", "/query"))
	http.Handle("/query", c.Handler(srv))

	log.Printf("connect to http://localhost:%s/ for GraphQL playground", port)

	// Ensure the HTTP server runs properly
	srvServer := &http.Server{
		Addr:              ":" + port,
		ReadHeaderTimeout: 3 * time.Second,
	}
	log.Fatal(srvServer.ListenAndServe())
}
