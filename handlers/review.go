package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/decks"
	"github.com/Fankhauserli/voabkr-backend/reviews"
	"github.com/Fankhauserli/voabkr-backend/scheduling"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
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

	err := h.Grades.Enroll(c.Request.Context(), intUserID, int64(req.CardId))
	switch {
	case errors.Is(err, reviews.ErrNoAccess), errors.Is(err, decks.ErrNoSuchDeck):
		c.JSON(http.StatusForbidden, gin.H{"error": "Access to this private deck is restricted"})
	case err != nil:
		log.Printf("[WARN] Review: failed to create Review: %v", err)
		respondWithError(c, http.StatusBadRequest, "Failed to create Review", err)
	default:
		c.JSON(http.StatusCreated, gin.H{"message": "Review created"})
	}
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

	outcomes := h.Grades.ApplyBatch(c.Request.Context(), caller, req.Grades)
	results := make([]*types.GradeResult, 0, len(outcomes))
	for _, outcome := range outcomes {
		results = append(results, &types.GradeResult{
			CardId:   outcome.CardID,
			Accepted: outcome.Outcome.Accepted(),
			Reason:   gradeReason(outcome.Outcome),
		})
	}

	c.JSON(http.StatusOK, &types.GradeBatchResponse{Results: results})
}

// gradeReason is the wire's word for an outcome. These strings are frozen: a
// device tells a replay from a refusal by them.
func gradeReason(outcome reviews.Outcome) string {
	switch outcome {
	case reviews.Replay:
		return "already recorded; the later Grade stands"
	case reviews.NoSchedule:
		return "no Review schedule"
	case reviews.InvalidGrade:
		return "invalid Grade"
	case reviews.InvalidInterval:
		return "invalid Interval"
	case reviews.InvalidEfactor:
		return "invalid Efactor"
	case reviews.InvalidRepetitions:
		return "invalid repetitions"
	case reviews.InvalidAnsweredAt:
		return "invalid answeredAt"
	case reviews.InvalidNextReviewAt:
		return "invalid nextReviewAt"
	case reviews.UnknownCard:
		return "unknown Card"
	case reviews.NoAccess:
		return "no access to this Deck"
	case reviews.StoreFailed:
		return "could not store the Grade"
	default:
		return ""
	}
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

	// The Card's Deck decides who may study it: the writer reads the Card and
	// asks the same gate every Grade write asks before any Review is touched.
	err = h.Grades.ApplyLegacy(c.Request.Context(), intUserID, intId, grade)
	switch {
	case errors.Is(err, reviews.ErrUnknownCard):
		respondWithError(c, http.StatusNotFound, "Card not found", err)
		return
	case errors.Is(err, decks.ErrNoSuchDeck), errors.Is(err, reviews.ErrNoAccess):
		c.JSON(http.StatusForbidden, gin.H{"error": "Access to this private deck is restricted"})
		return
	case errors.Is(err, reviews.ErrReadReview), errors.Is(err, reviews.ErrCreateReview):
		respondWithError(c, http.StatusInternalServerError, "Failed to get UserCard", err)
		return
	case errors.Is(err, reviews.ErrStoreReview):
		respondWithError(c, http.StatusInternalServerError, "Failed to update UserCard", err)
		return
	case err != nil:
		log.Printf("[WARN] Review: failed to get the Card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card", err)
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
