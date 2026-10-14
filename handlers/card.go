package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) GetCards(c *gin.Context) {
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

	caller, _ := callerID(c)

	// A Deck-filtered read is settled by the Deck first: the Cards are its, and
	// serving them is a decision for the caller's own access to it.
	if targetDeckID > 0 {
		if _, ok := h.requireReadableDeck(c, targetDeckID, caller); !ok {
			return
		}
	}

	// limit and offset are opt-in: a request that sends neither gets every Card,
	// as it did before paging existed.
	paged := c.Query("limit") != "" || c.Query("offset") != ""

	limit := int32(100)
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 32); err == nil && l > 0 {
			if l > 500 {
				limit = 500
			} else {
				limit = int32(l)
			}
		}
	}

	offset := int32(0)
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.ParseInt(offsetStr, 10, 32); err == nil && o >= 0 {
			offset = int32(o)
		}
	}

	// Only a Deck's own Cards are shared data worth an entry every caller reads.
	// The caller's own Review schedules are not: they are attached on the way out.
	var cacheKey string
	if targetDeckID > 0 {
		if paged {
			cacheKey = fmt.Sprintf("cards:deck:%d:lim%d:off%d", targetDeckID, limit, offset)
		} else {
			cacheKey = fmt.Sprintf("cards:deck:%d", targetDeckID)
		}

		if h.Cache != nil {
			var cachedList types.CardList
			if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedList); err == nil && found {
				cards, err := h.withSchedules(c.Request.Context(), caller, cachedList.Cards)
				if err != nil {
					respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedules", err)
					return
				}
				c.JSON(http.StatusOK, cards)
				return
			}
		}
	}

	if h.DB == nil {
		c.JSON(http.StatusOK, []*types.Card{})
		return
	}

	var cards []db.Card
	var err error
	switch {
	case targetDeckID > 0 && paged:
		cards, err = h.DB.ListCardsByDeck(c.Request.Context(), db.ListCardsByDeckParams{
			DeckID: targetDeckID,
			Limit:  limit,
			Offset: offset,
		})
	case targetDeckID > 0:
		cards, err = h.DB.ListCardsByDeckAll(c.Request.Context(), targetDeckID)
	case paged:
		cards, err = h.DB.ListAccessibleCardsPaged(c.Request.Context(), db.ListAccessibleCardsPagedParams{
			UserID:     callerParam(caller),
			PageLimit:  limit,
			PageOffset: offset,
		})
	default:
		cards, err = h.DB.ListAccessibleCards(c.Request.Context(), callerParam(caller))
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	returnCards, err := h.cardsWithSchedules(c.Request.Context(), caller, cards)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedules", err)
		return
	}

	if h.Cache != nil && cacheKey != "" {
		// The entry holds the Deck's Cards, which are the same for every caller,
		// and never the Review schedule of the one asking (ADR-0001).
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, &types.CardList{Cards: cardsWithoutSchedules(cards)}, 5*time.Minute)
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
		DeckID:          int64(req.DeckId),
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

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "cards:")
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

	// The Card belongs to a Deck, and the Deck decides who may read it. The row is
	// therefore read every time and the caller settled before anything answers.
	card, err := h.DB.GetCard(c.Request.Context(), intId)
	if err != nil {
		log.Printf("[WARN] Card: Failed to get card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card", err)
		return
	}

	caller, _ := callerID(c)
	if _, ok := h.requireReadableDeck(c, card.DeckID, caller); !ok {
		return
	}

	response, err := h.cardResponseFor(c.Request.Context(), caller, card)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedule", err)
		return
	}

	c.JSON(http.StatusOK, response)
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
		DeckID:          int64(req.DeckId),
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

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "cards:")
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

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "cards:")
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Card deleted"})
}

// cardResponseFor renders one Card with the caller's own Review schedule of it.
func (h *Handler) cardResponseFor(ctx context.Context, caller int64, card db.Card) (*types.Card, error) {
	schedules, err := h.reviewSchedules(ctx, caller, []int64{card.ID})
	if err != nil {
		return nil, err
	}
	return cardResponse(card, schedules[card.ID]), nil
}

// cardsWithSchedules renders Cards with the caller's own Review schedule of each,
// so a device can compute forward from what it is given. A Card the caller has
// not studied carries no schedule.
//
// A failure to read the schedules is returned rather than swallowed: a Card
// served without the schedule it has would tell the device it was never studied.
func (h *Handler) cardsWithSchedules(ctx context.Context, caller int64, cards []db.Card) ([]*types.Card, error) {
	return h.withSchedules(ctx, caller, cardsWithoutSchedules(cards))
}

// cardsWithoutSchedules renders Cards as the shared data they are: a Deck's
// Cards, with no caller's Review schedule among them, for an entry every caller
// reads.
func cardsWithoutSchedules(cards []db.Card) []*types.Card {
	rendered := make([]*types.Card, 0, len(cards))
	for _, card := range cards {
		rendered = append(rendered, cardResponse(card, nil))
	}
	return rendered
}

// withSchedules gives Cards that are already rendered the caller's own Review
// schedule of each — what comes back out of an entry they share. The Cards are
// the caller's to fill in: a cache hands back values it unmarshalled for this
// request, never the values it holds.
func (h *Handler) withSchedules(ctx context.Context, caller int64, cards []*types.Card) ([]*types.Card, error) {
	cardIDs := make([]int64, 0, len(cards))
	for _, card := range cards {
		cardIDs = append(cardIDs, int64(card.Id))
	}

	schedules, err := h.reviewSchedules(ctx, caller, cardIDs)
	if err != nil {
		return nil, err
	}

	for _, card := range cards {
		if review, ok := schedules[int64(card.Id)]; ok {
			card.ReviewSchedule = reviewSchedule(review)
		}
	}
	return cards, nil
}

// reviewSchedules reads the caller's Reviews of the given Cards, keyed by Card
// id. An anonymous caller has none, and the read is skipped.
func (h *Handler) reviewSchedules(ctx context.Context, caller int64, cardIDs []int64) (map[int64]*db.UserCard, error) {
	if caller == access.Anonymous || len(cardIDs) == 0 {
		return nil, nil
	}

	reviews, err := h.DB.ListUserCardSchedules(ctx, db.ListUserCardSchedulesParams{
		UserID:  caller,
		CardIds: cardIDs,
	})
	if err != nil {
		log.Printf("[WARN] Card: failed to read the caller's Review schedules: %v", err)
		return nil, err
	}

	byCard := make(map[int64]*db.UserCard, len(reviews))
	for i := range reviews {
		byCard[reviews[i].CardID] = &reviews[i]
	}
	return byCard, nil
}

// cardResponse is one Card as callers see it, its Review schedule included once
// the caller has one.
func cardResponse(card db.Card, review *db.UserCard) *types.Card {
	response := &types.Card{
		Id:          uint32(card.ID),
		DeckId:      uint32(card.DeckID),
		KoreanWord:  card.KoreanWord,
		EnglishWord: card.EnglishWord,
		Context:     card.Context.String,
		Example:     card.ExampleSentence.String,
	}

	if review != nil {
		response.ReviewSchedule = reviewSchedule(review)
	}

	return response
}

// reviewSchedule is a stored Review as the wire carries it.
func reviewSchedule(review *db.UserCard) *types.ReviewSchedule {
	return &types.ReviewSchedule{
		Interval:     review.Interval,
		Efactor:      review.Efactor,
		Repetitions:  review.Repetitions,
		NextReviewAt: formatTime(review.NextReviewAt),
	}
}

// formatTime renders a stored moment the way the wire carries it, and a NULL as
// empty.
func formatTime(at pgtype.Timestamptz) string {
	if !at.Valid {
		return ""
	}
	return at.Time.UTC().Format(time.RFC3339)
}
