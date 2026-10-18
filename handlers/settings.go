package handlers

import (
	"log"
	"net/http"

	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
)

func (h *Handler) GetSettings(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}

	settings, err := h.Account.Settings(c.Request.Context(), intUserID)
	if err != nil {
		log.Printf("[WARN] Settings: failed to retrieve or initialize settings for user %d: %v", intUserID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve settings"})
		return
	}

	c.JSON(http.StatusOK, settings)
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	intUserID, ok := requireCaller(c)
	if !ok {
		return
	}

	var req types.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Settings: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	updated, err := h.Account.UpdateSettings(c.Request.Context(), intUserID, &req)
	if err != nil {
		log.Printf("[WARN] Settings: failed to update settings: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update settings", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "Settings updated",
		"settings": updated,
	})
}
