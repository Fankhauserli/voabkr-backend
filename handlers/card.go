package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) GetCards(c *gin.Context) {
	cards, err := h.DB.ListCards(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	var returnCards []types.Card

	for _, c := range cards {
		returnCards = append(returnCards, types.Card{
			ID:          uint(c.ID),
			DeckID:      uint(c.DeckID),
			KoreanWord:  c.KoreanWord,
			EnglishWord: c.EnglishWord,
			Context:     c.Context.String,
			Example:     c.ExampleSentence.String,
		})
	}

	c.JSON(http.StatusOK, returnCards)
}

func (h *Handler) CreateCards(c *gin.Context) {
	var req types.CardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Card: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	_, err := h.DB.CreateCard(c.Request.Context(), db.CreateCardParams{
		DeckID:          int64(req.DeckID),
		KoreanWord:      req.KoreanWord,
		EnglishWord:     req.EnglishWord,
		Context:         pgtype.Text{String: req.Context},
		ExampleSentence: pgtype.Text{String: req.Example},
	})

	if err != nil {
		log.Printf("[WARN] Card: failed to create Card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create Card", err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Card created"})
}

func (h *Handler) GetCardByID(c *gin.Context) {
	id, isValid := c.Params.Get("id")

	if !isValid {
		log.Printf("[WARN] Card: Failed to get card id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)

	if err != nil {
		log.Printf("[WARN] Card: Failed to get card id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id as int", err)
		return
	}

	card, err := h.DB.GetCard(c.Request.Context(), intId)

	if err != nil {
		log.Printf("[WARN] Card: Failed to get card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card", err)
		return
	}

	c.JSON(http.StatusOK, types.Card{
		ID:          uint(card.ID),
		DeckID:      uint(card.DeckID),
		KoreanWord:  card.KoreanWord,
		EnglishWord: card.EnglishWord,
		Context:     card.Context.String,
		Example:     card.ExampleSentence.String,
	})
}

func (h *Handler) UpdateCard(c *gin.Context) {
	var req types.CardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Card: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}
	id, isValid := c.Params.Get("id")

	if !isValid {
		log.Printf("[WARN] Card: Failed to get card id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)

	if err != nil {
		log.Printf("[WARN] Card: Failed to get card id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id as int", err)
		return
	}

	err = h.DB.UpdateCard(c.Request.Context(), db.UpdateCardParams{
		ID:              intId,
		DeckID:          int64(req.DeckID),
		KoreanWord:      req.KoreanWord,
		EnglishWord:     req.EnglishWord,
		Context:         pgtype.Text{String: req.Context},
		ExampleSentence: pgtype.Text{String: req.Example},
	})

	if err != nil {
		log.Printf("[WARN] Card: failed to update Card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update Card", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Card updated"})
}

func (h *Handler) DeleteCard(c *gin.Context) {
	id, isValid := c.Params.Get("id")

	if !isValid {
		log.Printf("[WARN] Card: Failed to get card id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)

	if err != nil {
		log.Printf("[WARN] Card: Failed to get card id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id as int", err)
		return
	}

	err = h.DB.DeleteCard(c.Request.Context(), intId)
	if err != nil {
		log.Printf("[WARN] Card: failed to delete Card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to delete Card", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Card deleted"})

}
