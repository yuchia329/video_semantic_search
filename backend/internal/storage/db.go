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

// InitSchema sets up all tables, runs idempotent migrations, and seeds demo rows.
func (db *DB) InitSchema(ctx context.Context) error {
	queries := []string{
		// Core video record.
		`CREATE TABLE IF NOT EXISTS videos (
			id          SERIAL PRIMARY KEY,
			title       TEXT NOT NULL,
			s3_key      TEXT NOT NULL,
			status      VARCHAR(50) DEFAULT 'uploaded',
			summary     TEXT,
			analysis    TEXT,
			user_id     VARCHAR(64) DEFAULT 'anonymous',
			is_demo     BOOLEAN DEFAULT FALSE,
			created_at  TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,

		// Idempotent column migrations (safe to re-run on every startup).
		`ALTER TABLE videos ADD COLUMN IF NOT EXISTS summary  TEXT;`,
		`ALTER TABLE videos ADD COLUMN IF NOT EXISTS analysis TEXT;`,
		`ALTER TABLE videos ADD COLUMN IF NOT EXISTS user_id  VARCHAR(64) DEFAULT 'anonymous';`,
		`ALTER TABLE videos ADD COLUMN IF NOT EXISTS is_demo  BOOLEAN DEFAULT FALSE;`,

		// Mark any existing rows with user_id = 'demo' as is_demo.
		`UPDATE videos SET is_demo = TRUE WHERE user_id = 'demo' AND is_demo = FALSE;`,

		// Chat sessions (one per video conversation).
		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id         SERIAL PRIMARY KEY,
			video_id   INTEGER REFERENCES videos(id) ON DELETE CASCADE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,

		// Ordered chat turns.
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id         SERIAL PRIMARY KEY,
			session_id INTEGER REFERENCES chat_sessions(id) ON DELETE CASCADE,
			role       VARCHAR(20) NOT NULL CHECK (role IN ('user', 'assistant')),
			content    TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,

		// Seed 5 demo videos (idempotent — only inserts if none exist yet).
		`INSERT INTO videos (title, s3_key, status, summary, user_id, is_demo)
		 SELECT title, s3_key, status, summary, 'demo', TRUE FROM (VALUES
		   ('Demo: Product Walkthrough',   'demo/product_walkthrough.mp4',   'completed',
		    'A guided tour of the product''s key features and how to get started.'),
		   ('Demo: Team Stand-up Meeting', 'demo/standup_meeting.mp4',       'completed',
		    'A 10-minute engineering stand-up covering sprint progress and blockers.'),
		   ('Demo: System Architecture',   'demo/system_architecture.mp4',   'completed',
		    'A technical deep-dive into the distributed system design and trade-offs.'),
		   ('Demo: User Research Review',  'demo/user_research_review.mp4',  'completed',
		    'Highlights from five user-interview sessions and key insights extracted.'),
		   ('Demo: Quarterly Planning',    'demo/quarterly_planning.mp4',    'completed',
		    'OKR review and roadmap discussion for the upcoming quarter.')
		 ) AS src(title, s3_key, status, summary)
		 WHERE NOT EXISTS (SELECT 1 FROM videos WHERE is_demo = TRUE);`,
	}

	for _, q := range queries {
		if _, err := db.Pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("failed to execute schema init query:\n%s\nerror: %w", q, err)
		}
	}
	return nil
}

func (db *DB) Close() {
	db.Pool.Close()
}
