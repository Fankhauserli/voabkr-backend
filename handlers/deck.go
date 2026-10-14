package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func generateShareToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "sec_" + hex.EncodeToString(b)
}

func (h *Handler) GetDecks(c *gin.Context) {
	session := sessions.Default(c)
	sessionUserID := session.Get("user_id")

	// 1. Unauthenticated (Guests) -> return only public decks
	if sessionUserID == nil {
		// decks:public holds public Decks alone, so it reads the same for every
		// sessionless caller. An entry that could hold a private Deck must not
		// answer here: which Decks a caller may list is a decision about the
		// caller, and per ADR-0001 a cached entry holds data, never decisions.
		cacheKey := "decks:public"
		if h.Cache != nil {
			var cachedList types.DeckList
			if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedList); err == nil && found {
				c.JSON(http.StatusOK, cachedList.Decks)
				return
			}
		}

		decks, err := h.DB.ListPublicDecks(c.Request.Context())
		if err != nil {
			respondWithError(c, http.StatusInternalServerError, "Failed to retrieve public decks", err)
			return
		}

		returnDecks := make([]*types.Deck, 0, len(decks))
		for _, d := range decks {
			var ownerID uint32
			if d.OwnerID.Valid {
				ownerID = uint32(d.OwnerID.Int64)
			}
			returnDecks = append(returnDecks, &types.Deck{
				Id:             uint32(d.ID),
				Name:           d.Name,
				Type:           string(d.Type),
				OwnerId:        ownerID,
				IsPublic:       d.IsPublic,
				ShareToken:     d.ShareToken.String,
				UserAccessRole: "public",
			})
		}

		if h.Cache != nil {
			_ = h.Cache.SetProto(c.Request.Context(), cacheKey, &types.DeckList{Decks: returnDecks}, 5*time.Minute)
		}

		c.JSON(http.StatusOK, returnDecks)
		return
	}

	// 2. Authenticated user -> return public decks + owned decks + shared decks
	intUserID, err := strconv.ParseInt(sessionUserID.(string), 10, 64)
	if err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid session user ID", err)
		return
	}

	cacheKey := fmt.Sprintf("decks:user:%d", intUserID)
	if h.Cache != nil {
		var cachedList types.DeckList
		if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedList); err == nil && found {
			c.JSON(http.StatusOK, cachedList.Decks)
			return
		}
	}

	userDecks, err := h.DB.ListDecksForUser(c.Request.Context(), pgtype.Int8{Int64: intUserID, Valid: true})
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to retrieve decks", err)
		return
	}

	returnDecks := make([]*types.Deck, 0, len(userDecks))
	for _, d := range userDecks {
		var ownerID uint32
		if d.OwnerID.Valid {
			ownerID = uint32(d.OwnerID.Int64)
		}
		returnDecks = append(returnDecks, &types.Deck{
			Id:             uint32(d.ID),
			Name:           d.Name,
			Type:           string(d.Type),
			OwnerId:        ownerID,
			IsPublic:       d.IsPublic,
			ShareToken:     d.ShareToken.String,
			UserAccessRole: d.UserAccessRole,
		})
	}

	if h.Cache != nil {
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, &types.DeckList{Decks: returnDecks}, 5*time.Minute)
	}

	c.JSON(http.StatusOK, returnDecks)
}

func (h *Handler) CreateDeck(c *gin.Context) {
	session := sessions.Default(c)
	sessionUserID := session.Get("user_id")
	if sessionUserID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(sessionUserID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
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

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
	}

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

	// The Deck row decides who may read it, so it is read every time: a cached
	// body would carry the Access role of whichever caller warmed it.
	caller, _ := callerID(c)
	readable, ok := h.requireReadableDeck(c, intId, caller)
	if !ok {
		return
	}
	deck := readable.deck

	var ownerID uint32
	if deck.OwnerID.Valid {
		ownerID = uint32(deck.OwnerID.Int64)
	}

	c.JSON(http.StatusOK, &types.Deck{
		Id:             uint32(deck.ID),
		Name:           deck.Name,
		Type:           string(deck.Type),
		OwnerId:        ownerID,
		IsPublic:       deck.IsPublic,
		ShareToken:     deck.ShareToken.String,
		UserAccessRole: readable.role,
	})
}

func (h *Handler) UpdateDeck(c *gin.Context) {
	session := sessions.Default(c)
	sessionUserID := session.Get("user_id")
	if sessionUserID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(sessionUserID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
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

	// Only owner can change public/private visibility
	isPublicToSet := standing.deck.IsPublic
	if access.Owns(standing.deck, intUserID) {
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

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deck updated"})
}

func (h *Handler) DeleteDeck(c *gin.Context) {
	session := sessions.Default(c)
	sessionUserID := session.Get("user_id")
	if sessionUserID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(sessionUserID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
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

	err = h.DB.DeleteDeck(c.Request.Context(), intId)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to delete deck", err)
		return
	}

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
		_ = h.Cache.DeletePrefix(c.Request.Context(), "cards:")
	}

	c.JSON(http.StatusOK, gin.H{"message": "Deck deleted"})
}

func (h *Handler) ShareDeck(c *gin.Context) {
	session := sessions.Default(c)
	sessionUserID := session.Get("user_id")
	if sessionUserID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(sessionUserID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
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
	deck := standing.deck

	// Ensure share_token exists
	shareToken := deck.ShareToken.String
	if !deck.ShareToken.Valid || shareToken == "" {
		shareToken = generateShareToken()
		_ = h.DB.SetDeckShareToken(c.Request.Context(), db.SetDeckShareTokenParams{
			ID:         deck.ID,
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
			DeckID:       deck.ID,
			UserID:       targetUserID,
			InvitedEmail: pgtype.Text{String: req.Email, Valid: true},
			Permission:   permission,
		})
		if shareErr != nil {
			respondWithError(c, http.StatusInternalServerError, "Failed to share deck with invited email", shareErr)
			return
		}
	}

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
	}

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

	cacheKey := fmt.Sprintf("deck:shared:%s", token)
	if h.Cache != nil {
		var cachedDeck types.Deck
		if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedDeck); err == nil && found {
			c.JSON(http.StatusOK, &cachedDeck)
			return
		}
	}

	if h.DB == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shared deck not found or link has expired"})
		return
	}

	deck, err := h.DB.GetDeckByShareToken(c.Request.Context(), pgtype.Text{String: token, Valid: true})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shared deck not found or link has expired"})
		return
	}

	var ownerName string
	var ownerID uint32
	if deck.OwnerID.Valid {
		ownerID = uint32(deck.OwnerID.Int64)
		ownerUser, err := h.DB.GetUser(c.Request.Context(), deck.OwnerID.Int64)
		if err == nil {
			ownerName = ownerUser.Name
		}
	}

	resp := &types.Deck{
		Id:         uint32(deck.ID),
		Name:       deck.Name,
		Type:       string(deck.Type),
		IsPublic:   deck.IsPublic,
		OwnerId:    ownerID,
		ShareToken: deck.ShareToken.String,
	}

	if h.Cache != nil {
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, resp, 5*time.Minute)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         deck.ID,
		"name":       deck.Name,
		"type":       deck.Type,
		"isPublic":   deck.IsPublic,
		"ownerId":    ownerID,
		"shareToken": deck.ShareToken.String,
		"ownerName":  ownerName,
	})
}


func (h *Handler) JoinSharedDeck(c *gin.Context) {
	session := sessions.Default(c)
	sessionUserID := session.Get("user_id")
	if sessionUserID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(sessionUserID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing share token"})
		return
	}

	deck, err := h.DB.GetDeckByShareToken(c.Request.Context(), pgtype.Text{String: token, Valid: true})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Shared deck not found"})
		return
	}

	if deck.OwnerID.Valid && deck.OwnerID.Int64 == intUserID {
		c.JSON(http.StatusOK, gin.H{
			"message": "You are already the owner of this deck",
			"data":    gin.H{"deckId": deck.ID},
		})
		return
	}

	_, err = h.DB.CreateDeckShare(c.Request.Context(), db.CreateDeckShareParams{
		DeckID:     deck.ID,
		UserID:     pgtype.Int8{Int64: intUserID, Valid: true},
		Permission: "view",
	})
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to join shared deck", err)
		return
	}

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Deck added to your study library",
		"data":    gin.H{"deckId": deck.ID},
	})
}

func (h *Handler) RevokeDeckShare(c *gin.Context) {
	session := sessions.Default(c)
	sessionUserID := session.Get("user_id")
	if sessionUserID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, err := strconv.ParseInt(sessionUserID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	deckIDStr := c.Param("id")
	targetUserIDStr := c.Param("user_id")

	deckID, _ := strconv.ParseInt(deckIDStr, 10, 64)
	targetUserID, _ := strconv.ParseInt(targetUserIDStr, 10, 64)

	if _, ok := h.requireDeckOwner(c, deckID, intUserID, "Only the deck owner can revoke shared access"); !ok {
		return
	}

	err = h.DB.DeleteDeckShare(c.Request.Context(), db.DeleteDeckShareParams{
		DeckID: deckID,
		UserID: pgtype.Int8{Int64: targetUserID, Valid: true},
	})
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to revoke share access", err)
		return
	}

	if h.Cache != nil {
		_ = h.Cache.DeletePrefix(c.Request.Context(), "decks:")
	}

	c.JSON(http.StatusOK, gin.H{"message": "Access revoked successfully"})
}
