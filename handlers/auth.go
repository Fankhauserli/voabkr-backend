package handlers

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Fankhauserli/voabkr-backend/helpers"
	"github.com/Fankhauserli/voabkr-backend/mail"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/tokens"
	"github.com/Fankhauserli/voabkr-backend/types"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Lifetimes are one source of truth: the same value bounds the stored token and
// the validity the email states.
const (
	verificationTokenLifetime  = 24 * time.Hour
	passwordResetTokenLifetime = time.Hour
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

	err = middleware.SignIn(c, user.ID)
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

	_, err = h.DB.CreateUserSettings(c.Request.Context(), createdUser.ID)
	if err != nil {
		log.Printf("[ERROR] Register: failed to create user settings for user %d (%s): %v", createdUser.ID, createdUser.Email, err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create user settings", err)
		return
	}

	// The account exists once the rows are written. A token or delivery failure
	// is logged and does not undo it: resend verification is the recovery path.
	if token, tokenErr := h.issueVerificationToken(c, createdUser); tokenErr != nil {
		log.Printf("[ERROR] Register: failed to issue a verification token for user %d (%s): %v", createdUser.ID, createdUser.Email, tokenErr)
	} else if result := h.Mail.SendVerification(createdUser.Email, createdUser.Name, token, verificationTokenLifetime); result.Outcome == mail.Failed {
		log.Printf("[ERROR] Register: failed to send verification email for user %d (%s): %v", createdUser.ID, createdUser.Email, result.Err)
	}

	err = middleware.SignIn(c, createdUser.ID)
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
	if err := middleware.SignOut(c); err != nil {
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
		// The token itself never reaches a log, in any mode.
		log.Printf("[WARN] VerifyEmail: invalid or expired verification token: %v", err)
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

func (h *Handler) ResendVerificationEmail(c *gin.Context) {
	// A signed-in learner's own address is the one to send to; a guest names it
	// in the body. A session this instance cannot place is treated as the guest
	// it is, so a lost verification message never strands the learner.
	caller, signedIn := middleware.Caller(c)

	var user db.User
	var err error

	if signedIn {
		user, err = h.DB.GetUser(c.Request.Context(), caller)
	}

	if user.ID == 0 {
		var req types.ResendVerificationRequest
		if bindErr := c.ShouldBindJSON(&req); bindErr == nil && req.Email != "" {
			user, err = h.DB.GetUserByEmail(c.Request.Context(), req.Email)
		}
	}

	if user.ID == 0 || err != nil {
		if !signedIn {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Please log in or provide your registered email address"})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		}
		return
	}

	if user.EmailVerified {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Email is already verified"})
		return
	}

	// Invalidate any existing verification tokens for this user before issuing a fresh one
	_ = h.DB.DeleteVerificationTokensByUserID(c.Request.Context(), user.ID)

	token, err := h.issueVerificationToken(c, user)
	if err != nil {
		log.Printf("[ERROR] ResendVerificationEmail: failed to issue a verification token for user %d (%s): %v", user.ID, user.Email, err)
		respondWithError(c, http.StatusInternalServerError, "Failed to send verification email", err)
		return
	}

	if result := h.Mail.SendVerification(user.Email, user.Name, token, verificationTokenLifetime); result.Outcome == mail.Failed {
		log.Printf("[ERROR] ResendVerificationEmail: failed to send verification email for user %d (%s): %v", user.ID, user.Email, result.Err)
		respondWithError(c, http.StatusInternalServerError, "Failed to send verification email", result.Err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Verification link sent successfully. Please check your inbox."})
}

// issueVerificationToken mints a verification token for user and stores it, so
// the token the mail module is handed is the one that verifies the address.
func (h *Handler) issueVerificationToken(c *gin.Context, user db.User) (string, error) {
	token, err := h.Tokens.Mint(tokens.Verification)
	if err != nil {
		return "", err
	}

	_, err = h.DB.CreateVerificationToken(c.Request.Context(), db.CreateVerificationTokenParams{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(verificationTokenLifetime), Valid: true},
	})
	if err != nil {
		return "", err
	}

	return token, nil
}

func (h *Handler) ForgotPassword(c *gin.Context) {
	var req types.ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	user, err := h.DB.GetUserByEmail(c.Request.Context(), req.Email)
	if err == nil && user.ID > 0 {
		_ = h.DB.DeletePasswordResetTokensByUserID(c.Request.Context(), user.ID)

		token, mintErr := h.Tokens.Mint(tokens.PasswordReset)
		if mintErr != nil {
			log.Printf("[ERROR] ForgotPassword: failed to mint a reset token for %s: %v", user.Email, mintErr)
		} else if _, storeErr := h.DB.CreatePasswordResetToken(c.Request.Context(), db.CreatePasswordResetTokenParams{
			UserID:    user.ID,
			Token:     token,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(passwordResetTokenLifetime), Valid: true},
		}); storeErr != nil {
			log.Printf("[ERROR] ForgotPassword: failed to store a reset token for %s: %v", user.Email, storeErr)
		} else {
			log.Printf("[INFO] ForgotPassword: created password reset token for %s", user.Email)
			if result := h.Mail.SendPasswordReset(user.Email, user.Name, token, passwordResetTokenLifetime); result.Outcome == mail.Failed {
				log.Printf("[ERROR] ForgotPassword: failed to send reset email for %s: %v", user.Email, result.Err)
			}
		}
	}

	// Always return OK to avoid email enumeration
	c.JSON(http.StatusOK, gin.H{"message": "If an account exists with this email, a reset link has been dispatched."})
}

func (h *Handler) ResetPassword(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing reset token"})
		return
	}

	var req types.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithError(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters long"})
		return
	}

	resetRecord, err := h.DB.GetPasswordResetToken(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired reset token"})
		return
	}

	hash, err := helpers.HashPassword(req.Password)
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to hash password", err)
		return
	}

	err = h.DB.UpdateUserPassword(c.Request.Context(), db.UpdateUserPasswordParams{
		ID:           resetRecord.UserID,
		PasswordHash: hash,
	})
	if err != nil {
		respondWithError(c, http.StatusInternalServerError, "Failed to update password", err)
		return
	}

	_ = h.DB.DeletePasswordResetToken(c.Request.Context(), token)

	c.JSON(http.StatusOK, gin.H{"message": "Password successfully reset. You may now log in."})
}
