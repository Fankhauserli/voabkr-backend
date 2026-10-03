package handlers

import (
	"log"
	"net/http"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func (h *Handler) GetSettings(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, ok := userID.(int64)

	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	settings, err := h.DB.GetUserSettings(c.Request.Context(), intUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve settings"})
		return
	}

	response := types.SettingsResponse{
		CardsPerDay: uint(settings.CardsPerDay),
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handler) UpdateSettings(c *gin.Context) {

	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	intUserID, ok := userID.(int64)

	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req types.SettingsResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Settings: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	err := h.DB.UpdateUserSettings(c.Request.Context(), db.UpdateUserSettingsParams{
		UserID:      intUserID,
		CardsPerDay: int32(req.CardsPerDay),
	})
	if err != nil {
		log.Printf("[WARN] Settings: failed to update settings: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update settings", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Settings updated"})
}
