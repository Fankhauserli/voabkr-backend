package helpers

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/gin-gonic/gin"
)

func generateSecureToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func SendVerificationEmail(c *gin.Context, db *db.Queries, userID int, email string) error {

	token := generateSecureToken(64)

	db.CreateVerificationToken(c.Request.Context(), db.CreateVerificationTokenParams{
		UserID:    int64(userID),
		Token:     token,                                                            // You should generate a secure token here
		ExpiresAt: c.Request.Context().Value("now").(time.Time).Add(24 * time.Hour), // Token expires in 24 hours
	})

	return nil
}
