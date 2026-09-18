package helpers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
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
		log.Printf("[ERROR] Failed to store verification token for user %d: %v", userID, err)
		return fmt.Errorf("failed to store verification token: %w", err)
	}

	smtpAddr := os.Getenv("SMTP_ADDR")
	smtpUser := os.Getenv("SMTP_USER")
	smtpPass := os.Getenv("SMTP_PASS")
	fromEmail := os.Getenv("FROM_EMAIL")

	if smtpAddr == "" || fromEmail == "" {
		log.Printf("[INFO] SMTP_ADDR or FROM_EMAIL is not configured, skipping email delivery")
		return nil
	}

	host, _, err := net.SplitHostPort(smtpAddr)
	if err != nil {
		// If port was omitted, default to 587
		host = smtpAddr
		smtpAddr = net.JoinHostPort(smtpAddr, "587")
	}

	var smtpAuth smtp.Auth
	if smtpUser != "" && smtpPass != "" {
		smtpAuth = smtp.PlainAuth("", smtpUser, smtpPass, host)
	}

	msg := []byte("Subject: Verify your email\r\n\r\nVerification email content, including the token: " + token)
	if err := smtp.SendMail(smtpAddr, smtpAuth, fromEmail, []string{email}, msg); err != nil {
		log.Printf("[ERROR] Failed to send email via SMTP to %s (server %s): %v", email, smtpAddr, err)
		return fmt.Errorf("failed to send verification email: %w", err)
	}

	return nil
}
