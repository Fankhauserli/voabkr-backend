package handlers

import (
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
)

type Handler struct {
	DB    *db.Queries
	Cache cache.Cache
}

func NewHandler(db *db.Queries, c cache.Cache) *Handler {
	return &Handler{
		DB:    db,
		Cache: c,
	}
}
