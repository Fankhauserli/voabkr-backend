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
)

func (h *Handler) GetDecks(c *gin.Context) {
	cacheKey := "decks:all"
	if h.Cache != nil {
		var cachedList types.DeckList
		if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedList); err == nil && found {
			c.JSON(http.StatusOK, cachedList.Decks)
			return
		}
	}

	decks, err := h.DB.ListDecks(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err})
		return
	}

	returnDecks := make([]*types.Deck, 0, len(decks))

	for _, c := range decks {
		returnDecks = append(returnDecks, &types.Deck{
			Id:   uint32(c.ID),
			Name: c.Name,
			Type: string(c.Type),
		})
	}

	if h.Cache != nil {
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, &types.DeckList{Decks: returnDecks}, 5*time.Minute)
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

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
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

	cacheKey := fmt.Sprintf("deck:%d", intId)
	if h.Cache != nil {
		var cachedDeck types.Deck
		if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedDeck); err == nil && found {
			c.JSON(http.StatusOK, &cachedDeck)
			return
		}
	}

	deck, err := h.DB.GetDeck(c.Request.Context(), intId)
	if err != nil {
		log.Printf("[WARN] Deck: Failed to get Deck: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to get Deck", err)
		return
	}

	resp := &types.Deck{
		Id:   uint32(deck.ID),
		Name: deck.Name,
		Type: string(deck.Type),
	}

	if h.Cache != nil {
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, resp, 5*time.Minute)
	}

	c.JSON(http.StatusOK, resp)
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

	if h.Cache != nil {
		_ = h.Cache.Delete(c.Request.Context(), fmt.Sprintf("deck:%d", intId))
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
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

	if h.Cache != nil {
		_ = h.Cache.Delete(c.Request.Context(), fmt.Sprintf("deck:%d", intId))
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
		_ = h.Cache.DeletePrefix(c.Request.Context(), "cards:")
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Deck deleted"})
}
