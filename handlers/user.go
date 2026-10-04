package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func (h *Handler) GetUserProfile(c *gin.Context) {
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

	cacheKey := fmt.Sprintf("user:profile:%d", intUserID)
	if h.Cache != nil {
		var cachedUser types.UserResponse
		if found, err := h.Cache.GetProto(c.Request.Context(), cacheKey, &cachedUser); err == nil && found {
			c.JSON(http.StatusOK, &cachedUser)
			return
		}
	}

	user, err := h.DB.GetUser(c.Request.Context(), intUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve user profile"})
		return
	}

	response := types.UserResponse{
		Id:         uint32(user.ID),
		Name:       user.Name,
		Email:      user.Email,
		IsVerified: user.EmailVerified,
		IsActive:   user.IsActive,
	}

	if h.Cache != nil {
		_ = h.Cache.SetProto(c.Request.Context(), cacheKey, &response, 10*time.Minute)
	}

	c.JSON(http.StatusOK, &response)
}

func (h *Handler) UpdateUserProfile(c *gin.Context) {
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

	var req types.UserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	err = h.DB.UpdateUser(c.Request.Context(), db.UpdateUserParams{
		ID:    intUserID,
		Name:  req.Name,
		Email: req.Email,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user profile"})
		return
	}

	if h.Cache != nil {
		_ = h.Cache.Delete(c.Request.Context(), fmt.Sprintf("user:profile:%d", intUserID))
	}

	c.JSON(http.StatusOK, gin.H{"message": "User profile updated successfully"})
}
