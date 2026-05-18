package graph

// This file will not be regenerated automatically.
// Add any dependencies you require here.

import (
	"context"
	"fmt"
	"strconv"

	"github.com/yuchia329/video_semantic_search/backend/graph/model"
	"github.com/yuchia329/video_semantic_search/backend/internal/kafka"
	"github.com/yuchia329/video_semantic_search/backend/internal/storage"
	"github.com/yuchia329/video_semantic_search/backend/internal/worker"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

const (
	// ContextKeyUserID is set by the user-ID middleware in server.go.
	ContextKeyUserID contextKey = "userID"
)

// userIDFromCtx retrieves the caller's user-ID from the request context.
// Falls back to "anonymous" when no ID is present.
func userIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyUserID).(string); ok && v != "" {
		return v
	}
	return "anonymous"
}

type Resolver struct {
	DB           *storage.DB
	Producer     *kafka.Producer
	WorkerConfig worker.Config // ML service endpoints; read from env in server.go
}

// dbQueryVideo fetches a single video by ID.
func dbQueryVideo(ctx context.Context, db *storage.DB, id string) (*model.Video, error) {
	v := &model.Video{}
	var vid int
	err := db.Pool.QueryRow(ctx,
		`SELECT id, title, status,
		        COALESCE(summary,''),
		        COALESCE(analysis,''),
		        COALESCE(user_id,'anonymous'),
		        is_demo,
		        created_at::text
		 FROM videos WHERE id=$1`, id,
	).Scan(&vid, &v.Title, &v.Status, &v.Summary, &v.Analysis, &v.UserID, &v.IsDemo, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("video not found: %w", err)
	}
	v.ID = strconv.Itoa(vid)
	return v, nil
}
