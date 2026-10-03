package handlers

import (
	"log"
	"net/http"
	"strconv"

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

	intUserID, err := strconv.ParseInt(userID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	settings, err := h.DB.GetUserSettings(c.Request.Context(), intUserID)
	if err != nil {
		settings, err = h.DB.CreateUserSettings(c.Request.Context(), intUserID)
		if err != nil {
			log.Printf("[WARN] Settings: failed to retrieve or initialize settings for user %d: %v", intUserID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve settings"})
			return
		}
	}

	studyDir := settings.StudyDirection
	if studyDir == "" {
		studyDir = "koreanToEnglish"
	}

	response := types.SettingsResponse{
		CardsPerDay:       uint(settings.CardsPerDay),
		StudyDirection:    studyDir,
		ScratchPadEnabled: settings.ScratchPadEnabled,
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

	intUserID, err := strconv.ParseInt(userID.(string), 10, 64)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req types.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Settings: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	settings, err := h.DB.GetUserSettings(c.Request.Context(), intUserID)
	if err != nil {
		settings, err = h.DB.CreateUserSettings(c.Request.Context(), intUserID)
		if err != nil {
			log.Printf("[WARN] Settings: failed to initialize settings for user %d: %v", intUserID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings"})
			return
		}
	}

	cardsPerDay := settings.CardsPerDay
	if req.CardsPerDay != nil && *req.CardsPerDay > 0 {
		cardsPerDay = int32(*req.CardsPerDay)
	}

	studyDirection := settings.StudyDirection
	if studyDirection == "" {
		studyDirection = "koreanToEnglish"
	}
	if req.StudyDirection != nil && *req.StudyDirection != "" {
		studyDirection = *req.StudyDirection
	}

	scratchPadEnabled := settings.ScratchPadEnabled
	if req.ScratchPadEnabled != nil {
		scratchPadEnabled = *req.ScratchPadEnabled
	}

	err = h.DB.UpdateUserSettings(c.Request.Context(), db.UpdateUserSettingsParams{
		UserID:            intUserID,
		CardsPerDay:       cardsPerDay,
		StudyDirection:    studyDirection,
		ScratchPadEnabled: scratchPadEnabled,
	})
	if err != nil {
		log.Printf("[WARN] Settings: failed to update settings: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to update settings", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Settings updated",
		"settings": types.SettingsResponse{
			CardsPerDay:       uint(cardsPerDay),
			StudyDirection:    studyDirection,
			ScratchPadEnabled: scratchPadEnabled,
		},
	})
}
