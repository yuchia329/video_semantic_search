package graph

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

import (
	"github.com/yuchia329/video_semantic_search/backend/internal/kafka"
	"github.com/yuchia329/video_semantic_search/backend/internal/storage"
)

type Resolver struct {
	DB       *storage.DB
	Producer *kafka.Producer
}
