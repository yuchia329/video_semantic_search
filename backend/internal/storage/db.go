package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgx5 "github.com/pgvector/pgvector-go/pgx"
)

type DB struct {
	Pool *pgxpool.Pool
}

func NewDB(ctx context.Context, connectionString string) (*DB, error) {
	// Pre-initialize the vector extension before setting up the pool's AfterConnect hook
	// because pgx5.RegisterTypes will fail if the vector type doesn't exist yet.
	initConn, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("unable to connect for pre-init: %w", err)
	}
	_, err = initConn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector;")
	initConn.Close(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create vector extension: %w", err)
	}

	config, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database url: %w", err)
	}

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgx5.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// InitSchema sets up the vector extension and required tables if they don't exist
func (db *DB) InitSchema(ctx context.Context) error {
	queries := []string{
		`CREATE EXTENSION IF NOT EXISTS vector;`,
		`CREATE TABLE IF NOT EXISTS videos (
			id SERIAL PRIMARY KEY,
			title TEXT NOT NULL,
			s3_key TEXT NOT NULL,
			status VARCHAR(50) DEFAULT 'uploaded',
			summary TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS transcript_chunks (
			id SERIAL PRIMARY KEY,
			video_id INTEGER REFERENCES videos(id),
			start_time REAL,
			end_time REAL,
			text TEXT NOT NULL,
			type VARCHAR(20) DEFAULT 'audio', -- 'audio' or 'visual'
			embedding vector(1024) -- SigLIP and BGE-m3 might have different dimensions, assuming 1024 for BGE-m3 and 768 for SigLIP. We'll use 1024 or separate tables.
		);`,
        `CREATE TABLE IF NOT EXISTS visual_chunks (
			id SERIAL PRIMARY KEY,
			video_id INTEGER REFERENCES videos(id),
			timestamp REAL,
			text TEXT, -- VLM-generated caption of the frame
			embedding vector(1024) -- BGE-m3 text embedding of the caption
		);`,
	}

	for _, query := range queries {
		if _, err := db.Pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to execute schema initialization query: %w", err)
		}
	}
	return nil
}

func (db *DB) Close() {
	db.Pool.Close()
}
