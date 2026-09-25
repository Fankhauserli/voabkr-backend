package handlers

import (
	"log"
	"net/http"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func (h *Handler) CreateReview(c *gin.Context) {
	var req types.Review
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Review: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, ok := userID.(int64)

	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	_, err := h.DB.CreateUserCard(c.Request.Context(), db.CreateUserCardParams{
		UserID: intUserID,
		CardID: int64(req.CardID),
	})
	if err != nil {
		log.Printf("[WARN] Review: failed to create Review: %v", err)
		respondWithError(c, http.StatusBadRequest, "Failed to create Review", err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Review created"})
}

func (h *Handler) GetReviews(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, ok := userID.(int64)

	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	cards, err := h.DB.GetCardsDueForReview(c.Request.Context(), intUserID)
	if err != nil {
		log.Printf("[WARN] Review: failed to get Cards: %v", err)
		respondWithError(c, http.StatusInternalServerError, "failed to get Cards", err)
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

func (h *Handler) UpdateReview(c *gin.Context) {
	// implement
}

func (h *Handler) GetReviewsSince(c *gin.Context) {
	// implement
}
