package handlers

import (
	"errors"
	"net/http"

	"github.com/Fankhauserli/voabkr-backend/account"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
)

func (h *Handler) GetUserProfile(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}

	profile, err := h.Account.Profile(c.Request.Context(), intUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user profile"})
		return
	}

	c.JSON(http.StatusOK, profile)
}

func (h *Handler) UpdateUserProfile(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}

	var req types.UserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	err := h.Account.UpdateProfile(c.Request.Context(), intUserID, req.Name, req.Email)
	switch {
	case errors.Is(err, account.ErrEmailTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "User with this email already exists"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user profile"})
	default:
		c.JSON(http.StatusOK, gin.H{"message": "User profile updated successfully"})
	}
}

func (h *Handler) DeleteAccount(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}

	// Completely erase user account and records (GDPR Article 17 / Swiss revDSG right to erasure)
	if err := h.Account.Erase(c.Request.Context(), intUserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete account"})
		return
	}

	// The erased learner's Decks went with them, and so did the Cards of those.
	h.Decks.InvalidateEveryDeck(c.Request.Context())

	// The account is gone; the cookie it was signed in with is ended by the same
	// policy every sign-out uses.
	_ = middleware.SignOut(c)

	c.JSON(http.StatusOK, gin.H{"message": "Account successfully erased"})
}
