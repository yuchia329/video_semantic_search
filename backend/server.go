package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

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
	//
	// Local:      use .env.local defaults (MinIO, localhost gRPC/HTTP targets)
	// Production: set these via Kubernetes ConfigMap + Secret
	port        := envOr("PORT",        defaultPort)
	dbURL       := envOr("DATABASE_URL", "postgresql://video_user:video_password@localhost:5432/video_db?sslmode=disable")
	kafkaBrokers := strings.Split(envOr("KAFKA_BROKERS", "localhost:9092"), ",")
	kafkaTopic  := envOr("KAFKA_TOPIC", "video-processing")
	s3Bucket    := envOr("S3_BUCKET",   "video-semantic-search")
	s3Region    := envOr("AWS_REGION",  "us-east-1")
	// S3_ENDPOINT: set to MinIO URL for local dev; leave empty for real AWS S3 in production.
	s3Endpoint  := envOr("S3_ENDPOINT", "http://localhost:9000")

	// ML service endpoints — overridden in production to point at the gpu-tunnel K8s Service.
	mlCfg := worker.Config{
		TranscriptionTarget: envOr("TRANSCRIPTION_TARGET", "localhost:8902"),
		EmotionTarget:       envOr("EMOTION_TARGET",       "localhost:8904"),
		VLLMURL:             envOr("VLLM_URL",             "http://localhost:8900/v1/chat/completions"),
		VLLMModel:           envOr("VLLM_MODEL",           "cyankiwi/Qwen3-VL-8B-Instruct-AWQ-4bit"),
	}

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

	broker := NewBroker()

	// ── Worker processor (background goroutine) ──────────────────────────────
	proc := worker.NewProcessor(consumer, db, s3, mlCfg)
	proc.OnStatusChange = func(videoID, status string) {
		broker.Broadcast(fmt.Sprintf(`{"video_id": "%s", "status": "%s"}`, videoID, status))
	}
	go proc.Start(ctx)
	log.Println("Worker processor started")

	// ── GraphQL server ──────────────────────────────────────────────────────
	resolver := &graph.Resolver{DB: db, Producer: producer, WorkerConfig: mlCfg}
	srv := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: resolver}))

	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{Cache: lru.New[string](100)})

	// All routes go through CORS + user-ID injection middleware.
	chain := func(h http.Handler) http.Handler {
		return corsMiddleware(userIDMiddleware(h))
	}

	http.Handle("/",       chain(playground.Handler("Video Chat — GraphQL Playground", "/query")))
	http.Handle("/query",  chain(srv))
	http.Handle("/upload", chain(uploadHandler(db, s3, producer)))
	http.Handle("/delete", chain(deleteHandler(db)))
	http.Handle("/stream", chain(streamHandler(db, s3)))
	http.Handle("/status-stream", chain(broker))

	log.Printf("Server ready at http://localhost:%s/", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// ─── Middleware ───────────────────────────────────────────────────────────────

// corsMiddleware adds permissive CORS headers (suitable for local dev).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// userIDMiddleware reads the X-User-ID header and injects it into the context.
func userIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-ID")
		if uid == "" {
			uid = "anonymous"
		}
		ctx := context.WithValue(r.Context(), graph.ContextKeyUserID, uid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ─── REST handlers ────────────────────────────────────────────────────────────

// uploadHandler accepts multipart/form-data (fields: file, title), streams the
// file into MinIO, enforces the 3-video per-user limit, and enqueues processing.
func uploadHandler(db *storage.DB, s3c *storage.S3Client, producer *kafka.Producer) http.Handler {
	type response struct {
		ID     int    `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
		S3Key  string `json:"s3_key"`
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			userID = "anonymous"
		}

		// Enforce 3-video limit.
		var count int
		if err := db.Pool.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM videos WHERE user_id=$1 AND is_demo=FALSE`, userID,
		).Scan(&count); err == nil && count >= 3 {
			http.Error(w, "upload limit reached: delete a video before uploading a new one", http.StatusConflict)
			return
		}

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "field 'file' is required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		title := r.FormValue("title")
		if title == "" {
			title = header.Filename
		}

		s3Key := fmt.Sprintf("raw/%d_%s", time.Now().UnixMilli(), header.Filename)

		if err := s3c.UploadFile(r.Context(), s3Key, file); err != nil {
			log.Printf("Upload to MinIO failed: %v", err)
			http.Error(w, "storage error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		var videoID int
		if err := db.Pool.QueryRow(r.Context(),
			`INSERT INTO videos (title, s3_key, status, user_id) VALUES ($1, $2, 'queued', $3) RETURNING id`,
			title, s3Key, userID,
		).Scan(&videoID); err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		type processEvent struct {
			VideoID string `json:"video_id"`
			S3Key   string `json:"s3_key"`
		}
		if err := producer.ProduceEvent(r.Context(), fmt.Sprintf("%d", videoID), processEvent{
			VideoID: fmt.Sprintf("%d", videoID),
			S3Key:   s3Key,
		}); err != nil {
			log.Printf("Failed to publish Kafka event for video %d: %v", videoID, err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response{ID: videoID, Title: title, Status: "queued", S3Key: s3Key})
	})
}

// deleteHandler deletes a video by ID=<query param>, enforcing ownership via X-User-ID.
func deleteHandler(db *storage.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id query param required", http.StatusBadRequest)
			return
		}
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			userID = "anonymous"
		}
		tag, err := db.Pool.Exec(r.Context(),
			`DELETE FROM videos WHERE id=$1 AND user_id=$2 AND is_demo=FALSE`, id, userID,
		)
		if err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "not found or not owned by you", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// streamHandler generates a short-lived pre-signed MinIO URL and redirects the
// browser to it.  MinIO handles HTTP Range requests natively, enabling seek.
func streamHandler(db *storage.DB, s3c *storage.S3Client) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id query param required", http.StatusBadRequest)
			return
		}

		var s3Key string
		if err := db.Pool.QueryRow(r.Context(), `SELECT s3_key FROM videos WHERE id=$1`, id).Scan(&s3Key); err != nil {
			http.Error(w, "video not found", http.StatusNotFound)
			return
		}

		url, err := s3c.GeneratePresignedDownloadURL(r.Context(), s3Key, 2*time.Hour)
		if err != nil {
			http.Error(w, "failed to generate stream URL: "+err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── SSE Event Broker ─────────────────────────────────────────────────────────

type Broker struct {
	Notifier       chan string
	newClients     chan chan string
	closingClients chan chan string
	clients        map[chan string]bool
}

func NewBroker() *Broker {
	b := &Broker{
		Notifier:       make(chan string, 1),
		newClients:     make(chan chan string),
		closingClients: make(chan chan string),
		clients:        make(map[chan string]bool),
	}
	go b.listen()
	return b
}

func (b *Broker) listen() {
	for {
		select {
		case s := <-b.newClients:
			b.clients[s] = true
		case s := <-b.closingClients:
			delete(b.clients, s)
		case event := <-b.Notifier:
			for clientMessageChan := range b.clients {
				select {
				case clientMessageChan <- event:
				default:
				}
			}
		}
	}
}

func (b *Broker) Broadcast(msg string) {
	b.Notifier <- msg
}

func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	messageChan := make(chan string)
	b.newClients <- messageChan

	defer func() {
		b.closingClients <- messageChan
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-messageChan:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
