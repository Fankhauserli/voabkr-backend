package helpers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func generateSecureToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func SendVerificationEmail(c *gin.Context, database *db.Queries, userID int, email string) error {
	token := generateSecureToken(64)

	_, err := database.CreateVerificationToken(c.Request.Context(), db.CreateVerificationTokenParams{
		UserID:    int64(userID),
		Token:     token,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}, // Token expires in 24 hours
	})
	if err != nil {
		return fmt.Errorf("failed to store verification token: %w", err)
	}

	smtpAddr := os.Getenv("SMTP_ADDR")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASS")
	fromEmail := os.Getenv("FROM_EMAIL")

	if smtpAddr == "" || fromEmail == "" {
		// If SMTP is not configured, skip sending email to avoid blocking registration in dev
		return nil
	}

	var smtpAuth smtp.Auth
	if smtpUser != "" && smtpPass != "" {
		host, _, err := net.SplitHostPort(smtpAddr)
		if err != nil {
			host = smtpAddr
		}
		smtpAuth = smtp.PlainAuth("", smtpUser, smtpPass, host)
	}

	msg := []byte("Subject: Verify your email\r\n\r\nVerification email content, including the token: " + token)
	if err := smtp.SendMail(smtpAddr, smtpAuth, fromEmail, []string{email}, msg); err != nil {
		return fmt.Errorf("failed to send verification email: %w", err)
	}

	return nil
}
