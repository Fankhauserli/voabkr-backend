package handlers

import (
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/decks"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
)

type Handler struct {
	DB    *db.Queries
	Cache cache.Cache

	// Decks reads Decks: which entry answers one, the fetch on miss, and what a
	// write invalidates. No handler holds one of its keys.
	Decks *decks.Reader

	// Cards reads Cards the same way, Review schedules and all.
	Cards *cards.Reader
}

func NewHandler(db *db.Queries, c cache.Cache) *Handler {
	return &Handler{
		DB:    db,
		Cache: c,
		Decks: decks.NewReader(db, c),
		Cards: cards.NewReader(db, c),
	}
}
