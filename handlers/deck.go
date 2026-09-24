package handlers

import (
	"net/http"

	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
)

func (h *Handler) GetDecks(c *gin.Context) {
	decks, err := h.DB.ListDecks(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	var returnDecks []types.Deck

	for _, c := range decks {
		returnDecks = append(returnDecks, types.Deck{
			ID:   uint(c.ID),
			Name: c.Name,
			Type: string(c.Type),
		})
	}

	c.JSON(http.StatusOK, returnDecks)
}

func (h *Handler) CreateDeck(c *gin.Context) {
	// Implement logic to create a new deck
}

func (h *Handler) GetDeckByID(c *gin.Context) {
	// Implement logic to get a deck by ID
}

func (h *Handler) UpdateDeck(c *gin.Context) {
	// Implement logic to update a deck
}

func (h *Handler) DeleteDeck(c *gin.Context) {
	// Implement logic to delete a deck
}
