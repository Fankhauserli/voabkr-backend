package handlers

import "github.com/Fankhauserli/voabkr-backend/sql/db"

type Handler struct {
	DB *db.Queries
}

func NewHandler(db *db.Queries) *Handler {
	return &Handler{
		DB: db,
	}
}
