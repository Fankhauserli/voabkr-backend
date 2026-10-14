package handlers

import (
	"context"
	"errors"
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
		CardID: int64(req.CardId),
	})
	if err != nil {
		log.Printf("[WARN] Review: failed to create Review: %v", err)
		respondWithError(c, http.StatusBadRequest, "Failed to create Review", err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Review created"})
}

func (h *Handler) GetReviews(c *gin.Context) {
	intUserID, ok := callerID(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	deckIDStr := c.Query("deck_id")
	if deckIDStr == "" {
		deckIDStr = c.Query("deckId")
	}
	if deckIDStr == "" {
		deckIDStr = c.Query("deck")
	}
	var targetDeckID int64
	if deckIDStr != "" {
		targetDeckID, _ = strconv.ParseInt(deckIDStr, 10, 64)
	}

	// A study session over one Deck is settled by that Deck, before any Card is
	// read.
	if targetDeckID > 0 {
		if _, ok := h.requireReadableDeck(c, targetDeckID, intUserID); !ok {
			return
		}
	}

	cards, err := h.DB.GetCardsDueForReview(c.Request.Context(), intUserID)
	if err != nil {
		log.Printf("[WARN] Review: failed to get Cards: %v", err)
		respondWithError(c, http.StatusInternalServerError, "failed to get Cards", err)
		return
	}

	// The queue is the stored schedule's: the Cards whose Review is due at or
	// before now, in whatever order they are read — the order a Session offers
	// them in is the device's. Which new Cards a learner meets is its decision
	// too, so none are introduced here.
	if targetDeckID > 0 {
		cards = cardsOfDeck(cards, targetDeckID)
	}

	returnCards, err := h.cardsWithSchedules(c.Request.Context(), intUserID, cards)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedules", err)
		return
	}

	c.JSON(http.StatusOK, returnCards)
}

// SubmitGrades accepts Grades the device has already scheduled, storing the
// Review schedule it is given rather than computing one. Each entry is settled on
// its own, so one refused Grade is not a failed batch.
func (h *Handler) SubmitGrades(c *gin.Context) {
	caller, ok := callerID(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req types.GradeBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Grade: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// A batch that parsed to nothing is a client that meant to send something: it
	// would take a 200 as its Grades delivered and drop them. Unknown fields are
	// discarded on the way in, so a mistyped key arrives as exactly this.
	if len(req.Grades) == 0 {
		respondWithError(c, http.StatusBadRequest, "No Grades in the batch", nil)
		return
	}

	results := make([]*types.GradeResult, 0, len(req.Grades))
	for _, entry := range req.Grades {
		results = append(results, h.applyGrade(c.Request.Context(), caller, entry))
	}

	c.JSON(http.StatusOK, &types.GradeBatchResponse{Results: results})
}

// applyGrade stores one Grade the device scheduled, or says why it could not.
// Every refusal is this entry's alone.
func (h *Handler) applyGrade(ctx context.Context, caller int64, entry *types.GradeEntry) *types.GradeResult {
	result := &types.GradeResult{CardId: entry.CardId}

	grade, err := parseGrade(entry)
	if err != nil {
		result.Reason = err.Error()
		return result
	}

	card, err := h.DB.GetCard(ctx, int64(entry.CardId))
	if err != nil {
		result.Reason = "unknown Card"
		return result
	}

	// A Grade is only stored on a Deck the learner may read: a revoked DeckShare
	// leaves the Grade refused, and the device keeps it for a later Sync.
	if _, err := h.deckAccess(ctx, card.DeckID, caller); err != nil {
		result.Reason = "no access to this Deck"
		return result
	}

	stored, err := h.DB.ApplyGrade(ctx, db.ApplyGradeParams{
		UserID:         caller,
		CardID:         int64(entry.CardId),
		Efactor:        grade.efactor,
		Interval:       grade.interval,
		Repetitions:    grade.repetitions,
		LastReviewedAt: pgtype.Timestamptz{Time: grade.answeredAt, Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: grade.nextReviewAt, Valid: true},
	})
	if err != nil {
		log.Printf("[WARN] Grade: failed to store a Grade for Card %d: %v", entry.CardId, err)
		result.Reason = "could not store the Grade"
		return result
	}

	// A Grade answered no later than the one already stored is a replay: it is
	// accepted, so the device can drop it, and it changes nothing.
	result.Accepted = true
	if stored == 0 {
		result.Reason = "already recorded; the later Grade stands"
	}

	return result
}

// scheduledGrade is a Grade entry read into the shapes the store needs: the
// Review schedule the device computed, and the two moments it carries.
type scheduledGrade struct {
	interval     int32
	efactor      float64
	repetitions  int32
	answeredAt   time.Time
	nextReviewAt time.Time
}

// parseGrade reads a Grade entry, refusing anything the backend could not store
// exactly as given.
func parseGrade(entry *types.GradeEntry) (scheduledGrade, error) {
	schedule := entry.Schedule
	switch {
	case schedule == nil:
		return scheduledGrade{}, errors.New("no Review schedule")
	case entry.Grade < 1 || entry.Grade > 4:
		return scheduledGrade{}, errors.New("invalid Grade")
	case schedule.Interval < 0:
		return scheduledGrade{}, errors.New("invalid Interval")
	case schedule.Efactor <= 0:
		return scheduledGrade{}, errors.New("invalid Efactor")
	case schedule.Repetitions < 0:
		return scheduledGrade{}, errors.New("invalid repetitions")
	}

	answeredAt, err := time.Parse(time.RFC3339, entry.AnsweredAt)
	if err != nil {
		return scheduledGrade{}, errors.New("invalid answeredAt")
	}

	nextReviewAt, err := time.Parse(time.RFC3339, schedule.NextReviewAt)
	if err != nil {
		return scheduledGrade{}, errors.New("invalid nextReviewAt")
	}

	return scheduledGrade{
		interval:     schedule.Interval,
		efactor:      schedule.Efactor,
		repetitions:  schedule.Repetitions,
		answeredAt:   answeredAt,
		nextReviewAt: nextReviewAt,
	}, nil
}

// cardsOfDeck keeps the Cards belonging to one Deck.
func cardsOfDeck(cards []db.Card, deckID int64) []db.Card {
	kept := make([]db.Card, 0, len(cards))
	for _, card := range cards {
		if card.DeckID == deckID {
			kept = append(kept, card)
		}
	}
	return kept
}

// UpdateReview schedules a Grade here, the way this backend did before the
// device took the scheduler over. It stays for the clients built against it; a
// Grade sent to the batch route is stored as the device computed it.
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
		card, err = h.DB.CreateUserCard(c.Request.Context(), db.CreateUserCardParams{
			UserID: intUserID,
			CardID: intId,
		})
		if err != nil {
			log.Printf("[WARN] Review: failed to get or initialize UserCard: %v", err)
			respondWithError(c, http.StatusInternalServerError, "Failed to get UserCard", err)
			return
		}
	}

	// ── Anki-style 4-button SM-2 scheduling ──────────────────────────────────
	//
	// Button mapping (sent as ease 1–4):
	//   1 = Again  → relearning step: 1 min
	//   2 = Hard   → relearning step: 6 min  (or 1.2× current if graduated)
	//   3 = Good   → next graduated interval (or 10 min on first success)
	//   4 = Easy   → skip relearning, jump to 4-day minimum
	//
	// The backend stores next_review_at as a timestamp, so we set it relative
	// to NOW() (not relative to the previous next_review_at, which Anki used
	// for days but which breaks for sub-day steps).
	//
	// interval column (INT, days) is used only for graduated cards.
	// For relearning cards the interval stays 0 so we can detect them.

	// Update E-Factor for rated ease (Anki applies EF change on every answer).
	// Anki formula: EF' = EF + (0.1 - (3-q)*(0.08 + (3-q)*0.02))  (q in 0-4)
	// We remap our 1-4 to Anki's 0-4 scale: ankiQ = ease - 1
	ankiQ := req.Ease - 1 // 0=Again, 1=Hard, 2=Good, 3=Easy
	newEfactor := card.Efactor + (0.1 - float64(3-ankiQ)*(0.08+float64(3-ankiQ)*0.02))
	if newEfactor < 1.3 {
		newEfactor = 1.3
	}

	now := time.Now()
	var nextReviewAt time.Time
	var newInterval int32

	switch req.Ease {
	case 1: // Again – relearning: 1 minute
		newInterval = 0 // stays in relearning
		nextReviewAt = now.Add(1 * time.Minute)

	case 2: // Hard
		if card.Interval == 0 {
			// Relearning: 6-minute step
			newInterval = 0
			nextReviewAt = now.Add(6 * time.Minute)
		} else {
			// Graduated: 1.2× current interval (Anki Hard multiplier), min 1 day
			newInterval = int32(float64(card.Interval) * 1.2)
			if newInterval < 1 {
				newInterval = 1
			}
			nextReviewAt = now.AddDate(0, 0, int(newInterval))
		}

	case 3: // Good
		if card.Interval == 0 {
			// First successful recall from relearning: 10-minute step, then graduate
			newInterval = 0
			nextReviewAt = now.Add(10 * time.Minute)
		} else if card.Interval == 1 {
			// First graduation: 6 days (Anki default)
			newInterval = 6
			nextReviewAt = now.AddDate(0, 0, 6)
		} else {
			// Graduated: interval × EF
			newInterval = int32(float64(card.Interval) * newEfactor)
			if newInterval <= card.Interval {
				newInterval = card.Interval + 1 // always make progress
			}
			nextReviewAt = now.AddDate(0, 0, int(newInterval))
		}

	case 4: // Easy – skip relearning, jump to at least 4 days
		if card.Interval < 4 {
			newInterval = 4
		} else {
			// Graduated + Easy multiplier (Anki uses EF × 1.3 for Easy)
			newInterval = int32(float64(card.Interval) * newEfactor * 1.3)
		}
		if newInterval < 4 {
			newInterval = 4
		}
		nextReviewAt = now.AddDate(0, 0, int(newInterval))

	default:
		// Fallback: treat as Good
		newInterval = card.Interval + 1
		nextReviewAt = now.AddDate(0, 0, int(newInterval))
	}

	err = h.DB.UpdateUserCard(c.Request.Context(), db.UpdateUserCardParams{
		UserID:         intUserID,
		CardID:         intId,
		Efactor:        newEfactor,
		Interval:       newInterval,
		Repetitions:    card.Repetitions + 1,
		LastReviewedAt: pgtype.Timestamptz{Time: now, Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: nextReviewAt, Valid: true},
	})
	if err != nil {
		log.Printf("[WARN] Review: failed to update UserCard: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update UserCard", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Review updated"})
}

func (h *Handler) GetReviewsSince(c *gin.Context) {
	intUserID, ok := callerID(c)
	if !ok {
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

	deckIDStr := c.Query("deck_id")
	if deckIDStr == "" {
		deckIDStr = c.Query("deckId")
	}
	if deckIDStr == "" {
		deckIDStr = c.Query("deck")
	}
	var targetDeckID int64
	if deckIDStr != "" {
		targetDeckID, _ = strconv.ParseInt(deckIDStr, 10, 64)
	}

	if targetDeckID > 0 {
		if _, ok := h.requireReadableDeck(c, targetDeckID, intUserID); !ok {
			return
		}
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

	if targetDeckID > 0 {
		cards = cardsOfDeck(cards, targetDeckID)
	}

	returnCards, err := h.cardsWithSchedules(c.Request.Context(), intUserID, cards)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedules", err)
		return
	}

	c.JSON(http.StatusOK, returnCards)
}
