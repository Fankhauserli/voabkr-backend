package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strconv"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func generateShareToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "sec_" + hex.EncodeToString(b)
}

func (h *Handler) GetDecks(c *gin.Context) {
	caller, signedIn := middleware.Caller(c)

	// A session naming a learner this instance cannot place is told to sign in
	// again rather than served a guest's decks.
	if middleware.UnresolvedSession(c) {
		middleware.RefuseUnauthorized(c)
		return
	}

	// 1. Unauthenticated (Guests) -> return only public decks
	if !signedIn {
		decks, err := h.Decks.Public(c.Request.Context())
		if err != nil {
			respondWithError(c, http.StatusInternalServerError, "Failed to retrieve public decks", err)
			return
		}

		c.JSON(http.StatusOK, decks)
		return
	}

	// 2. Authenticated user -> return public decks + owned decks + shared decks
	decks, err := h.Decks.ForCaller(c.Request.Context(), caller)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to retrieve decks", err)
		return
	}

	c.JSON(http.StatusOK, decks)
}

func (h *Handler) CreateDeck(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}
	var req types.DeckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Deck: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	shareToken := generateShareToken()

	newDeck, err := h.DB.CreateDeck(c.Request.Context(), db.CreateDeckParams{
		Name:       req.Name,
		Type:       db.DeckType(req.Type),
		OwnerID:    pgtype.Int8{Int64: intUserID, Valid: true},
		IsPublic:   req.IsPublic,
		ShareToken: pgtype.Text{String: shareToken, Valid: true},
	})

	if err != nil {
		log.Printf("[WARN] Deck: failed to create Deck: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create Deck", err)
		return
	}

	h.Decks.Invalidate(c.Request.Context(), newDeck.ID)

	c.JSON(http.StatusCreated, gin.H{
		"message": "Deck created",
		"data": gin.H{
			"id":         newDeck.ID,
			"shareToken": shareToken,
		},
	})
}

func (h *Handler) GetDeckByID(c *gin.Context) {
	id, isValid := c.Params.Get("id")
	if !isValid {
		respondWithError(c, http.StatusBadRequest, "Missing deck id parameter", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid deck id parameter", err)
		return
	}

	// The Deck row decides who may read it, so it is read every time: a body
	// carrying the Access role of whoever warmed it must not answer for another
	// caller.
	caller, _ := middleware.Caller(c)
	standing, ok := h.requireReadableDeck(c, intId, caller)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, standing.Body())
}

func (h *Handler) UpdateDeck(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}
	id, isValid := c.Params.Get("id")
	if !isValid {
		respondWithError(c, http.StatusBadRequest, "Missing deck id parameter", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid deck id parameter", err)
		return
	}

	standing, ok := h.requireDeckEdit(c, intId, intUserID, "You do not have permission to edit this deck")
	if !ok {
		return
	}

	var req types.DeckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Only owner can change public/private visibility — a decision about the
	// Deck, read from the row, where the body is for responses.
	isPublicToSet := standing.Deck.IsPublic
	if access.Owns(standing.Deck, intUserID) {
		isPublicToSet = req.IsPublic
	}

	err = h.DB.UpdateDeck(c.Request.Context(), db.UpdateDeckParams{
		ID:       intId,
		Name:     req.Name,
		Type:     db.DeckType(req.Type),
		IsPublic: isPublicToSet,
	})

	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to update deck", err)
		return
	}

	h.Decks.Invalidate(c.Request.Context(), intId)

	c.JSON(http.StatusOK, gin.H{"message": "Deck updated"})
}

func (h *Handler) DeleteDeck(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}
	id, isValid := c.Params.Get("id")
	if !isValid {
		respondWithError(c, http.StatusBadRequest, "Missing deck id parameter", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid deck id parameter", err)
		return
	}

	if _, ok := h.requireDeckOwner(c, intId, intUserID, "Only the deck owner can delete this deck"); !ok {
		return
	}

	// Cleared while the row is still there: the share-link entry is keyed by the
	// token the row carries.
	h.Decks.Invalidate(c.Request.Context(), intId)

	err = h.DB.DeleteDeck(c.Request.Context(), intId)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to delete deck", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deck deleted"})
}

func (h *Handler) ShareDeck(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}
	id, isValid := c.Params.Get("id")
	if !isValid {
		respondWithError(c, http.StatusBadRequest, "Missing deck id parameter", nil)
		return
	}

	intId, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid deck id parameter", err)
		return
	}

	standing, ok := h.requireDeckOwner(c, intId, intUserID, "Only the deck owner can share this deck")
	if !ok {
		return
	}
	deck := standing.Body()

	// Ensure share_token exists
	shareToken := deck.ShareToken
	if shareToken == "" {
		shareToken = generateShareToken()
		_ = h.DB.SetDeckShareToken(c.Request.Context(), db.SetDeckShareTokenParams{
			ID:         int64(deck.Id),
			ShareToken: pgtype.Text{String: shareToken, Valid: true},
		})
	}

	var req types.ShareDeckRequest
	_ = c.ShouldBindJSON(&req)

	if req.Email != "" {
		permission := req.Permission
		if permission != "edit" {
			permission = "view"
		}

		targetUser, userErr := h.DB.GetUserByEmail(c.Request.Context(), req.Email)
		var targetUserID pgtype.Int8
		if userErr == nil && targetUser.ID > 0 {
			targetUserID = pgtype.Int8{Int64: targetUser.ID, Valid: true}
		}

		_, shareErr := h.DB.CreateDeckShare(c.Request.Context(), db.CreateDeckShareParams{
			DeckID:       int64(deck.Id),
			UserID:       targetUserID,
			InvitedEmail: pgtype.Text{String: req.Email, Valid: true},
			Permission:   permission,
		})
		if shareErr != nil {
			respondWithError(c, http.StatusInternalServerError, "Failed to share deck with invited email", shareErr)
			return
		}
	}

	h.Decks.Invalidate(c.Request.Context(), intId)

	c.JSON(http.StatusOK, gin.H{
		"shareToken": shareToken,
		"message":    "Deck shared successfully",
	})
}

func (h *Handler) GetSharedDeckByToken(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing share token"})
		return
	}

	shared, err := h.Decks.Shared(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shared deck not found or link has expired"})
		return
	}

	// An entry holds the Deck alone; a row read also names its owner. The two
	// shapes are a difference the wire contract keeps for now.
	if shared.FromEntry {
		c.JSON(http.StatusOK, shared.Deck)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         shared.Deck.Id,
		"name":       shared.Deck.Name,
		"type":       shared.Deck.Type,
		"isPublic":   shared.Deck.IsPublic,
		"ownerId":    shared.Deck.OwnerId,
		"shareToken": shared.Deck.ShareToken,
		"ownerName":  shared.OwnerName,
	})
}

func (h *Handler) JoinSharedDeck(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing share token"})
		return
	}

	shared, err := h.Decks.Shared(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shared deck not found"})
		return
	}

	if shared.Deck.OwnerId == uint32(intUserID) {
		c.JSON(http.StatusOK, gin.H{
			"message": "You are already the owner of this deck",
			"data":    gin.H{"deckId": shared.Deck.Id},
		})
		return
	}

	_, err = h.DB.CreateDeckShare(c.Request.Context(), db.CreateDeckShareParams{
		DeckID:     int64(shared.Deck.Id),
		UserID:     pgtype.Int8{Int64: intUserID, Valid: true},
		Permission: "view",
	})
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to join shared deck", err)
		return
	}

	h.Decks.Invalidate(c.Request.Context(), int64(shared.Deck.Id))

	c.JSON(http.StatusOK, gin.H{
		"message": "Deck added to your study library",
		"data":    gin.H{"deckId": shared.Deck.Id},
	})
}

func (h *Handler) RevokeDeckShare(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}
	deckIDStr := c.Param("id")
	targetUserIDStr := c.Param("user_id")

	deckID, _ := strconv.ParseInt(deckIDStr, 10, 64)
	targetUserID, _ := strconv.ParseInt(targetUserIDStr, 10, 64)

	if _, ok := h.requireDeckOwner(c, deckID, intUserID, "Only the deck owner can revoke shared access"); !ok {
		return
	}

	err := h.DB.DeleteDeckShare(c.Request.Context(), db.DeleteDeckShareParams{
		DeckID: deckID,
		UserID: pgtype.Int8{Int64: targetUserID, Valid: true},
	})
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to revoke share access", err)
		return
	}

	h.Decks.Invalidate(c.Request.Context(), deckID)

	c.JSON(http.StatusOK, gin.H{"message": "Access revoked successfully"})
}
