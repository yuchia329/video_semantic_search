package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func NewDB(ctx context.Context, connectionString string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// InitSchema sets up the required tables if they don't exist.
// pgvector is no longer needed — the new architecture uses no vector embeddings.
func (db *DB) InitSchema(ctx context.Context) error {
	queries := []string{
		// Core video record. analysis holds the VLM's structured JSON analysis run at upload time.
		`CREATE TABLE IF NOT EXISTS videos (
			id          SERIAL PRIMARY KEY,
			title       TEXT NOT NULL,
			s3_key      TEXT NOT NULL,
			status      VARCHAR(50) DEFAULT 'uploaded',
			summary     TEXT,
			analysis    TEXT,
			created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,

		// One session per user/video conversation.
		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id         SERIAL PRIMARY KEY,
			video_id   INTEGER REFERENCES videos(id) ON DELETE CASCADE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,

		// Ordered chat turns for a session. role is 'user' or 'assistant'.
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id         SERIAL PRIMARY KEY,
			session_id INTEGER REFERENCES chat_sessions(id) ON DELETE CASCADE,
			role       VARCHAR(20) NOT NULL CHECK (role IN ('user', 'assistant')),
			content    TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, q := range queries {
		if _, err := db.Pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("failed to execute schema init query: %w", err)
		}
	}
	return nil
}

func (db *DB) Close() {
	db.Pool.Close()
}
