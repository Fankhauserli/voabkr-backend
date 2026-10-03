package handlers

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
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

	intUserID, err := strconv.ParseInt(userID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	_, err = h.DB.CreateUserCard(c.Request.Context(), db.CreateUserCardParams{
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

	intUserID, err := strconv.ParseInt(userID.(string), 10, 64)

	if err != nil {
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
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(userID.(string), 10, 64)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	id, isValid := c.Params.Get("id")

	if !isValid {
		log.Printf("[WARN] Review: Failed to get card id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)

	if err != nil {
		log.Printf("[WARN] Review: Failed to get card id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id as int", err)
		return
	}

	var req types.ReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Review: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// determine the next review date based on the ease value and the efactor like sm2 algorithm

	// get current efactor and interval for the card
	card, err := h.DB.GetUserCard(c.Request.Context(), db.GetUserCardParams{
		UserID: intUserID,
		CardID: intId,
	})
	if err != nil {
		log.Printf("[WARN] Review: failed to get UserCard: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get UserCard", err)
		return
	}

	// lets first calculate the new efactor based on the ease value and the current efactor
	//  EF':=EF+(0.1-(5-q)*(0.08+(5-q)*0.02))
	// where:
	// EF' - new value of the E-Factor,
	// EF - old value of the E-Factor,
	// q - quality of the response in the 0-5 grade scale.
	// If EF is less than 1.3 then let EF be 1.3.

	newEfactor := card.Efactor + (0.1 - float64(5-req.Ease)*(0.08+(float64(5-req.Ease)*0.02)))
	if newEfactor < 1.3 {
		newEfactor = 1.3
	}

	// now we can calculate the new interval based on the new efactor and the current interval
	// I(1):=1
	// I(2):=6
	// for n>2: I(n):=I(n-1)*EF
	// where:
	// I(n) - inter-repetition interval after the n-th repetition (in days),
	// EF - E-Factor of a given item
	// If interval is a fraction, round it up to the nearest integer.

	var newInterval int32
	switch card.Interval {
	case 0:
		newInterval = 1
	case 1:
		newInterval = 6
	default:
		newInterval = int32(float64(card.Interval) * newEfactor)
	}

	// now we can update the user_card with the new efactor, interval and next_review_date
	err = h.DB.UpdateUserCard(c.Request.Context(), db.UpdateUserCardParams{
		UserID:       intUserID,
		CardID:       intId,
		Efactor:      newEfactor,
		Interval:     newInterval,
		NextReviewAt: pgtype.Timestamptz{Time: card.NextReviewAt.Time.AddDate(0, 0, int(newInterval)), Valid: true},
	})
	if err != nil {
		log.Printf("[WARN] Review: failed to update UserCard: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update UserCard", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Review updated"})
}

func (h *Handler) GetReviewsSince(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(userID.(string), 10, 64)

	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	since, isValid := c.Params.Get("time")

	if !isValid {
		log.Printf("[WARN] Review: Failed to get card since parameter")
		respondWithError(c, http.StatusInternalServerError, "Failed to get card since parameter", nil)
		return
	}

	sinceTime, err := time.Parse(time.RFC3339, since)

	if err != nil {
		log.Printf("[WARN] Review: Failed to get card since parameter as time: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card since parameter as time", err)
		return
	}

	cards, err := h.DB.GetCardsDueForReviewAfter(c.Request.Context(), db.GetCardsDueForReviewAfterParams{
		UserID:       intUserID,
		NextReviewAt: pgtype.Timestamptz{Time: sinceTime, Valid: true},
	})
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
