package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/reviews"
	"github.com/Fankhauserli/voabkr-backend/scheduling"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) CreateReview(c *gin.Context) {
	var req types.Review
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Review: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}

	_, err := h.DB.CreateUserCard(c.Request.Context(), db.CreateUserCardParams{
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
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}

	targetDeckID := queueDeckID(c)

	// A study session over one Deck is settled by that Deck, before any Card is
	// read.
	if targetDeckID > 0 {
		if _, ok := h.requireReadableDeck(c, targetDeckID, intUserID); !ok {
			return
		}
	}

	// The queue is the module's: the Cards whose Review is due at or before now,
	// filtered to the named Deck in SQL and carrying the caller's own schedules.
	// The order a Session offers them in is the device's.
	queued, err := h.Reviews.Queue(c.Request.Context(), reviews.Query{
		Caller: intUserID,
		DeckID: targetDeckID,
	})
	if err != nil {
		respondQueueError(c, err)
		return
	}

	c.JSON(http.StatusOK, queued)
}

// queueDeckID reads the Deck a queue read is filtered by. All three spellings the
// wire has carried are accepted; a missing one — and anything that is not a
// positive id, as before the filter moved into SQL — means every readable Deck.
func queueDeckID(c *gin.Context) int64 {
	deckIDStr := c.Query("deck_id")
	if deckIDStr == "" {
		deckIDStr = c.Query("deckId")
	}
	if deckIDStr == "" {
		deckIDStr = c.Query("deck")
	}
	if deckIDStr == "" {
		return 0
	}

	deckID, err := strconv.ParseInt(deckIDStr, 10, 64)
	if err != nil || deckID <= 0 {
		return 0
	}
	return deckID
}

// respondQueueError answers a failed queue read in the words these routes have
// always used for each failure.
func respondQueueError(c *gin.Context, err error) {
	if errors.Is(err, cards.ErrSchedules) {
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedules", err)
		return
	}

	log.Printf("[WARN] Review: failed to get Cards: %v", err)
	respondWithError(c, http.StatusInternalServerError, "failed to get Cards", err)
}

// SubmitGrades accepts Grades the device has already scheduled, storing the
// Review schedule it is given rather than computing one. Each entry is settled on
// its own, so one refused Grade is not a failed batch.
func (h *Handler) SubmitGrades(c *gin.Context) {
	caller, ok := requireCaller(c)
	if !ok {
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

// UpdateReview schedules a Grade here, the way this backend did before the
// device took the scheduler over. It stays for the clients built against it; a
// Grade sent to the batch route is stored as the device computed it.
func (h *Handler) UpdateReview(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
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

	// An out-of-range ease is not a Grade this route can schedule; it used to
	// fall through to a default schedule and be recorded as an answer.
	grade, isGrade := scheduling.ParseGrade(req.Ease)
	if !isGrade {
		respondWithError(c, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	// The Card's Deck decides who may study it, so the Card is read before any
	// Review is touched: a Grade on a Deck the caller cannot read is refused, and
	// creates nothing.
	card, err := h.Cards.ByID(c.Request.Context(), intUserID, intId)
	switch {
	case errors.Is(err, cards.ErrNoSuchCard):
		respondWithError(c, http.StatusNotFound, "Card not found", err)
		return
	case errors.Is(err, cards.ErrSchedules):
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedule", err)
		return
	case err != nil:
		log.Printf("[WARN] Review: failed to get Card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card", err)
		return
	}

	if _, ok := h.requireReadableDeck(c, card.DeckID, intUserID); !ok {
		return
	}

	// The stored Review is the state scheduling continues from. Only a Review
	// that is not there is created; any other read failure fails the request,
	// since starting a fresh schedule would hide the database's answer.
	review, err := h.DB.GetUserCard(c.Request.Context(), db.GetUserCardParams{
		UserID: intUserID,
		CardID: intId,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		review, err = h.DB.CreateUserCard(c.Request.Context(), db.CreateUserCardParams{
			UserID: intUserID,
			CardID: intId,
		})
		if err != nil {
			log.Printf("[WARN] Review: failed to initialize UserCard: %v", err)
			respondWithError(c, http.StatusInternalServerError, "Failed to get UserCard", err)
			return
		}
	case err != nil:
		log.Printf("[WARN] Review: failed to get UserCard: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get UserCard", err)
		return
	}

	now := time.Now()
	next := scheduling.Next(scheduling.State{
		Interval:    review.Interval,
		Efactor:     review.Efactor,
		Repetitions: review.Repetitions,
	}, grade, now)

	err = h.DB.UpdateUserCard(c.Request.Context(), db.UpdateUserCardParams{
		UserID:         intUserID,
		CardID:         intId,
		Efactor:        next.Efactor,
		Interval:       next.Interval,
		Repetitions:    next.Repetitions,
		LastReviewedAt: pgtype.Timestamptz{Time: now, Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: next.NextReviewAt, Valid: true},
	})
	if err != nil {
		log.Printf("[WARN] Review: failed to update UserCard: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update UserCard", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Review updated"})
}

func (h *Handler) GetReviewsSince(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
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

	targetDeckID := queueDeckID(c)

	if targetDeckID > 0 {
		if _, ok := h.requireReadableDeck(c, targetDeckID, intUserID); !ok {
			return
		}
	}

	queued, err := h.Reviews.Queue(c.Request.Context(), reviews.Query{
		Caller: intUserID,
		DeckID: targetDeckID,
		Since:  sinceTime,
	})
	if err != nil {
		respondQueueError(c, err)
		return
	}

	c.JSON(http.StatusOK, queued)
}
