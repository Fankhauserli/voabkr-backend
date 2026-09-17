package helpers

import (
	"crypto/rand"
	"encoding/hex"
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

	database.CreateVerificationToken(c.Request.Context(), db.CreateVerificationTokenParams{
		UserID:    int64(userID),
		Token:     token,                                                                                                   // You should generate a secure token here
		ExpiresAt: pgtype.Timestamptz{Time: c.Request.Context().Value("now").(time.Time).Add(24 * time.Hour), Valid: true}, // Token expires in 24 hours
	})

	SMTTPAddr := os.Getenv("SMTP_ADDR")
	SMTPUser := os.Getenv("SMTP_USER")
	SMTPPass := os.Getenv("SMTP_PASS")
	FromEmail := os.Getenv("FROM_EMAIL")

	smtpAuth := smtp.PlainAuth("", SMTPUser, SMTPPass, SMTTPAddr)
	if SMTPUser == "" || SMTPPass == "" {
		smtpAuth = nil // No authentication if credentials are not provided
	}

	smtp.SendMail(SMTTPAddr, smtpAuth, FromEmail, []string{email}, []byte("Verification email content, including the token: "+token))

	return nil
}
