package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func (h *Handler) GetCards(c *gin.Context) {
	targetDeckID, page := cardQuery(c)

	caller, _ := middleware.Caller(c)

	// A Deck-filtered read is settled by the Deck first: the Cards are its, and
	// serving them is a decision for the caller's own access to it.
	if targetDeckID > 0 {
		if _, ok := h.requireReadableDeck(c, targetDeckID, caller); !ok {
			return
		}
	}

	// The Cards arrive with the caller's own Review schedules attached: they are
	// read for the caller asking rather than assembled here.
	var (
		bodies []*types.Card
		err    error
	)
	if targetDeckID > 0 {
		bodies, err = h.Cards.ByDeck(c.Request.Context(), caller, targetDeckID, page)
	} else {
		bodies, err = h.Cards.ForCaller(c.Request.Context(), caller, page)
	}
	switch {
	case errors.Is(err, cards.ErrSchedules):
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedules", err)
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	c.JSON(http.StatusOK, bodies)
}

// cardQuery reads the Deck a listing is filtered by, and the window it asks for.
// limit and offset are opt-in: a request that sends neither gets every Card, as
// it did before paging existed.
func cardQuery(c *gin.Context) (deckID int64, page cards.Page) {
	deckIDStr := c.Query("deck_id")
	if deckIDStr == "" {
		deckIDStr = c.Query("deckId")
	}
	if deckIDStr == "" {
		deckIDStr = c.Query("deck")
	}
	if deckIDStr != "" {
		deckID, _ = strconv.ParseInt(deckIDStr, 10, 64)
	}

	page.Paged = c.Query("limit") != "" || c.Query("offset") != ""
	page.Limit = 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 32); err == nil && l > 0 {
			if l > 500 {
				page.Limit = 500
			} else {
				page.Limit = int32(l)
			}
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.ParseInt(offsetStr, 10, 32); err == nil && o >= 0 {
			page.Offset = int32(o)
		}
	}

	return deckID, page
}

func (h *Handler) CreateCards(c *gin.Context) {
	var req types.CardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Card: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// The Card is created in the Deck the request names, so it is that Deck's
	// standing that decides whether the write happens at all.
	caller, _ := middleware.Caller(c)
	if _, ok := h.requireDeckEdit(c, int64(req.DeckId), caller, cardEditRefusal); !ok {
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

	h.Cards.Invalidate(c.Request.Context(), int64(req.DeckId))
	// The Deck body carries its Card total, so the entries holding it go too.
	h.Decks.Invalidate(c.Request.Context(), int64(req.DeckId))

	c.JSON(http.StatusCreated, gin.H{"message": "Card created"})
}

func (h *Handler) GetCardByID(c *gin.Context) {
	id, ok := cardParam(c)
	if !ok {
		return
	}

	// The Card belongs to a Deck, and the Deck decides who may read it. The Card
	// is therefore read every time — for the caller asking, so their own Review
	// schedule comes with it — and the caller settled before anything answers.
	caller, _ := middleware.Caller(c)
	card, err := h.Cards.ByID(c.Request.Context(), caller, id)
	switch {
	case errors.Is(err, cards.ErrNoSuchCard):
		respondWithError(c, http.StatusNotFound, "Card not found", err)
		return
	case errors.Is(err, cards.ErrSchedules):
		respondWithError(c, http.StatusInternalServerError, "Failed to read the Review schedule", err)
		return
	case err != nil:
		respondWithError(c, http.StatusInternalServerError, "Failed to get card", err)
		return
	}

	if _, ok := h.requireReadableDeck(c, card.DeckID, caller); !ok {
		return
	}

	c.JSON(http.StatusOK, card.Body)
}

// cardParam reads the Card id a request names. It answers the request itself:
// 500 in the shape this route has always refused a bad id in.
func cardParam(c *gin.Context) (int64, bool) {
	id, isValid := c.Params.Get("id")
	if !isValid {
		log.Printf("[WARN] Card: Failed to get card id")
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id", nil)
		return 0, false
	}

	intId, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		log.Printf("[WARN] Card: Failed to get card id as int: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card id as int", err)
		return 0, false
	}

	return intId, true
}

func (h *Handler) UpdateCard(c *gin.Context) {
	var req types.CardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Card: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	card, ok := h.requireWritableCard(c)
	if !ok {
		return
	}

	// A request naming another Deck asks for a move, which needs the edit on the
	// Deck the Card would land in as well.
	if claimed := int64(req.DeckId); claimed != card.DeckID {
		caller, _ := middleware.Caller(c)
		if _, ok := h.requireDeckEdit(c, claimed, caller, cardEditRefusal); !ok {
			return
		}
	}

	err := h.DB.UpdateCard(c.Request.Context(), db.UpdateCardParams{
		ID:              card.ID,
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

	// The Card it was in, and — when the request moved it — the Deck it landed in.
	// The Deck bodies carry Card totals, so the entries holding them go too.
	h.Cards.Invalidate(c.Request.Context(), card.DeckID)
	h.Decks.Invalidate(c.Request.Context(), card.DeckID)
	if int64(req.DeckId) != card.DeckID {
		h.Cards.Invalidate(c.Request.Context(), int64(req.DeckId))
		h.Decks.Invalidate(c.Request.Context(), int64(req.DeckId))
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Card updated"})
}

func (h *Handler) DeleteCard(c *gin.Context) {
	card, ok := h.requireWritableCard(c)
	if !ok {
		return
	}

	err := h.DB.DeleteCard(c.Request.Context(), card.ID)
	if err != nil {
		log.Printf("[WARN] Card: failed to delete Card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to delete Card", err)
		return
	}

	h.Cards.Invalidate(c.Request.Context(), card.DeckID)
	// The Deck body carries its Card total, so the entries holding it go too.
	h.Decks.Invalidate(c.Request.Context(), card.DeckID)

	c.JSON(http.StatusAccepted, gin.H{"message": "Card deleted"})
}

// requireWritableCard loads the Card named by the request's :id and settles
// whether the caller may write it. The Deck the Card is stored in is the one
// that decides, never one the request claims, so the row is read first. It
// answers the request itself: 404 when there is no such Card, 403 when there is
// but the caller may not write the Deck holding it.
//
// Both an edit and a delete go through here, so the rule is applied in one place
// rather than re-spelled per handler.
func (h *Handler) requireWritableCard(c *gin.Context) (db.Card, bool) {
	id, ok := cardParam(c)
	if !ok {
		return db.Card{}, false
	}

	card, err := h.DB.GetCard(c.Request.Context(), id)
	if err != nil {
		log.Printf("[WARN] Card: failed to get card: %v", err)
		respondWithError(c, http.StatusNotFound, "Card not found", err)
		return db.Card{}, false
	}

	caller, _ := middleware.Caller(c)
	if _, ok := h.requireDeckEdit(c, card.DeckID, caller, cardEditRefusal); !ok {
		return db.Card{}, false
	}

	return card, true
}

// cardEditRefusal is how a caller is told their standing on a Deck does not
// reach its Cards — the words a Card write is refused in.
const cardEditRefusal = "You do not have permission to edit this deck's Cards"
