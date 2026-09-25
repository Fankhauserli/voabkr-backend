package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
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
	var req types.DeckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Deck: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	_, err := h.DB.CreateDeck(c.Request.Context(), db.CreateDeckParams{
		Name: req.Name,
		Type: db.DeckType(req.Type),
	})

	if err != nil {
		log.Printf("[WARN] Deck: failed to create Deck: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create Deck", err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Deck created"})
}

func (h *Handler) GetDeckByID(c *gin.Context) {
	id, isValid := c.Params.Get("id")

	if !isValid {
		log.Printf("[WARN] Deck: Failed to get Deck id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck id", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)

	if err != nil {
		log.Printf("[WARN] Deck: Failed to get Deck id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck id as int", err)
		return
	}

	deck, err := h.DB.GetDeck(c.Request.Context(), intId)
	if err != nil {
		log.Printf("[WARN] Deck: Failed to get Deck: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck", err)
		return
	}

	c.JSON(http.StatusOK, types.Deck{
		ID:   uint(deck.ID),
		Name: deck.Name,
		Type: string(deck.Type),
	})
}

func (h *Handler) UpdateDeck(c *gin.Context) {
	var req types.DeckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Deck: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	id, isValid := c.Params.Get("id")

	if !isValid {
		log.Printf("[WARN] Deck: Failed to get Deck id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck id", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)

	if err != nil {
		log.Printf("[WARN] Deck: Failed to get Deck id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck id as int", err)
		return
	}

	err = h.DB.UpdateDeck(c.Request.Context(), db.UpdateDeckParams{
		ID:   intId,
		Name: req.Name,
		Type: db.DeckType(req.Type),
	})

	if err != nil {
		log.Printf("[WARN] Deck: failed to update Deck: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update Deck", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Deck updated"})
}

func (h *Handler) DeleteDeck(c *gin.Context) {
	id, isValid := c.Params.Get("id")

	if !isValid {
		log.Printf("[WARN] Deck: Failed to get Deck id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck id", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)

	if err != nil {
		log.Printf("[WARN] Deck: Failed to get Deck id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck id as int", err)
		return
	}

	err = h.DB.DeleteDeck(c.Request.Context(), intId)
	if err != nil {
		log.Printf("[WARN] Deck: failed to delete Deck: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to delete Deck", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Deck deleted"})

}
