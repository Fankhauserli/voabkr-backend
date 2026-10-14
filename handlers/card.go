package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

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

	var cacheKey string
	switch {
	case targetDeckID > 0 && paged:
		cacheKey = fmt.Sprintf("cards:deck:%d:lim%d:off%d", targetDeckID, limit, offset)
	case targetDeckID > 0:
		cacheKey = fmt.Sprintf("cards:deck:%d", targetDeckID)
	case paged:
		cacheKey = fmt.Sprintf("cards:all:lim%d:off%d", limit, offset)
	default:
		cacheKey = "cards:all"
	}

	if h.Cache != nil {
		var cachedList types.CardList
		if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedList); err == nil && found {
			c.JSON(http.StatusOK, cachedList.Cards)
			return
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
		cards, err = h.DB.ListCardsPaged(c.Request.Context(), db.ListCardsPagedParams{
			Limit:  limit,
			Offset: offset,
		})
	default:
		cards, err = h.DB.ListCards(c.Request.Context())
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	returnCards := make([]*types.Card, 0, len(cards))
	for _, card := range cards {
		returnCards = append(returnCards, &types.Card{
			Id:          uint32(card.ID),
			DeckId:      uint32(card.DeckID),
			KoreanWord:  card.KoreanWord,
			EnglishWord: card.EnglishWord,
			Context:     card.Context.String,
			Example:     card.ExampleSentence.String,
		})
	}

	if h.Cache != nil {
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, &types.CardList{Cards: returnCards}, 5*time.Minute)
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

	cacheKey := fmt.Sprintf("card:%d", intId)
	if h.Cache != nil {
		var cachedCard types.Card
		if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedCard); err == nil && found {
			c.JSON(http.StatusOK, &cachedCard)
			return
		}
	}

	card, err := h.DB.GetCard(c.Request.Context(), intId)

	if err != nil {
		log.Printf("[WARN] Card: Failed to get card: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get card", err)
		return
	}

	resp := &types.Card{
		Id:          uint32(card.ID),
		DeckId:      uint32(card.DeckID),
		KoreanWord:  card.KoreanWord,
		EnglishWord: card.EnglishWord,
		Context:     card.Context.String,
		Example:     card.ExampleSentence.String,
	}

	if h.Cache != nil {
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, resp, 5*time.Minute)
	}

	c.JSON(http.StatusOK, resp)
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
		_ = h.Cache.Delete(c.Request.Context(), fmt.Sprintf("card:%d", intId))
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
		_ = h.Cache.Delete(c.Request.Context(), fmt.Sprintf("card:%d", intId))
		_ = h.Cache.DeletePrefix(c.Request.Context(), "cards:")
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Card deleted"})
}
