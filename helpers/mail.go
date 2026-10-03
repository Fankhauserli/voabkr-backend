package helpers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/templates"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

// VerificationEmailData contains variables available within email templates.
type VerificationEmailData struct {
	Name            string
	Email           string
	VerificationURL string
	Token           string
	ExpireHours     int
	Year            int
}

func generateSecureToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// renderVerificationEmail renders the HTML and Plain Text versions of the email template.
func renderVerificationEmail(data VerificationEmailData) (plainText string, htmlContent string, err error) {
	// Parse HTML template
	tmplHTML, err := template.ParseFS(templates.MailFS, "mail/verification.html")
	if err != nil {
		return "", "", fmt.Errorf("failed to parse html email template: %w", err)
	}
	var htmlBuf bytes.Buffer
	if err := tmplHTML.Execute(&htmlBuf, data); err != nil {
		return "", "", fmt.Errorf("failed to execute html email template: %w", err)
	}

	// Parse Plain Text template
	tmplTxt, err := template.ParseFS(templates.MailFS, "mail/verification.txt")
	if err != nil {
		return "", "", fmt.Errorf("failed to parse text email template: %w", err)
	}
	var txtBuf bytes.Buffer
	if err := tmplTxt.Execute(&txtBuf, data); err != nil {
		return "", "", fmt.Errorf("failed to execute text email template: %w", err)
	}

	return txtBuf.String(), htmlBuf.String(), nil
}

// buildMIMEMessage formats a multipart/alternative email body with both plaintext and HTML parts.
func buildMIMEMessage(from, to, subject, textBody, htmlBody string) []byte {
	boundary := fmt.Sprintf("voabkr_boundary_%d", time.Now().UnixNano())
	var msg bytes.Buffer

	// Headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// Plain text alternative
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(textBody)
	msg.WriteString("\r\n\r\n")

	// HTML alternative
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n\r\n")

	// Final boundary
	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	return msg.Bytes()
}

// SendVerificationEmail creates a secure verification token and dispatches a styled verification email.
func SendVerificationEmail(c *gin.Context, database *db.Queries, userID int, email string, name string) error {
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
		log.Printf("[INFO] SMTP_ADDR or FROM_EMAIL is not configured, skipping email delivery for %s (token: %s)", email, token)
		return nil
	}

	// Determine frontend verification URL
	frontendBaseURL := os.Getenv("FRONTEND_URL")
	if frontendBaseURL == "" {
		frontendBaseURL = os.Getenv("APP_URL")
	}
	if frontendBaseURL == "" {
		frontendBaseURL = "http://localhost:5173" // Vite default dev port
	}
	frontendBaseURL = strings.TrimRight(frontendBaseURL, "/")
	verificationURL := fmt.Sprintf("%s/verify-email?token=%s", frontendBaseURL, token)

	// Render templates
	data := VerificationEmailData{
		Name:            strings.TrimSpace(name),
		Email:           email,
		VerificationURL: verificationURL,
		Token:           token,
		ExpireHours:     24,
		Year:            time.Now().Year(),
	}

	textBody, htmlBody, err := renderVerificationEmail(data)
	if err != nil {
		log.Printf("[ERROR] Failed to render email templates for %s: %v", email, err)
		return fmt.Errorf("failed to render verification email: %w", err)
	}

	// Prepare SMTP auth
	var smtpAuth smtp.Auth
	if smtpUser != "" && smtpPass != "" {
		host, _, splitErr := net.SplitHostPort(smtpAddr)
		if splitErr != nil {
			host = smtpAddr
		}
		smtpAuth = smtp.PlainAuth("", smtpUser, smtpPass, host)
	}

	subject := "Verify your voabkr account | 이메일 인증"
	msg := buildMIMEMessage(fromEmail, email, subject, textBody, htmlBody)

	if err := smtp.SendMail(smtpAddr, smtpAuth, fromEmail, []string{email}, msg); err != nil {
		log.Printf("[ERROR] Failed to send email via SMTP to %s (server %s): %v", email, smtpAddr, err)
		return fmt.Errorf("failed to send verification email: %w", err)
	}

	log.Printf("[INFO] Verification email successfully sent to %s", email)
	return nil
}
