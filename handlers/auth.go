package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Fankhauserli/voabkr-backend/helpers"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

func respondWithError(c *gin.Context, code int, message string, err error) {
	if err != nil {
		log.Printf("[ERROR] %s: %v", message, err)
		_ = c.Error(err)
	} else {
		log.Printf("[ERROR] %s", message)
	}

	resp := gin.H{"error": message}
	if err != nil && gin.Mode() == gin.DebugMode {
		resp["details"] = err.Error()
	}
	c.JSON(code, resp)
}

func setupSession(c *gin.Context, userID string) error {
	session := sessions.Default(c)
	session.Set("user_id", userID)

	// Must call Save() to write data to Redis and issue the Set-Cookie header
	if err := session.Save(); err != nil {
		return err
	}
	return nil
}

func (h *Handler) Login(c *gin.Context) {
	var req types.LoginRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Login: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	user, err := h.DB.GetUserByEmail(c, req.Email)
	if err != nil {
		log.Printf("[WARN] Login: user not found (%s): %v", req.Email, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	if !helpers.CheckPasswordHash(req.Password, user.PasswordHash) {
		log.Printf("[WARN] Login: invalid password for user (%s)", req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	err = setupSession(c, fmt.Sprint(user.ID))
	if err != nil {
		log.Printf("[ERROR] Login: failed to create session for user %d (%s): %v", user.ID, req.Email, err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create session", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged in successfully"})
}

func (h *Handler) Register(c *gin.Context) {
	var req types.RegisterRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[WARN] Register: invalid request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	hashedPassword, err := helpers.HashPassword(req.Password)
	if err != nil {
		log.Printf("[ERROR] Register: failed to hash password: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to hash password", err)
		return
	}

	createdUser, err := h.DB.CreateUser(c.Request.Context(), db.CreateUserParams{
		Email:        req.Email,
		PasswordHash: hashedPassword,
		Name:         req.Name,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "User with this email already exists"})
			return
		}
		log.Printf("[ERROR] Register: failed to create user (%s): %v", req.Email, err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create user", err)
		return
	}

	err = helpers.SendVerificationEmail(c, h.DB, int(createdUser.ID), createdUser.Email)
	if err != nil {
		log.Printf("[ERROR] Register: failed to send verification email for user %d (%s): %v", createdUser.ID, createdUser.Email, err)
		// Clean up created user to avoid leaving an unusable orphan record
		_ = h.DB.DeleteVerificationTokensByUserID(c.Request.Context(), createdUser.ID)
		_ = h.DB.DeleteUser(c.Request.Context(), createdUser.ID)
		respondWithError(c, http.StatusInternalServerError, "Failed to send verification email", err)
		return
	}

	err = setupSession(c, fmt.Sprint(createdUser.ID))
	if err != nil {
		log.Printf("[ERROR] Register: failed to create session for user %d (%s): %v", createdUser.ID, createdUser.Email, err)
		// Clean up created user to avoid leaving an unusable orphan record
		_ = h.DB.DeleteVerificationTokensByUserID(c.Request.Context(), createdUser.ID)
		_ = h.DB.DeleteUser(c.Request.Context(), createdUser.ID)
		respondWithError(c, http.StatusInternalServerError, "Failed to create session", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Registered successfully"})
}

func (h *Handler) Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Options(sessions.Options{MaxAge: -1}) // Clears cookie on client
	if err := session.Save(); err != nil {
		log.Printf("[ERROR] Logout: failed to terminate session: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to terminate session", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

func (h *Handler) VerifyEmail(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		token = c.Query("token")
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token is required"})
		return
	}

	verification, err := h.DB.GetVerificationToken(c.Request.Context(), token)
	if err != nil {
		log.Printf("[WARN] VerifyEmail: invalid or expired token '%s': %v", token, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired token"})
		return
	}

	if verification.ExpiresAt.Time.Before(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Token has expired"})
		return
	}

	err = h.DB.UpdateUserEmailVerified(c.Request.Context(), verification.UserID)
	if err != nil {
		log.Printf("[ERROR] VerifyEmail: failed to update email verified for user %d: %v", verification.UserID, err)
		respondWithError(c, http.StatusInternalServerError, "Failed to verify email", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Email verified successfully"})
}
